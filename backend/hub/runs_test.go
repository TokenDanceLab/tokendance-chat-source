package hub

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go/shared"
	"tokendancechat/backend/llm"
	"tokendancechat/backend/store"
)

type controlledCall struct {
	input  []llm.Message
	tools  []llm.Tool
	emit   llm.EventHandler
	finish chan llm.Result
}
type controlledModel struct {
	calls chan *controlledCall
	stop  chan struct{}
}

func (m *controlledModel) Model() string      { return "controlled" }
func (m *controlledModel) MaxTokens() int     { return 100 }
func (m *controlledModel) MaxToolRounds() int { return 3 }
func (m *controlledModel) ChatStream(_ context.Context, _ string, input []llm.Message, tools []llm.Tool, emit llm.EventHandler) (llm.Result, error) {
	c := &controlledCall{input: input, tools: tools, emit: emit, finish: make(chan llm.Result, 1)}
	select {
	case m.calls <- c:
	case <-m.stop:
		return llm.Result{}, context.Canceled
	}
	// Deliberately ignore model cancellation, emulating a provider/tool that
	// finishes late. The hub must fence callbacks and final publication itself.
	select {
	case r := <-c.finish:
		return r, nil
	case <-m.stop:
		return llm.Result{}, context.Canceled
	}
}
func nextCall(t *testing.T, m *controlledModel) *controlledCall {
	t.Helper()
	select {
	case c := <-m.calls:
		return c
	case <-time.After(3 * time.Second):
		t.Fatal("model did not start")
		return nil
	}
}
func finishCall(c *controlledCall, text string) {
	c.finish <- llm.Result{Text: text, Parts: []llm.Part{{Type: "text", Text: text}}, UsageKnown: true, Usage: llm.Usage{PromptTokens: 20, CompletionTokens: 4}}
}

func runTestHub(t *testing.T) (*Hub, *store.Store, *controlledModel) {
	t.Helper()
	t.Setenv("CHAT_SKIP_SEED", "true")
	s, err := store.New(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	h := New(s, &llm.Config{Model: "controlled"}, "TokenBot")
	m := &controlledModel{calls: make(chan *controlledCall, 16), stop: make(chan struct{})}
	h.runModel = m
	h.budget.limits = botBudgetLimits{maxConcurrent: 4}
	t.Cleanup(func() {
		h.Stop()
		close(m.stop)
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			h.runMu.Lock()
			active := h.runActive
			h.runMu.Unlock()
			if active == 0 {
				break
			}
			time.Sleep(time.Millisecond)
		}
		s.Close()
	})
	return h, s, m
}
func runClient(h *Hub, name, sub, room string) *Client {
	c := &Client{hub: h, username: name, authenticatedUsername: name, authenticatedSubject: sub, send: make(chan []byte, 256), currentRoomID: room}
	if sub != "" {
		c.authenticatedSessionCurrent = func() bool {
			r, err := h.store.(*store.Store).OIDCLogoutGeneration(sub)
			return err == nil && r == c.authenticatedSessionGeneration
		}
	}
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
	return c
}
func enqueueTestRun(t *testing.T, c *Client, text string, private bool) store.BotRun {
	t.Helper()
	room, to := c.getCurrentRoomID(), ""
	if private {
		room, to = "", c.hub.botName
	}
	trigger, err := c.hub.store.InsertMessage(c.username, text, "", room, to, "", "")
	if err != nil {
		t.Fatal(err)
	}
	c.hub.enqueueRun(c, trigger, private)
	rows, err := c.hub.runStore.RecentBotRuns(room, c.actorID(), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.TriggerMessageID == trigger.ID {
			return r
		}
	}
	t.Fatal("run not created")
	return store.BotRun{}
}
func awaitStatus(t *testing.T, s *store.Store, id, status string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r, err := s.GetBotRun(id)
		if err == nil && r.Status == status {
			return
		}
		time.Sleep(time.Millisecond)
	}
	r, _ := s.GetBotRun(id)
	t.Fatalf("run status=%s want %s", r.Status, status)
}
func drainEvents(c *Client) []Message {
	var out []Message
	for {
		select {
		case data := <-c.send:
			var m Message
			_ = json.Unmarshal(data, &m)
			out = append(out, m)
		default:
			return out
		}
	}
}

func TestRunsFreezeTriggerAndFairRoomQueue(t *testing.T) {
	h, s, model := runTestHub(t)
	room := h.DefaultRoomID()
	alice := runClient(h, "alice", "account-A", room)
	bob := runClient(h, "bob", "account-B", room)
	trigger, err := s.InsertMessage("alice", "@TokenBot original-A", "", room, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// The noisy message arrives even before enqueue/snapshot; rowid cutoff must
	// still prevent it from stealing A's trigger.
	_, _ = s.InsertMessage("bob", "LATER-NOISE-B", "", room, "", "", "")
	h.enqueueRun(alice, trigger, false)
	first := nextCall(t, model)
	raw, _ := json.Marshal(first.input)
	if strings.Contains(string(raw), "LATER-NOISE-B") || !strings.Contains(first.input[len(first.input)-1].Content, trigger.ID) {
		t.Fatalf("trigger was not frozen: %s", raw)
	}
	a2 := enqueueTestRun(t, alice, "A2", false)
	a3 := enqueueTestRun(t, alice, "A3", false)
	b1 := enqueueTestRun(t, bob, "B1", false)
	select {
	case <-model.calls:
		t.Fatal("same room ran concurrently")
	default:
	}
	finishCall(first, "answer A1")
	second := nextCall(t, model)
	if !strings.Contains(second.input[len(second.input)-1].Content, b1.TriggerMessageID) {
		t.Fatal("requester rotation did not let B precede A's backlog")
	}
	if err := second.emit(llm.Event{Kind: "text", Text: "answer B1"}); err != nil {
		t.Fatal(err)
	}
	finishCall(second, "answer B1")
	third := nextCall(t, model)
	if !strings.Contains(third.input[len(third.input)-1].Content, a2.TriggerMessageID) {
		t.Fatal("same actor FIFO was lost")
	}
	finishCall(third, "answer A2")
	fourth := nextCall(t, model)
	finishCall(fourth, "answer A3")
	awaitStatus(t, s, a3.ID, "completed")
	for _, m := range drainEvents(alice) {
		if m.RunID != "" {
			raw, _ := json.Marshal(m)
			if strings.Contains(string(raw), "account-A") || strings.Contains(string(raw), "account-B") {
				t.Fatal("subject leaked on public wire")
			}
			if m.Type == "stream" || m.Type == "message" {
				if m.MessageID == "" || m.StreamID != m.RunID {
					t.Fatalf("unstable stream identity: %+v", m)
				}
			}
		}
	}
}

func TestRunsCancelOwnerCrossTabAndLateCallbacks(t *testing.T) {
	h, s, model := runTestHub(t)
	room := h.DefaultRoomID()
	a := runClient(h, "alice", "account-A", room)
	tab := runClient(h, "alice", "account-A", room)
	b := runClient(h, "bob", "account-B", room)
	r := enqueueTestRun(t, a, "request", false)
	call := nextCall(t, model)
	b.handleBotCancel(Message{RunID: r.ID})
	b.handleBotCancel(Message{RunID: "missing-private-run"})
	var denials []Message
	for _, e := range drainEvents(b) {
		if e.Type == "error" {
			denials = append(denials, e)
		}
	}
	if len(denials) != 2 || denials[0].ErrorCode != "RUN_UNAVAILABLE" || denials[0].Content != denials[1].Content {
		t.Fatalf("cancel leaks authorization/existence: %+v", denials)
	}
	if err := call.emit(llm.Event{Kind: "text", Text: "before"}); err != nil {
		t.Fatal(err)
	}
	tab.handleBotCancel(Message{RunID: r.ID})
	awaitStatus(t, s, r.ID, "cancelled")
	drainEvents(a)
	drainEvents(tab)
	for i := 0; i < 50; i++ {
		if err := call.emit(llm.Event{Kind: "text", Text: "late"}); !errors.Is(err, context.Canceled) {
			t.Fatalf("late chunk accepted: %v", err)
		}
	}
	finishCall(call, "late final")
	time.Sleep(20 * time.Millisecond)
	for _, e := range drainEvents(a) {
		if e.RunID == r.ID && (e.Type == "stream" || e.Type == "message") {
			t.Fatalf("late output published: %+v", e)
		}
	}
	messages := s.GetRoomMessages(room, 100, 0)
	for _, m := range messages {
		if m.RunID == r.ID {
			t.Fatal("cancelled final was persisted")
		}
	}
}

func TestRunsQueuedCancellationAndConversationIndependence(t *testing.T) {
	h, s, model := runTestHub(t)
	room := h.DefaultRoomID()
	other, _ := s.CreateRoom("other")
	a := runClient(h, "alice", "account-A", room)
	tab := runClient(h, "alice", "account-A", other)
	b := runClient(h, "bob", "account-B", room)
	r := enqueueTestRun(t, a, "one", false)
	call := nextCall(t, model)
	queued := enqueueTestRun(t, b, "queued", false)
	b.handleBotCancel(Message{RunID: queued.ID})
	awaitStatus(t, s, queued.ID, "cancelled")
	independent := enqueueTestRun(t, tab, "other room", false)
	otherCall := nextCall(t, model)
	finishCall(otherCall, "other done")
	awaitStatus(t, s, independent.ID, "completed")
	finishCall(call, "first done")
	awaitStatus(t, s, r.ID, "completed")
	select {
	case <-model.calls:
		t.Fatal("cancelled queued run started")
	case <-time.After(20 * time.Millisecond):
	}
	got, _ := s.GetBotRun(queued.ID)
	if got.ChargedTokens != 0 {
		t.Fatalf("queued run charged tokens: %d", got.ChargedTokens)
	}
}

func TestRunsPrivateActorIsolationSnapshotAndFinalIdentity(t *testing.T) {
	h, s, model := runTestHub(t)
	room := h.DefaultRoomID()
	a := runClient(h, "alice", "account-A", room)
	tab := runClient(h, "alice", "account-A", room)
	b := runClient(h, "bob", "account-B", room)
	imposter := runClient(h, "alice", "different-subject", room)
	a.handleChatMessage(Message{Content: "private-request", To: "TokenBot"})
	call := nextCall(t, model)
	if len(drainEvents(b)) != 0 || len(drainEvents(imposter)) != 0 {
		t.Fatal("private echo/run leaked")
	}
	if len(drainEvents(tab)) == 0 {
		t.Fatal("same account tab did not see private request")
	}
	rows, _ := s.RecentBotRuns("", a.actorID(), 10)
	if len(rows) != 1 {
		t.Fatal("missing private run")
	}
	r := rows[0]
	if err := call.emit(llm.Event{Kind: "reasoning", Text: "private reasoning"}); err != nil {
		t.Fatal(err)
	}
	finishCall(call, "private answer")
	awaitStatus(t, s, r.ID, "completed")
	h.runMu.Lock()
	h.runMu.Unlock()
	events := drainEvents(tab)
	found := false
	for _, e := range events {
		if e.Type == "message" && e.RunID == r.ID {
			found = true
			if e.ID != r.MessageID || e.MessageID != e.ID || !e.Private {
				t.Fatalf("final ID/audience: %+v", e)
			}
		}
	}
	if !found {
		t.Fatal("private final missing on other tab")
	}
	if len(drainEvents(b)) != 0 || len(drainEvents(imposter)) != 0 {
		t.Fatal("private stream/final leaked")
	}
	h.sendRunSnapshot(tab)
	events = drainEvents(tab)
	if len(events) != 1 || events[0].Status != "completed" {
		t.Fatalf("reconnect terminal snapshot: %+v", events)
	}
	if events[0].TriggerPreview != "private-request" {
		t.Fatalf("snapshot trigger preview: %+v", events[0])
	}
}

func TestRunsAccountLogoutFencesToolsAndPublication(t *testing.T) {
	h, s, model := runTestHub(t)
	room := h.DefaultRoomID()
	a := runClient(h, "alice", "account-A", room)
	r := enqueueTestRun(t, a, "request", false)
	call := nextCall(t, model)
	queued := enqueueTestRun(t, a, "second", false)
	now := time.Now()
	_, err := s.ApplyOIDCBackchannelLogout("account-A", "logout-1", "hash", now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	// Persistence revocation alone is sufficient, even before hub notification.
	if _, err := call.tools[0].Execute(context.Background(), json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "run_inactive") {
		t.Fatalf("tool accepted revoked session: %v", err)
	}
	drainEvents(a)
	if err := call.emit(llm.Event{Kind: "text", Text: "late"}); err == nil {
		t.Fatal("revoked session published")
	}
	if err := h.DisconnectAccountSessions("alice", 1); err != nil {
		t.Fatal(err)
	}
	finishCall(call, "late final")
	awaitStatus(t, s, r.ID, "cancelled")
	awaitStatus(t, s, queued.ID, "cancelled")
	for _, m := range s.GetRoomMessages(room, 100, 0) {
		if m.RunID == r.ID {
			t.Fatal("revoked session final persisted")
		}
	}
}

type delayedTool struct{ start, release chan struct{} }

func (t *delayedTool) Name() string                          { return "delayed" }
func (t *delayedTool) Description() string                   { return "test" }
func (t *delayedTool) Parameters() shared.FunctionParameters { return nil }
func (t *delayedTool) Execute(context.Context, json.RawMessage) (string, error) {
	close(t.start)
	<-t.release
	return "secret late result", nil
}
func TestRunsCancellationFencesLateToolResult(t *testing.T) {
	h, _, model := runTestHub(t)
	a := runClient(h, "alice", "account-A", h.DefaultRoomID())
	r := enqueueTestRun(t, a, "request", false)
	call := nextCall(t, model)
	h.runMu.Lock()
	run := h.runs[r.ID]
	h.runMu.Unlock()
	slow := &delayedTool{start: make(chan struct{}), release: make(chan struct{})}
	tool := &runTool{Tool: slow, hub: h, run: run}
	done := make(chan error, 1)
	go func() {
		result, err := tool.Execute(context.Background(), nil)
		if result != "" {
			done <- errors.New("late result escaped")
			return
		}
		done <- err
	}()
	<-slow.start
	a.handleBotCancel(Message{RunID: r.ID})
	close(slow.release)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "run_inactive") {
		t.Fatalf("late result not fenced: %v", err)
	}
	finishCall(call, "late answer")
}

func TestRunsQueueBoundsAndExpiredSession(t *testing.T) {
	h, s, model := runTestHub(t)
	h.budget.limits.maxConcurrent = 1
	a := runClient(h, "alice", "account-A", h.DefaultRoomID())
	active := enqueueTestRun(t, a, "one", false)
	call := nextCall(t, model)
	second := enqueueTestRun(t, a, "two", false)
	enqueueTestRun(t, a, "three", false)
	drainEvents(a)
	trigger, _ := s.InsertMessage("alice", "four", "", a.getCurrentRoomID(), "", "", "")
	h.enqueueRun(a, trigger, false)
	events := drainEvents(a)
	if len(events) != 1 || events[0].ErrorCode != "RUN_QUEUE_FULL" {
		t.Fatalf("unbounded actor queue: %+v", events)
	}
	a.handleBotCancel(Message{RunID: second.ID})
	// Even with all execution slots occupied, cancelling a queued run frees its
	// actor queue capacity immediately.
	enqueueTestRun(t, a, "replacement after queued cancel", false)
	expired := runClient(h, "expired", "expired-sub", a.getCurrentRoomID())
	expired.authenticatedSessionExpiresAt = time.Now().Unix() - 1
	trigger, _ = s.InsertMessage("expired", "expired request", "", a.getCurrentRoomID(), "", "", "")
	h.enqueueRun(expired, trigger, false)
	events = drainEvents(expired)
	if len(events) != 1 || events[0].ErrorCode != "RUN_UNAVAILABLE" {
		t.Fatalf("expired run admitted: %+v", events)
	}
	a.handleBotCancel(Message{RunID: active.ID})
	finishCall(call, "late")
}

func TestRunsAccountTabsRemainRegistered(t *testing.T) {
	h, _, _ := runTestHub(t)
	go h.Run()
	room := h.DefaultRoomID()
	a := &Client{hub: h, username: "alice", authenticatedUsername: "alice", authenticatedSubject: "account-A", send: make(chan []byte, 20), currentRoomID: room}
	tab := &Client{hub: h, username: "alice", authenticatedUsername: "alice", authenticatedSubject: "account-A", send: make(chan []byte, 20), currentRoomID: room}
	h.register <- a
	h.register <- tab
	deadline := time.Now().Add(time.Second)
	for h.ConnectionCount() != 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if h.ConnectionCount() != 2 {
		t.Fatal("account tabs kicked each other")
	}
	for _, c := range []*Client{a, tab} {
		for _, e := range drainEvents(c) {
			if e.Type == "kicked" {
				t.Fatal("account tab kicked")
			}
		}
	}
}

func TestRunsInvalidQueueHeadDoesNotStrandSuccessor(t *testing.T) {
	h, s, model := runTestHub(t)
	room := h.DefaultRoomID()
	a := runClient(h, "alice", "account-A", room)
	b := runClient(h, "bob", "account-B", room)
	c := runClient(h, "carol", "account-C", room)
	enqueueTestRun(t, a, "active request", false)
	first := nextCall(t, model)
	expired := enqueueTestRun(t, b, "expires while queued", false)
	valid := enqueueTestRun(t, c, "valid successor", false)
	h.runMu.Lock()
	b.authenticatedSessionExpiresAt = time.Now().Unix() - 1
	h.runMu.Unlock()
	finishCall(first, "first answer")
	awaitStatus(t, s, expired.ID, "cancelled")
	// No new enqueue or watchdog is allowed to rescue the queue: completion of
	// the previous run must itself find and start the valid successor.
	successor := nextCall(t, model)
	if !strings.Contains(successor.input[len(successor.input)-1].Content, valid.TriggerMessageID) {
		t.Fatal("wrong successor started")
	}
	finishCall(successor, "successor answer")
	awaitStatus(t, s, valid.ID, "completed")
}

type logoutAfterFinalCommitStore struct {
	runStore
	revoke func() error
}

func (s logoutAfterFinalCommitStore) CompleteBotRun(id, content string, parts json.RawMessage, tokens int64, known bool) (StoredMessage, bool, error) {
	m, created, err := s.runStore.CompleteBotRun(id, content, parts, tokens, known)
	if err == nil && created {
		err = s.revoke()
	}
	return m, created, err
}

func TestRunsLogoutAfterCommitDeliversOnlySafeTerminal(t *testing.T) {
	for _, private := range []bool{false, true} {
		t.Run(map[bool]string{false: "public", true: "private"}[private], func(t *testing.T) {
			h, s, model := runTestHub(t)
			room := h.DefaultRoomID()
			a := runClient(h, "alice", "account-A", room)
			peer := runClient(h, "bob", "account-B", room)
			// A freshly authenticated generation may receive its own pure status,
			// but must not receive the revoked generation's result-bearing events.
			fresh := runClient(h, "alice", "account-A", room)
			fresh.authenticatedSessionGeneration = 1
			h.runStore = logoutAfterFinalCommitStore{runStore: h.runStore, revoke: func() error {
				now := time.Now()
				_, err := s.ApplyOIDCBackchannelLogout("account-A", "logout-after-commit", "hash", now, now.Add(time.Minute))
				return err
			}}
			r := enqueueTestRun(t, a, "original request", private)
			call := nextCall(t, model)
			if err := call.emit(llm.Event{Kind: "text", Text: "draft"}); err != nil {
				t.Fatal(err)
			}
			drainEvents(a)
			drainEvents(peer)
			drainEvents(fresh)
			finishCall(call, "committed answer")
			awaitStatus(t, s, r.ID, "completed")
			h.runMu.Lock()
			h.runMu.Unlock()
			observer := peer
			if private {
				observer = fresh
			}
			terminal := false
			for _, event := range drainEvents(observer) {
				if event.RunID != r.ID {
					continue
				}
				if event.Type == "message" || event.Type == "stream" || event.Content != "" || len(event.Parts) > 0 {
					t.Fatalf("revoked result escaped: %+v", event)
				}
				if event.Type == "bot_run" && event.Status == "completed" {
					terminal = true
				}
			}
			if !terminal {
				t.Fatal("committed run left authorized observer permanently running")
			}
			if private && len(drainEvents(peer)) != 0 {
				t.Fatal("private terminal leaked to another account")
			}
			for _, event := range drainEvents(a) {
				if private || event.Type != "bot_run" || event.Content != "" || len(event.Parts) > 0 {
					t.Fatal("revoked session received result-bearing or private data")
				}
			}
			stored, err := s.GetMessageByID(r.MessageID)
			if err != nil || stored.Content != "committed answer" {
				t.Fatal("already committed history was removed")
			}
		})
	}
}

func TestSendMessageToolPostsBoundedInterimMessages(t *testing.T) {
	h, s, model := runTestHub(t)
	room := h.DefaultRoomID()
	a := runClient(h, "alice", "account-A", room)
	a.handleChatMessage(Message{Content: "work on a long task", To: "TokenBot"})
	call := nextCall(t, model)
	drainEvents(a)
	tool := toolNamed(t, call.tools, "send_message")
	for _, content := range []string{"working on it", "halfway there"} {
		args, _ := json.Marshal(map[string]string{"content": content})
		if _, err := tool.Execute(context.Background(), args); err != nil {
			t.Fatalf("interim %q: %v", content, err)
		}
	}
	args, _ := json.Marshal(map[string]string{"content": "third update"})
	if _, err := tool.Execute(context.Background(), args); err == nil {
		t.Fatal("expected the third interim message to be rejected")
	}
	seen := map[string]bool{}
	for _, e := range drainEvents(a) {
		if e.Type == "message" && e.Private && e.RunID == "" {
			seen[e.Content] = true
		}
	}
	if !seen["working on it"] || !seen["halfway there"] {
		t.Fatalf("interim messages were not delivered: %+v", seen)
	}
	rows, err := s.RecentBotRuns("", a.actorID(), 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("runs: %v %v", rows, err)
	}
	finishCall(call, "final answer")
	awaitStatus(t, s, rows[0].ID, "completed")
	history := s.GetMessagesBetween("alice", "TokenBot", 20)
	interims, finals := 0, 0
	for _, m := range history {
		if m.Username != "TokenBot" {
			continue
		}
		switch m.Content {
		case "working on it", "halfway there":
			interims++
		case "final answer":
			finals++
		}
	}
	if interims != 2 || finals != 1 {
		t.Fatalf("history interims=%d finals=%d: %+v", interims, finals, history)
	}
}

func TestSendMessageToolErrorsAreCoded(t *testing.T) {
	h, s, model := runTestHub(t)
	a := runClient(h, "alice", "account-A", h.DefaultRoomID())
	a.handleChatMessage(Message{Content: "long task", To: "TokenBot"})
	call := nextCall(t, model)
	tool := toolNamed(t, call.tools, "send_message")

	cases := []struct{ name, args, code string }{
		{"invalid json", `{"content":`, "invalid_arguments"},
		{"missing content", `{}`, "missing_content"},
		{"blank content", `{"content":"  "}`, "missing_content"},
		{"too long", `{"content":"` + strings.Repeat("x", 601) + `"}`, "content_too_long"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tool.Execute(context.Background(), json.RawMessage(tc.args)); err == nil || !strings.Contains(err.Error(), tc.code+":") {
				t.Fatalf("err = %v, want a coded %s error", err, tc.code)
			}
		})
	}
	// The length rejection must tell the model both the actual and the cap.
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"content":"`+strings.Repeat("x", 601)+`"}`))
	if err == nil || !strings.Contains(err.Error(), "601") || !strings.Contains(err.Error(), "600") {
		t.Fatalf("length error not actionable: %v", err)
	}
	// A private run allows exactly two interim messages; the third is terminal
	// and tells the model not to retry.
	for _, content := range []string{"one", "two"} {
		args, _ := json.Marshal(map[string]string{"content": content})
		if _, err := tool.Execute(context.Background(), args); err != nil {
			t.Fatalf("interim %q: %v", content, err)
		}
	}
	args, _ := json.Marshal(map[string]string{"content": "three"})
	if _, err := tool.Execute(context.Background(), args); err == nil || !strings.Contains(err.Error(), "interim_limit:") {
		t.Fatalf("third interim: %v", err)
	}
	// After cancellation the tool returns a readable stop signal, not a bare
	// context error.
	rows, err := s.RecentBotRuns("", a.actorID(), 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("runs: %v %v", rows, err)
	}
	a.handleBotCancel(Message{RunID: rows[0].ID})
	if _, err := tool.Execute(context.Background(), args); err == nil || !strings.Contains(err.Error(), "run_inactive:") {
		t.Fatalf("cancelled run accepted an interim message: %v", err)
	}
	finishCall(call, "late final")
	awaitStatus(t, s, rows[0].ID, "cancelled")
}

// A burst final must reach the requester as one done (first segment) plus one
// message event per segment, all addressed and private — nothing less renders,
// nothing more leaks.
func TestRunsBurstFinalBroadcastsAllSegments(t *testing.T) {
	h, s, model := runTestHub(t)
	room := h.DefaultRoomID()
	a := runClient(h, "alice", "account-A", room)
	b := runClient(h, "bob", "account-B", room)
	a.handleChatMessage(Message{Content: "burst please", To: "TokenBot"})
	call := nextCall(t, model)
	rows, _ := s.RecentBotRuns("", a.actorID(), 10)
	if len(rows) != 1 {
		t.Fatal("missing private run")
	}
	r := rows[0]
	drainEvents(a)
	drainEvents(b)
	finishCall(call, "第一段<msg-break>第二段<msg-break>第三段")
	awaitStatus(t, s, r.ID, "completed")
	// The burst broadcasts while holding runMu; taking it once proves every
	// event has been queued before we drain.
	h.runMu.Lock()
	h.runMu.Unlock()
	events := drainEvents(a)
	var done, messages int
	ids := map[string]bool{}
	for _, e := range events {
		switch e.Type {
		case "stream":
			if e.Kind == "done" {
				done++
				if e.MessageID != r.MessageID {
					t.Fatalf("done must reference the first segment: %+v", e)
				}
			}
		case "message":
			messages++
			ids[e.ID] = true
			if !e.Private || e.To != "alice" {
				t.Fatalf("burst segment leaked audience: %+v", e)
			}
		}
	}
	if done != 1 || messages != 3 || len(ids) != 3 {
		t.Fatalf("burst events: done=%d messages=%d unique=%d", done, messages, len(ids))
	}
	if len(drainEvents(b)) != 0 {
		t.Fatal("burst leaked to another account")
	}
	stored := s.GetMessagesBetween("alice", "TokenBot", 10)
	botCount := 0
	for _, m := range stored {
		if m.Username == "TokenBot" {
			botCount++
		}
	}
	if botCount != 3 {
		t.Fatalf("stored burst = %d, want 3", botCount)
	}
}

// Guests exist only on their WS connection, so private history must load over
// WS too: load_private returns exactly the caller's thread with the peer.
func TestLoadPrivateHistoryScopesToCaller(t *testing.T) {
	h, s, _ := runTestHub(t)
	room := h.DefaultRoomID()
	a := runClient(h, "alice", "account-A", room)
	b := runClient(h, "bob", "account-B", room)
	if _, err := s.InsertMessage("alice", "我的私聊内容", "", "", "TokenBot", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertMessage("TokenBot", "给 alice 的回复", "", "", "alice", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertMessage("bob", "bob 的私聊", "", "", "TokenBot", "", ""); err != nil {
		t.Fatal(err)
	}
	drainEvents(a)
	drainEvents(b)
	a.handleLoadPrivateHistory(Message{To: "TokenBot"})
	events := drainEvents(a)
	if len(events) != 1 || events[0].Type != "private_history" {
		t.Fatalf("expected one private_history, got %+v", events)
	}
	if len(events[0].Messages) != 2 {
		t.Fatalf("alice thread = %d messages, want 2", len(events[0].Messages))
	}
	for _, m := range events[0].Messages {
		if m.Username == "bob" {
			t.Fatal("cross-tenant leak into alice thread")
		}
	}
	// bob asking for alice's thread gets his own, never alice's.
	b.handleLoadPrivateHistory(Message{To: "TokenBot"})
	bEvents := drainEvents(b)
	if len(bEvents) != 1 || len(bEvents[0].Messages) != 1 || bEvents[0].Messages[0].Username != "bob" {
		t.Fatalf("bob thread wrong: %+v", bEvents)
	}
	// Empty/self targets are ignored.
	drainEvents(a)
	a.handleLoadPrivateHistory(Message{To: ""})
	a.handleLoadPrivateHistory(Message{To: "alice"})
	if len(drainEvents(a)) != 0 {
		t.Fatal("invalid load_private produced output")
	}
}
