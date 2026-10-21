package hub

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"tokendancechat/backend/llm"
	"tokendancechat/backend/store"
)

const (
	maxQueuedRuns       = 64
	maxConversationRuns = 8
	maxActorQueuedRuns  = 2
	maxRunLifetime      = 2 * time.Minute
	maxRunExecution     = 60 * time.Second
	// How often a streaming run re-reads durable session state. Cancellation
	// and logout are already fenced in memory under runMu; this bounds the
	// remaining cross-check cost to a heartbeat instead of every token.
	runDurableCheckInterval = 500 * time.Millisecond
)

type runStore interface {
	WithBotRunPublication(string, func()) error
	SnapshotBotHistory(string, string, string, string, bool, int) ([]StoredMessage, error)
	CreateBotRun(store.BotRun, store.RunBudgetLimits) (string, error)
	GetBotRun(string) (store.BotRun, error)
	StartBotRun(string) error
	FinishBotRun(string, string, string, int64, bool) error
	CompleteBotRun(string, string, json.RawMessage, int64, bool) (StoredMessage, bool, error)
	InterruptBotRuns() error
	RecentBotRuns(string, string, int) ([]store.BotRun, error)
	GetMessageByID(string) (StoredMessage, error)
}

type botModel interface {
	ChatStream(context.Context, string, []llm.Message, []llm.Tool, llm.EventHandler) (llm.Result, error)
	Model() string
	MaxTokens() int
	MaxToolRounds() int
}

type botRun struct {
	record    store.BotRun
	client    *Client
	key, mode string
	input     []llm.Message
	trigger   StoredMessage
	ctx       context.Context
	cancel    context.CancelFunc
	timer     *time.Timer
}

type conversationQueue struct {
	pending   []*botRun
	active    bool
	lastActor string
}

// actorID is never populated from a WebSocket payload. Prefixes keep local,
// guest, and Account identities disjoint while preserving the OIDC handle map.
func (c *Client) actorID() string {
	if c.authenticatedSubject != "" {
		return "oidc:" + c.authenticatedSubject
	}
	if c.authenticatedUsername != "" {
		return "local:" + c.authenticatedUsername
	}
	return "guest:" + c.username
}

func (c *Client) SetAuthenticatedSubject(subject string)  { c.authenticatedSubject = subject }
func (c *Client) SetAuthenticatedSessionExpiry(exp int64) { c.authenticatedSessionExpiresAt = exp }

func (h *Hub) initRuns() {
	h.runStore, _ = h.store.(runStore)
	h.runs = make(map[string]*botRun)
	h.runQueues = make(map[string]*conversationQueue)
	h.runModel = h.llmClient
	if h.runStore != nil {
		if err := h.runStore.InterruptBotRuns(); err != nil {
			log.Printf("bot run recovery failed: %v", err)
			h.runStore = nil
		}
	}
}

func (h *Hub) RunControlEnabled() bool { return h.runStore != nil && h.LLMEnabled() }

func (h *Hub) enqueueRun(c *Client, trigger StoredMessage, private bool) {
	if h.runStore == nil || h.runModel == nil {
		h.SendToClient(c, Message{Type: "error", ErrorCode: "RUN_UNAVAILABLE", Content: "TokenBot is temporarily unavailable."})
		return
	}
	mode := "public"
	room := trigger.RoomID
	to := ""
	if private {
		mode = "private"
		room = ""
		to = c.username
	}
	if !c.authenticatedSessionIsCurrent() || (!private && room == "") {
		h.SendToClient(c, Message{Type: "error", ErrorCode: "RUN_UNAVAILABLE", Content: "TokenBot is temporarily unavailable."})
		return
	}
	h.runMu.Lock()
	defer h.runMu.Unlock()
	select {
	case <-h.done:
		return
	default:
	}
	key := conversationKey(mode, room, c.actorID())
	q := h.runQueues[key]
	actorPending := 0
	count := 0
	for _, queue := range h.runQueues {
		count += len(queue.pending)
		for _, r := range queue.pending {
			if r.record.ActorID == c.actorID() {
				actorPending++
			}
		}
	}
	if count >= maxQueuedRuns || actorPending >= maxActorQueuedRuns || q != nil && len(q.pending) >= maxConversationRuns {
		h.SendToClient(c, Message{Type: "error", ErrorCode: "RUN_QUEUE_FULL", Content: "TokenBot queue is full. Please try again shortly.", Private: private, RoomID: room, To: to})
		return
	}
	snapshot, err := h.runStore.SnapshotBotHistory(trigger.ID, room, c.username, h.botName, private, h.contextMessages()*historyLookbackFactor)
	if err != nil {
		h.SendToClient(c, Message{Type: "error", ErrorCode: "RUN_UNAVAILABLE", Content: "TokenBot is temporarily unavailable."})
		return
	}
	snapshot = h.windowFromAnchor(key, snapshot, h.contextMessages())
	input := make([]llm.Message, 0, len(snapshot)+1)
	for _, m := range snapshot {
		if m.Deleted || m.Username == "system" || m.Content == "" {
			continue
		}
		role := "user"
		if m.Username == h.botName {
			role = "assistant"
		}
		input = append(input, llm.Message{Role: role, Content: m.Content, Username: m.Username, Timestamp: m.Timestamp})
	}
	// The static prefix stays cacheable; the explicit task marker lives at the
	// tail and can never be replaced by another speaker's later room message.
	input = append(input, llm.Message{Role: "user", Content: "[Run trigger: message_id=" + trigger.ID + "; requested_by=@" + c.username + "]\nAnswer this frozen request:\n" + trigger.Content})
	raw, _ := json.Marshal(input)
	now := time.Now()
	r := &botRun{client: c, key: key, mode: mode, input: input, trigger: trigger, record: store.BotRun{ID: uuid.NewString(), MessageID: uuid.NewString(), TriggerMessageID: trigger.ID, ActorID: c.actorID(), RequestedBy: c.username, RoomID: room, ToUser: to, BotName: h.botName, SessionGeneration: c.authenticatedSessionGeneration, SessionExpiresAt: c.authenticatedSessionExpiresAt, Status: "queued", Input: raw, BudgetDay: now.In(botBudgetZone).Format(botBudgetDayLayout), IPKey: ipQuotaKey(c.ip), CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli()}}
	r.record.ReservedTokens = h.runTokenReservation(raw, mode)
	limits := h.budget.limits
	reason, err := h.runStore.CreateBotRun(r.record, store.RunBudgetLimits{PerUser: limits.perUser, PerIP: limits.perIP, Replies: limits.globalReplies, Tokens: limits.globalTokens})
	if reason == "duplicate" {
		return
	}
	if err != nil {
		log.Printf("bot run admission failed: %v", err)
		h.SendToClient(c, Message{Type: "error", ErrorCode: "RUN_UNAVAILABLE", Content: "TokenBot is temporarily unavailable."})
		return
	}
	if reason != "" {
		h.SendToClient(c, Message{Type: "error", ErrorCode: "BOT_BUDGET_EXCEEDED", Content: botBudgetMessage(reason), Private: private, RoomID: room, To: to})
		return
	}
	r.ctx, r.cancel = context.WithDeadline(context.Background(), now.Add(maxRunLifetime))
	h.runs[r.record.ID] = r
	if q == nil {
		q = &conversationQueue{}
		h.runQueues[key] = q
		h.runReady = append(h.runReady, key)
	}
	q.pending = append(q.pending, r)
	r.timer = time.AfterFunc(maxRunLifetime, func() {
		h.runMu.Lock()
		defer h.runMu.Unlock()
		if r.record.Status == "queued" || r.record.Status == "running" {
			h.finishRunLocked(r, "failed", "timeout", 0, r.record.Status == "queued")
			h.scheduleRunsLocked()
		}
	})
	h.sendRunLocked(r, runEvent(r.record, triggerPreviewText(r.trigger.Content)))
	h.scheduleRunsLocked()
}

// Reservations count every possible request in the bounded tool loop using a
// byte upper estimate for input tokens and bounded tool/result/assistant growth.
// SDK retries are disabled. Unknown or partial usage consumes this reservation.
func (h *Hub) runTokenReservation(input []byte, mode string) int64 {
	rounds := int64(h.runModel.MaxToolRounds() + 1)
	base := int64(len(input) + len(h.BuildPrompt(mode)) + 16384)
	return rounds*base + rounds*(rounds-1)/2*llm.MaxRoundGrowthBytes + rounds*int64(h.runModel.MaxTokens())
}

func (h *Hub) scheduleRunsLocked() {
	select {
	case <-h.done:
		return
	default:
	}
	limit := h.budget.limits.maxConcurrent
	if limit <= 0 {
		limit = defaultBotMaxConcurrent
	}
	if limit > 16 {
		limit = 16
	}
	for h.runActive < limit {
		var chosen *botRun
		discarded := false
		scans := len(h.runReady)
		for i := 0; i < scans; i++ {
			key := h.runReady[0]
			h.runReady = h.runReady[1:]
			q := h.runQueues[key]
			if q == nil {
				continue
			}
			// Remove cancelled queued entries without allowing them to start later.
			kept := q.pending[:0]
			for _, r := range q.pending {
				if r.record.Status == "queued" {
					kept = append(kept, r)
				}
			}
			q.pending = kept
			if !q.active && len(q.pending) == 0 {
				delete(h.runQueues, key)
				continue
			}
			h.runReady = append(h.runReady, key)
			if q.active || len(q.pending) == 0 {
				continue
			}
			index := 0
			for j, r := range q.pending {
				if r.record.ActorID != q.lastActor {
					index = j
					break
				}
			}
			chosen = q.pending[index]
			q.pending = append(q.pending[:index], q.pending[index+1:]...)
			if !h.runAuthorizedLocked(chosen) || h.runStore.StartBotRun(chosen.record.ID) != nil {
				h.finishRunLocked(chosen, "cancelled", "authorization", 0, true)
				discarded = true
				chosen = nil
				continue
			}
			q.active = true
			q.lastActor = chosen.record.ActorID
			h.runActive++
			chosen.record.Status = "running"
			chosen.record.UpdatedAt = time.Now().UnixMilli()
			h.sendRunLocked(chosen, runEvent(chosen.record, triggerPreviewText(chosen.trigger.Content)))
			break
		}
		if chosen == nil {
			// Dropping an invalid head is progress. Revisit ready conversations so a
			// valid successor can start even when no future enqueue/completion arrives.
			if discarded {
				continue
			}
			return
		}
		go h.executeRun(chosen)
	}
}

// clipLog bounds an error string for operational logs without dumping payloads.
func clipLog(value string, limit int) string {
	runes := []rune(value)
	if limit <= 0 || len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

// runPublishableLocked is the per-delta gate. Cancellation and every terminal
// transition happen under runMu, so the in-memory record is authoritative
// here. The durable session/generation check is still performed, but at a
// bounded interval (runDurableCheckInterval) instead of once per token.
func (h *Hub) runPublishableLocked(r *botRun) bool {
	if r.record.Status != "running" {
		return false
	}
	if r.mode == "public" && (r.record.RoomID == "" || r.client.getCurrentRoomID() != r.record.RoomID) {
		return false
	}
	return true
}

func (h *Hub) runAuthorizedLocked(r *botRun) bool {
	if r.ctx.Err() != nil || !r.client.authenticatedSessionIsCurrent() {
		return false
	}
	if r.mode == "public" && (r.record.RoomID == "" || r.client.getCurrentRoomID() != r.record.RoomID) {
		return false
	}
	persisted, err := h.runStore.GetBotRun(r.record.ID)
	return err == nil && (persisted.Status == "queued" || persisted.Status == "running")
}

func runEvent(r store.BotRun, preview string) Message {
	return Message{Type: "bot_run", RunID: r.ID, MessageID: r.MessageID, StreamID: r.ID, Status: r.Status, TriggerMessageID: r.TriggerMessageID, TriggerPreview: preview, RequestedBy: r.RequestedBy, RoomID: r.RoomID, Private: r.ToUser != "", To: r.ToUser, Timestamp: r.UpdatedAt}
}

// The snapshot path has no in-memory trigger; a bounded preview lets a
// reconnecting client label its activity rows without loading private history.
const triggerPreviewLimit = 120

func triggerPreviewText(content string) string {
	content = strings.Join(strings.Fields(content), " ")
	runes := []rune(content)
	if len(runes) > triggerPreviewLimit {
		return string(runes[:triggerPreviewLimit-1]) + "…"
	}
	return content
}

// Caller holds runMu through fanout: cancellation and publication have one
// ordering point. A cancellation event is never followed by a new chunk.
func (h *Hub) sendRunLocked(r *botRun, msg Message) {
	// Pure terminal metadata stays observable after revocation, including when
	// a final committed just before Account logout blocked its live delivery.
	// It contains no prompt/model content; private status still goes only to
	// current sessions of the same actor. Result-bearing events remain fenced.
	if msg.Type == "bot_run" && (msg.Status == "completed" || msg.Status == "cancelled" || msg.Status == "failed") {
		if r.mode == "private" {
			h.sendToActor(r.record.ActorID, msg)
		} else {
			data, _ := json.Marshal(msg)
			h.BroadcastToRoom(data, r.record.RoomID)
		}
		return
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.runStore.WithBotRunPublication(r.record.ID, func() {
		h.mu.RLock()
		defer h.mu.RUnlock()
		for c := range h.clients {
			if c.authenticatedSessionExpiresAt > 0 && time.Now().Unix() >= c.authenticatedSessionExpiresAt {
				continue
			}
			if r.mode == "private" {
				if c.actorID() != r.record.ActorID || c.authenticatedSessionGeneration != r.record.SessionGeneration {
					continue
				}
			} else if r.record.RoomID == "" || c.getCurrentRoomID() != r.record.RoomID {
				continue
			}
			select {
			case c.send <- data:
			default:
				h.droppedMessages.Add(1)
			}
		}
	})
}

func (h *Hub) sendToActor(actor string, msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	// Do not hold membership locks during the durable session check: run
	// publication takes the store fence before the membership read lock.
	h.mu.RLock()
	var candidates []*Client
	for c := range h.clients {
		if c.actorID() == actor {
			candidates = append(candidates, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range candidates {
		if !c.authenticatedSessionIsCurrent() {
			continue
		}
		h.mu.RLock()
		if h.clients[c] {
			select {
			case c.send <- data:
			default:
				h.droppedMessages.Add(1)
			}
		}
		h.mu.RUnlock()
	}
}

func (c *Client) handleBotCancel(msg Message) {
	h := c.hub
	h.runMu.Lock()
	defer h.runMu.Unlock()
	fail := func() {
		h.SendToClient(c, Message{Type: "error", ErrorCode: "RUN_UNAVAILABLE", Content: "This run is unavailable.", RunID: msg.RunID})
	}
	if h.runStore == nil || c.username == "" || !c.authenticatedSessionIsCurrent() {
		fail()
		return
	}
	persisted, err := h.runStore.GetBotRun(msg.RunID)
	if err != nil || persisted.ActorID != c.actorID() {
		fail()
		return
	}
	r := h.runs[msg.RunID]
	if r == nil || persisted.Status == "completed" || persisted.Status == "failed" || persisted.Status == "cancelled" {
		preview := ""
		if m, err := h.runStore.GetMessageByID(persisted.TriggerMessageID); err == nil {
			preview = triggerPreviewText(m.Content)
		}
		h.SendToClient(c, runEvent(persisted, preview))
		return
	}
	h.finishRunLocked(r, "cancelled", "requested", 0, r.record.Status == "queued")
	h.scheduleRunsLocked()
}

func (h *Hub) finishRunLocked(r *botRun, status, reason string, tokens int64, known bool) {
	if r.record.Status != "queued" && r.record.Status != "running" {
		return
	}
	wasQueued := r.record.Status == "queued"
	if err := h.runStore.FinishBotRun(r.record.ID, status, reason, tokens, known); err != nil {
		log.Printf("bot run terminal state failed: %v", err)
	}
	r.record.Status = status
	r.record.UpdatedAt = time.Now().UnixMilli()
	r.cancel()
	if r.timer != nil {
		r.timer.Stop()
	}
	h.sendRunLocked(r, runEvent(r.record, triggerPreviewText(r.trigger.Content)))
	if wasQueued {
		if q := h.runQueues[r.key]; q != nil {
			for i, pending := range q.pending {
				if pending == r {
					q.pending = append(q.pending[:i], q.pending[i+1:]...)
					break
				}
			}
		}
		delete(h.runs, r.record.ID)
	}
}

func (h *Hub) executeRun(r *botRun) {
	ctx, cancel := context.WithTimeout(r.ctx, maxRunExecution)
	defer cancel()
	started := time.Now()
	defer func() {
		recovered := recover()
		h.runMu.Lock()
		defer h.runMu.Unlock()
		if recovered != nil {
			log.Printf("bot run panic recovered")
			h.finishRunLocked(r, "failed", "internal", 0, false)
		}
		if r.record.Status == "running" {
			h.finishRunLocked(r, "failed", "interrupted", 0, false)
		}
		if q := h.runQueues[r.key]; q != nil {
			q.active = false
		}
		h.runActive--
		delete(h.runs, r.record.ID)
		if r.timer != nil {
			r.timer.Stop()
		}
		r.cancel()
		h.scheduleRunsLocked()
	}()
	tools := h.botTools(r.mode, r.record.RoomID, r.record.RequestedBy)
	if h.WebSearchEnabled() {
		tools = append(tools, &webTool{hub: h, run: r}, &webTool{hub: h, run: r, fetch: true})
	}
	for i, t := range tools {
		tools[i] = &runTool{Tool: t, hub: h, run: r}
	}
	var citations citationStream
	var lastDurableCheck time.Time
	result, err := h.runModel.ChatStream(ctx, h.BuildPrompt(r.mode), r.input, tools, func(event llm.Event) error {
		h.runMu.Lock()
		defer h.runMu.Unlock()
		if !h.runPublishableLocked(r) || ctx.Err() != nil {
			return context.Canceled
		}
		if now := time.Now(); now.Sub(lastDurableCheck) >= runDurableCheckInterval {
			lastDurableCheck = now
			if !h.runAuthorizedLocked(r) {
				return context.Canceled
			}
		}
		// done follows durable delivery below, never the provider's tentative end.
		if event.Kind == "done" {
			return nil
		}
		if event.Kind == "text" {
			event.Text = citations.push(event.Text)
			if event.Text == "" {
				return nil
			}
		}
		if r.mode == "public" {
			if event.Kind != "text" {
				return nil
			}
			event = llm.Event{Kind: "text", Text: event.Text}
		}
		msg := streamMessage(h.botName, r.record.ID, event, r.mode == "private")
		msg.RunID = r.record.ID
		msg.MessageID = r.record.MessageID
		msg.RoomID = r.record.RoomID
		msg.To = r.record.ToUser
		h.sendRunLocked(r, msg)
		return nil
	})
	h.runMu.Lock()
	defer h.runMu.Unlock()
	if r.record.Status != "running" {
		return
	}
	if ctx.Err() != nil || !h.runAuthorizedLocked(r) {
		status, reason := "cancelled", "authorization"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status, reason = "failed", "timeout"
		}
		h.finishRunLocked(r, status, reason, 0, false)
		return
	}
	if err != nil || sanitizeBotContent(result.Text) == "" {
		// Keep failure diagnosis possible without logging message content.
		if err != nil {
			log.Printf("bot run failed: mode=%s reason=model err=%s", r.mode, clipLog(err.Error(), 400))
		} else {
			log.Printf("bot run failed: mode=%s reason=empty-model-output", r.mode)
		}
		h.finishRunLocked(r, "failed", "model", usageTokens(result), false)
		return
	}
	parts := result.Parts
	if r.mode == "public" {
		parts = llm.PublicParts(parts)
	}
	raw, err := json.Marshal(parts)
	if err != nil {
		h.finishRunLocked(r, "failed", "internal", 0, false)
		return
	}
	m, created, err := h.runStore.CompleteBotRun(r.record.ID, sanitizeBotContent(result.Text), raw, usageTokens(result), result.UsageKnown)
	if err != nil {
		h.finishRunLocked(r, "failed", "delivery", 0, false)
		return
	}
	// The store rebuilds source parts and reference links from this run's
	// durable evidence in the same transaction as the final message.
	_ = json.Unmarshal(m.Parts, &parts)
	r.record.Status = "completed"
	r.record.UpdatedAt = time.Now().UnixMilli()
	if created {
		// Final message replaces the preallocated stream draft by the same ID.

		done := streamMessage(h.botName, r.record.ID, llm.Event{Kind: "done", Parts: parts}, r.mode == "private")
		done.RunID = r.record.ID
		done.MessageID = m.ID
		done.RoomID = m.RoomID
		done.To = r.record.ToUser
		h.sendRunLocked(r, done)
		h.sendRunLocked(r, Message{Type: "message", ID: m.ID, MessageID: m.ID, RunID: r.record.ID, StreamID: r.record.ID, Username: m.Username, Content: m.Content, Parts: parts, Timestamp: m.Timestamp, ReplyToID: m.ReplyToID, ReplyToUser: r.record.RequestedBy, ReplyToContent: r.trigger.Content, RoomID: m.RoomID, Private: r.mode == "private", To: r.record.ToUser})
	}
	h.sendRunLocked(r, runEvent(r.record, triggerPreviewText(r.trigger.Content)))
	logReply(r.mode, r.record.RequestedBy, h.runModel.Model(), h.PromptSHA(r.mode), len(r.input), result, time.Since(started))
}

// Tool implementations are bounded read-only operations. Recheck before and
// after each execution, including tools that ignore context cancellation.
type runTool struct {
	llm.Tool
	hub *Hub
	run *botRun
}

func (t *runTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	check := func() bool {
		t.hub.runMu.Lock()
		defer t.hub.runMu.Unlock()
		return ctx.Err() == nil && t.run.record.Status == "running" && t.hub.runAuthorizedLocked(t.run)
	}
	if !check() {
		return "", context.Canceled
	}
	output, err := t.Tool.Execute(ctx, args)
	if !check() {
		return "", context.Canceled
	}
	return output, err
}

func (h *Hub) sendRunSnapshot(c *Client) {
	h.runMu.Lock()
	defer h.runMu.Unlock()
	if h.runStore == nil || !c.authenticatedSessionIsCurrent() {
		return
	}
	rows, err := h.runStore.RecentBotRuns(c.getCurrentRoomID(), c.actorID(), 100)
	if err != nil {
		return
	}
	for _, r := range rows {
		preview := ""
		if m, err := h.runStore.GetMessageByID(r.TriggerMessageID); err == nil {
			preview = triggerPreviewText(m.Content)
		}
		h.SendToClient(c, runEvent(r, preview))
	}
}

func (h *Hub) cancelAccountRuns(username string, generation int64) {
	h.runMu.Lock()
	defer h.runMu.Unlock()
	for _, r := range h.runs {
		if r.record.RequestedBy == username && r.record.SessionGeneration < generation {
			h.finishRunLocked(r, "cancelled", "account_logout", 0, r.record.Status == "queued")
		}
	}
	h.scheduleRunsLocked()
}
