package hub

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"tokendancechat/backend/llm"
)

// Ambient participation lets TokenBot decide, without an @mention, whether a
// public message deserves a reply. Explicit mentions always win; this router is
// intentionally small, rate-limited and disabled by default.
type ambientPolicy struct {
	enabled  bool
	cooldown time.Duration
	perHour  int
	timeout  time.Duration
	// quiet and maxWait define the per-room debounce window: a message starts
	// (or resets) a quiet-period timer, and the batch is judged once the room
	// has been silent for quiet — but never later than maxWait after the first
	// message, so a busy room cannot starve the judge forever. Group chat
	// bursts (people splitting a thought into several messages) arrive 1–5s
	// apart, so 8s of silence almost always collects the whole burst.
	quiet   time.Duration
	maxWait time.Duration
}

// ambientPending is one room's open debounce window: the latest eligible
// message and its sender, plus the hard deadline the window may not outlive.
type ambientPending struct {
	timer       *time.Timer
	trigger     StoredMessage
	client      *Client
	maxDeadline time.Time
}

// ambientController holds the participation policy plus its mutable pacing
// state. Cooldown and the hourly reply budget are per room, so one busy room
// cannot starve the others; a single global in-flight decision and a global
// failure backoff bound what judging itself can cost. The debounce window runs
// before any of those budgets are touched: a burst of messages collapses into
// at most one decision, one reply and one quota tick.
type ambientController struct {
	policy ambientPolicy

	mu           sync.Mutex
	last         map[string]time.Time       // per-room: last accepted decision
	replies      map[string][]time.Time     // per-room: accepted replies in the last hour
	inFlight     bool                       // one judge call at a time, globally
	failures     int                        // consecutive judge failures
	backoffUntil time.Time                  // global retry gate after failures
	pending      map[string]*ambientPending // per-room open debounce window
}

const (
	// ambientContextLimit is how many recent room messages the judge sees.
	ambientContextLimit = 12
	// ambientMaxBackoff caps the exponential retry gate after judge failures.
	ambientMaxBackoff = 30 * time.Minute
)

// ambientImageOnly matches messages that carry only image markdown and no
// text. The judge cannot see images, so spending a decision on them is waste.
var ambientImageOnly = regexp.MustCompile(`^(!\[[^\]]*\]\([^)]*\)\s*)+$`)

// ambientBotAliases mirrors the mention aliases handled by the explicit path
// in client.go, so a stray "@bot" here is not mistaken for addressing a human.
var ambientBotAliases = []string{"bot", "tokenbot", "webuichat", "webuibot", "webui"}

func envFlag(name string, fallback bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func newAmbientPolicy() ambientPolicy {
	cooldown := envInt("CHAT_BOT_AMBIENT_COOLDOWN_SEC", 90)
	quiet := envInt("CHAT_BOT_AMBIENT_QUIET_SEC", 8)
	maxWait := envInt("CHAT_BOT_AMBIENT_MAX_WAIT_SEC", 30)
	if maxWait < quiet {
		maxWait = quiet
	}
	return ambientPolicy{
		enabled:  envFlag("CHAT_BOT_AMBIENT", false),
		cooldown: time.Duration(cooldown) * time.Second,
		perHour:  envInt("CHAT_BOT_AMBIENT_PER_HOUR", 6),
		timeout:  10 * time.Second,
		quiet:    time.Duration(quiet) * time.Second,
		maxWait:  time.Duration(maxWait) * time.Second,
	}
}

func newAmbientController() ambientController {
	return ambientController{
		policy:  newAmbientPolicy(),
		last:    make(map[string]time.Time),
		replies: make(map[string][]time.Time),
		pending: make(map[string]*ambientPending),
	}
}

// ambientSystemPrompt names the bot so the judge can recognize its own earlier
// replies in the transcript and avoid talking to itself.
func ambientSystemPrompt(botName string) string {
	return `You are the participation router for ` + botName + `, an AI member of a public chat room.
Decide whether ` + botName + ` should reply to the LATEST message, given the recent room transcript.
Lines from ` + botName + ` are the bot's own earlier replies.

Reply only when ` + botName + ` clearly adds value:
- the latest message asks the room a question or requests help it can answer;
- a technical, product or factual discussion where it has concrete useful knowledge;
- someone is clearly talking to or about ` + botName + ` without an @mention.

Do NOT reply when:
- the message is a greeting, thanks, acknowledgement, small talk or banter between people;
- the conversation has already wound down or people are wrapping up;
- the message is addressed to a specific person (a direct @mention, or a reply to their message);
- the message is mostly emoji, an image, or a link shared without a question;
- the same person is posting several messages in a row, thinking out loud;
- ` + botName + ` was the last speaker and the new message does not continue that exchange;
- the room can obviously answer itself, or a reply would interrupt the flow.

When in doubt, stay silent: a considerate bot speaks less than the room. Most messages should be ignored.

Return ONLY strict JSON: {"action":"reply"|"ignore","reason":"brief reason"}`
}

// maybeAmbientReply is called after a public message is stored. It never blocks
// the sending client and never judges immediately: eligible messages join the
// room's debounce window, so a burst of messages collapses into one decision
// about the latest state of the conversation.
func (h *Hub) maybeAmbientReply(c *Client, trigger StoredMessage) {
	// Without a durable run store an accepted decision would only surface a
	// "TokenBot unavailable" error to a user who never asked for anything.
	if !h.ambient.policy.enabled || h.llmClient == nil || h.runStore == nil {
		return
	}
	if !ambientEligible(trigger, h.botName) {
		return
	}
	h.ambient.mu.Lock()
	defer h.ambient.mu.Unlock()
	p := h.ambient.pending[trigger.RoomID]
	if p == nil {
		p = &ambientPending{maxDeadline: time.Now().Add(h.ambient.policy.maxWait)}
		h.ambient.pending[trigger.RoomID] = p
		p.timer = time.AfterFunc(ambientFireIn(h.ambient.policy.quiet, p.maxDeadline), func() { h.fireAmbient(trigger.RoomID) })
	} else {
		// A new message resets the quiet window — but never past the hard
		// deadline, so an endlessly busy room still gets its batch judged.
		p.timer.Reset(ambientFireIn(h.ambient.policy.quiet, p.maxDeadline))
	}
	p.trigger = trigger
	p.client = c
}

// ambientFireIn is min(quiet, time-to-deadline), clamped at zero: the window
// fires after the room goes quiet, and never later than the hard deadline.
func ambientFireIn(quiet time.Duration, maxDeadline time.Time) time.Duration {
	fireIn := quiet
	if remain := time.Until(maxDeadline); remain < fireIn {
		fireIn = remain
	}
	if fireIn < 0 {
		fireIn = 0
	}
	return fireIn
}

// fireAmbient closes the room's debounce window and, if the budgets still
// allow it, runs one decision about the latest message of the batch. A batch
// rejected here (cooldown, quota, backoff, an explicit run owning the room, or
// another decision in flight) sinks quietly — the next eligible message opens
// a fresh window instead of retrying stale context.
func (h *Hub) fireAmbient(roomID string) {
	h.ambient.mu.Lock()
	p := h.ambient.pending[roomID]
	delete(h.ambient.pending, roomID)
	h.ambient.mu.Unlock()
	if p == nil {
		return
	}
	select {
	case <-h.done:
		return
	default:
	}
	if !h.reserveAmbientSlot(roomID) {
		return
	}
	go h.evaluateAmbient(p.client, p.trigger)
}

// stopAmbient drops every open debounce window without judging it.
func (h *Hub) stopAmbient() {
	h.ambient.mu.Lock()
	defer h.ambient.mu.Unlock()
	for roomID, p := range h.ambient.pending {
		p.timer.Stop()
		delete(h.ambient.pending, roomID)
	}
}

// ambientEligible is the deterministic pre-filter: it costs no model tokens
// and drops messages a judge should never see.
func ambientEligible(trigger StoredMessage, botName string) bool {
	// The bot never wakes itself; system rows are not conversation.
	if trigger.Username == botName || trigger.Username == "system" {
		return false
	}
	// Bot finals land on the main room timeline, not inside threads, so
	// answering a thread message would reply in the wrong place.
	if trigger.ThreadID != "" {
		return false
	}
	content := strings.TrimSpace(trigger.Content)
	if len([]rune(content)) < 4 {
		return false
	}
	if ambientImageOnly.MatchString(content) {
		return false
	}
	// A message @mentioning a specific person belongs to that exchange. Bot
	// mentions never reach this path (they take the explicit route), and
	// room-wide aliases address everyone, the bot included.
	for _, mention := range parseMentions(content) {
		if isAssistantAlias(mention, botName, ambientBotAliases...) {
			continue
		}
		if mentionRoomAlias(mention) {
			continue
		}
		return false
	}
	return true
}

func mentionRoomAlias(name string) bool {
	return strings.EqualFold(name, "all") || strings.EqualFold(name, "everyone") ||
		strings.EqualFold(name, "here") || strings.EqualFold(name, "channel")
}

// reserveAmbientSlot enforces per-room cooldown and hourly pacing, one
// decision in flight, the failure backoff gate and the "one owner per room"
// rule before any model tokens are spent.
func (h *Hub) reserveAmbientSlot(roomID string) bool {
	h.ambient.mu.Lock()
	defer h.ambient.mu.Unlock()
	if h.ambient.inFlight || roomID == "" {
		return false
	}
	now := time.Now()
	if now.Before(h.ambient.backoffUntil) {
		return false
	}
	if last, ok := h.ambient.last[roomID]; ok && now.Sub(last) < h.ambient.policy.cooldown {
		return false
	}
	cutoff := now.Add(-time.Hour)
	kept := h.ambient.replies[roomID][:0]
	for _, at := range h.ambient.replies[roomID] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) == 0 {
		delete(h.ambient.replies, roomID)
	} else {
		h.ambient.replies[roomID] = kept
	}
	if len(kept) >= h.ambient.policy.perHour {
		return false
	}
	if !h.roomAmbientFree(roomID) {
		return false
	}
	h.ambient.inFlight = true
	h.ambient.last[roomID] = now
	return true
}

// roomAmbientFree reports whether no public run currently owns the room.
func (h *Hub) roomAmbientFree(roomID string) bool {
	h.runMu.Lock()
	defer h.runMu.Unlock()
	for _, run := range h.runs {
		if run.mode == "public" && run.record.RoomID == roomID &&
			(run.record.Status == "queued" || run.record.Status == "running") {
			return false
		}
	}
	return true
}

func (h *Hub) evaluateAmbient(c *Client, trigger StoredMessage) {
	defer func() {
		h.ambient.mu.Lock()
		h.ambient.inFlight = false
		h.ambient.mu.Unlock()
	}()
	if c == nil || !c.authenticatedSessionIsCurrent() {
		return
	}

	messages := h.store.GetRoomMessages(trigger.RoomID, ambientContextLimit, 0)
	lines := buildAmbientTranscript(messages, trigger, h.botName)

	ctx, cancel := context.WithTimeout(context.Background(), h.ambient.policy.timeout)
	defer cancel()
	answer, err := h.llmClient.Chat(ctx, ambientSystemPrompt(h.botName), []llm.Message{
		{Role: "user", Content: strings.Join(lines, "\n")},
	})
	if err != nil {
		h.noteAmbientFailure(trigger.RoomID, err)
		return
	}
	h.noteAmbientSuccess()
	action, reason := parseAmbientDecision(answer)
	log.Printf("ambient selector: room=%s author=%s action=%s reason=%s", trigger.RoomID, trigger.Username, action, clipLog(reason, 120))
	if action != "reply" {
		return
	}
	if !c.authenticatedSessionIsCurrent() {
		return
	}
	// The judge may have run for seconds; an explicit request that arrived
	// meanwhile owns the room now.
	if !h.roomAmbientFree(trigger.RoomID) {
		return
	}
	h.ambient.mu.Lock()
	h.ambient.replies[trigger.RoomID] = append(h.ambient.replies[trigger.RoomID], time.Now())
	h.ambient.mu.Unlock()
	h.enqueueRun(c, trigger, false, "ambient")
}

// noteAmbientFailure applies an exponential global retry gate (cooldown, 2x,
// 4x, ... capped at ambientMaxBackoff) so a broken model cannot turn every new
// message into a failing call plus a log line. The room's cooldown is
// refunded: the failure is not the room's fault, and the backoff gate is what
// throttles retries.
func (h *Hub) noteAmbientFailure(roomID string, err error) {
	h.ambient.mu.Lock()
	defer h.ambient.mu.Unlock()
	h.ambient.failures++
	shift := h.ambient.failures - 1
	if shift > 8 {
		shift = 8
	}
	backoff := h.ambient.policy.cooldown << shift
	if backoff <= 0 || backoff > ambientMaxBackoff {
		backoff = ambientMaxBackoff
	}
	h.ambient.backoffUntil = time.Now().Add(backoff)
	delete(h.ambient.last, roomID)
	log.Printf("ambient selector failed: room=%s failures=%d backoff=%s err=%s", roomID, h.ambient.failures, backoff, clipLog(err.Error(), 200))
}

func (h *Hub) noteAmbientSuccess() {
	h.ambient.mu.Lock()
	defer h.ambient.mu.Unlock()
	h.ambient.failures = 0
	h.ambient.backoffUntil = time.Time{}
}

// buildAmbientTranscript renders the recent room transcript for the judge.
// messages arrive newest-first (store contract); the trigger message itself is
// appended once as the marked LATEST line instead of appearing twice.
func buildAmbientTranscript(messages []StoredMessage, trigger StoredMessage, botName string) []string {
	authors := make(map[string]string, len(messages))
	for _, m := range messages {
		authors[m.ID] = m.Username
	}
	lines := make([]string, 0, len(messages)+1)
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.ID == trigger.ID || m.Deleted || m.Username == "system" {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		name := m.Username
		if m.ReplyToID != "" {
			if author, ok := authors[m.ReplyToID]; ok && author != m.Username {
				name = m.Username + " (replying to " + author + ")"
			}
		}
		lines = append(lines, name+": "+clipLog(content, 280))
	}
	lines = append(lines, "LATEST (by "+trigger.Username+"): "+clipLog(strings.TrimSpace(trigger.Content), 280))
	return lines
}

// parseAmbientDecision tolerates code fences and surrounding prose from smaller
// models while still requiring an explicit action.
func parseAmbientDecision(raw string) (action, reason string) {
	text := strings.TrimSpace(raw)
	if start := strings.Index(text, "{"); start >= 0 {
		if end := strings.LastIndex(text, "}"); end > start {
			text = text[start : end+1]
		}
	}
	var decision struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(text), &decision); err == nil {
		action = strings.ToLower(strings.TrimSpace(decision.Action))
		reason = strings.TrimSpace(decision.Reason)
		if action == "reply" || action == "ignore" {
			if reason == "" {
				reason = "-"
			}
			return action, reason
		}
	}
	// Smaller models often answer with prose instead of strict JSON. Negations
	// are checked before the bare "reply" token, which "should not reply" also
	// contains; anything ambiguous stays "ignore".
	lower := strings.ToLower(text)
	for _, neg := range []string{"not reply", "n't reply", "no reply", "not respond", "n't respond", "stay silent", "do not engage"} {
		if strings.Contains(lower, neg) {
			return "ignore", "prose-negated"
		}
	}
	if strings.Contains(lower, "ignore") {
		return "ignore", "prose-ignore"
	}
	if strings.Contains(lower, "reply") {
		return "reply", "prose-reply"
	}
	return "ignore", "unparseable"
}

// AmbientEnabled reports whether the router is active, for /api/config.
func (h *Hub) AmbientEnabled() bool { return h.ambient.policy.enabled && h.llmClient != nil }
