package hub

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"tokendancechat/backend/llm"
	"tokendancechat/backend/store"
)

func TestParseAmbientDecision(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		action string
	}{
		{"plain reply", `{"action":"reply","reason":"direct question"}`, "reply"},
		{"fenced", "```json\n{\"action\":\"reply\",\"reason\":\"needs help\"}\n```", "reply"},
		{"fenced without tag", "```\n{\"action\":\"reply\",\"reason\":\"needs help\"}\n```", "reply"},
		{"prose around", `Decision: {"action":"ignore","reason":"small talk"} done`, "ignore"},
		{"unknown action", `{"action":"maybe"}`, "ignore"},
		{"unparseable", `not json`, "ignore"},
		{"uppercase action", `{"action":"REPLY","reason":"question"}`, "reply"},
		{"whitespace action", `{"action":"  Reply  ","reason":"question"}`, "reply"},
		{"plain reply word", `I think TokenBot should reply here.`, "reply"},
		{"plain ignore word", `This is small talk, ignore it.`, "ignore"},
		{"negated reply", `I will not reply to this message.`, "ignore"},
		{"negated should not", `TokenBot should not reply here, it is small talk.`, "ignore"},
		{"negated no reply", `No reply needed, the room handled it.`, "ignore"},
		{"stay silent", `Best to stay silent here.`, "ignore"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			action, reason := parseAmbientDecision(tc.raw)
			if action != tc.action {
				t.Fatalf("action = %q, want %q", action, tc.action)
			}
			if reason == "" {
				t.Fatal("reason must never be empty")
			}
		})
	}
}

func TestAmbientEligible(t *testing.T) {
	tests := []struct {
		name    string
		trigger StoredMessage
		want    bool
	}{
		{"plain question", StoredMessage{Username: "alice", Content: "有谁知道 Go 1.26 什么时候发布？", RoomID: "r"}, true},
		{"bot never wakes itself", StoredMessage{Username: "TokenBot", Content: "这是一个足够长的回复", RoomID: "r"}, false},
		{"system row is not conversation", StoredMessage{Username: "system", Content: "user joined the room", RoomID: "r"}, false},
		{"thread reply", StoredMessage{Username: "alice", Content: "线程里的追问内容", RoomID: "r", ThreadID: "t1"}, false},
		{"too short", StoredMessage{Username: "alice", Content: "好的", RoomID: "r"}, false},
		{"image only", StoredMessage{Username: "alice", Content: "![image](/uploads/images/a.png)", RoomID: "r"}, false},
		{"multiple images only", StoredMessage{Username: "alice", Content: "![a](/uploads/images/a.png) ![b](/uploads/images/b.png)", RoomID: "r"}, false},
		{"image with real text", StoredMessage{Username: "alice", Content: "![image](/uploads/images/a.png) 这个报错是什么意思？", RoomID: "r"}, true},
		{"addressed to another person", StoredMessage{Username: "alice", Content: "@bob 你觉得这个方案怎么样", RoomID: "r"}, false},
		{"room-wide mention is everyone", StoredMessage{Username: "alice", Content: "@all 有谁会修这个编译错误", RoomID: "r"}, true},
		{"mention plus question to room", StoredMessage{Username: "alice", Content: "@here 有人遇到过这个 panic 吗", RoomID: "r"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ambientEligible(tc.trigger, "TokenBot"); got != tc.want {
				t.Fatalf("ambientEligible = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBuildAmbientTranscript(t *testing.T) {
	long := strings.Repeat("字", 300)
	// Store contract: newest first.
	messages := []StoredMessage{
		{ID: "m5", Username: "alice", Content: "最新问题是什么", RoomID: "r"},
		{ID: "m4", Username: "bob", Content: long, RoomID: "r"},
		{ID: "m3", Username: "bob", Content: "引用回复内容", ReplyToID: "m1", RoomID: "r"},
		{ID: "m2", Username: "system", Content: "alice joined", RoomID: "r"},
		{ID: "m1", Username: "alice", Content: "最早的话题", RoomID: "r"},
		{ID: "m0", Username: "carol", Content: "已删除的话", RoomID: "r", Deleted: true},
	}
	trigger := StoredMessage{ID: "m6", Username: "dave", Content: "  谁能解释下这个报错  ", RoomID: "r"}
	lines := buildAmbientTranscript(messages, trigger, "TokenBot")

	want := []string{
		"alice: 最早的话题",
		"bob (replying to alice): 引用回复内容",
		"bob: " + strings.Repeat("字", 280) + "…",
		"alice: 最新问题是什么",
		"LATEST (by dave): 谁能解释下这个报错",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestBuildAmbientTranscriptSkipsDuplicateTrigger(t *testing.T) {
	// The trigger is already stored when evaluation starts; it must appear once,
	// as the LATEST line, not twice.
	messages := []StoredMessage{
		{ID: "m2", Username: "alice", Content: "这是触发消息本身", RoomID: "r"},
		{ID: "m1", Username: "bob", Content: "之前的消息", RoomID: "r"},
	}
	trigger := StoredMessage{ID: "m2", Username: "alice", Content: "这是触发消息本身", RoomID: "r"}
	lines := buildAmbientTranscript(messages, trigger, "TokenBot")
	if len(lines) != 2 {
		t.Fatalf("lines = %v, want 2 entries", lines)
	}
	if !strings.HasPrefix(lines[1], "LATEST (by alice)") {
		t.Fatalf("last line must be the marked trigger, got %q", lines[1])
	}
}

func newAmbientTestController(enabled bool, cooldown time.Duration, perHour int) ambientController {
	return ambientController{
		policy:  ambientPolicy{enabled: enabled, cooldown: cooldown, perHour: perHour, timeout: 10 * time.Second, quiet: 8 * time.Second, maxWait: 30 * time.Second},
		last:    make(map[string]time.Time),
		replies: make(map[string][]time.Time),
		pending: make(map[string]*ambientPending),
	}
}

func TestReserveAmbientSlotHonorsCooldownAndPacing(t *testing.T) {
	h := &Hub{
		ambient:   newAmbientTestController(true, time.Minute, 2),
		runs:      map[string]*botRun{},
		runQueues: map[string]*conversationQueue{},
	}
	if !h.reserveAmbientSlot("room") {
		t.Fatal("first slot should be reserved")
	}
	h.ambient.mu.Lock()
	h.ambient.inFlight = false
	h.ambient.mu.Unlock()
	if h.reserveAmbientSlot("room") {
		t.Fatal("cooldown must block a second slot in the same room")
	}

	h.ambient.mu.Lock()
	h.ambient.last["room"] = time.Now().Add(-2 * time.Minute)
	h.ambient.replies["room"] = []time.Time{time.Now(), time.Now()}
	h.ambient.mu.Unlock()
	if h.reserveAmbientSlot("room") {
		t.Fatal("hourly pacing must block once perHour is reached")
	}
}

func TestReserveAmbientSlotIsPerRoom(t *testing.T) {
	h := &Hub{
		ambient:   newAmbientTestController(true, time.Minute, 1),
		runs:      map[string]*botRun{},
		runQueues: map[string]*conversationQueue{},
	}
	if !h.reserveAmbientSlot("busy") {
		t.Fatal("first slot should be reserved")
	}
	h.ambient.mu.Lock()
	h.ambient.inFlight = false
	h.ambient.replies["busy"] = []time.Time{time.Now()} // exhaust busy's budget
	h.ambient.mu.Unlock()

	if h.reserveAmbientSlot("busy") {
		t.Fatal("busy room exhausted its hourly budget")
	}
	if !h.reserveAmbientSlot("quiet") {
		t.Fatal("another room must keep its own cooldown and budget")
	}
}

func TestReserveAmbientSlotYieldsToActiveRun(t *testing.T) {
	h := &Hub{
		ambient:   newAmbientTestController(true, 0, 10),
		runs:      map[string]*botRun{"active": {mode: "public", record: store.BotRun{ID: "active", RoomID: "room", Status: "running"}}},
		runQueues: map[string]*conversationQueue{},
	}
	if h.reserveAmbientSlot("room") {
		t.Fatal("an active public run must own the room")
	}
}

func TestAmbientFailureBackoff(t *testing.T) {
	h := &Hub{
		ambient:   newAmbientTestController(true, time.Minute, 6),
		runs:      map[string]*botRun{},
		runQueues: map[string]*conversationQueue{},
	}
	// A reserved slot exists before the failing evaluation.
	if !h.reserveAmbientSlot("room") {
		t.Fatal("first slot should be reserved")
	}
	h.ambient.mu.Lock()
	h.ambient.inFlight = false
	h.ambient.mu.Unlock()

	h.noteAmbientFailure("room", fmt.Errorf("boom"))

	h.ambient.mu.Lock()
	if h.ambient.failures != 1 {
		t.Fatalf("failures = %d, want 1", h.ambient.failures)
	}
	backoff := time.Until(h.ambient.backoffUntil)
	if backoff < 50*time.Second || backoff > 70*time.Second {
		t.Fatalf("first failure backoff = %v, want ~1m", backoff)
	}
	if _, ok := h.ambient.last["room"]; ok {
		t.Fatal("failed decisions must refund the room cooldown")
	}
	h.ambient.mu.Unlock()

	if h.reserveAmbientSlot("room") {
		t.Fatal("backoff gate must block retries even though the cooldown was refunded")
	}

	// Consecutive failures grow exponentially and are capped.
	h.noteAmbientFailure("room", fmt.Errorf("boom"))
	h.noteAmbientFailure("room", fmt.Errorf("boom"))
	h.ambient.mu.Lock()
	got := time.Until(h.ambient.backoffUntil)
	h.ambient.mu.Unlock()
	if got < 3*time.Minute || got > 5*time.Minute {
		t.Fatalf("third consecutive failure backoff = %v, want ~4m", got)
	}

	h.ambient.mu.Lock()
	h.ambient.failures = 20
	h.ambient.mu.Unlock()
	h.noteAmbientFailure("room", fmt.Errorf("boom"))
	h.ambient.mu.Lock()
	got = time.Until(h.ambient.backoffUntil)
	h.ambient.mu.Unlock()
	if got > ambientMaxBackoff {
		t.Fatalf("backoff must be capped at %v, got %v", ambientMaxBackoff, got)
	}

	// A success clears the gate.
	h.noteAmbientSuccess()
	h.ambient.mu.Lock()
	if h.ambient.failures != 0 || !h.ambient.backoffUntil.IsZero() {
		t.Fatal("success must reset failures and the backoff gate")
	}
	h.ambient.mu.Unlock()
	if !h.reserveAmbientSlot("room") {
		t.Fatal("reserve must work again after the gate clears")
	}
}

func TestMaybeAmbientReplyDisabledDoesNothing(t *testing.T) {
	h := &Hub{ambient: newAmbientTestController(false, time.Minute, 6)}
	trigger := StoredMessage{Username: "alice", Content: "a real question", RoomID: "room"}
	h.maybeAmbientReply(nil, trigger)
	h.ambient.mu.Lock()
	defer h.ambient.mu.Unlock()
	if h.ambient.inFlight {
		t.Fatal("disabled ambient mode must not create work")
	}
}

// TestMaybeAmbientReplyPrefilters proves the deterministic filter drops
// messages before any slot is reserved or any model token is spent.
func TestMaybeAmbientReplyPrefilters(t *testing.T) {
	t.Setenv("CHAT_BOT_AMBIENT", "true")
	s, err := store.New(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer s.Close()
	// No BaseURL: any accidental judge call would fail fast, and the assertions
	// below prove no slot was even reserved.
	h := New(s, &llm.Config{Provider: "openai", APIKey: "test", Model: "stub", Timeout: time.Second}, "TokenBot")
	defer h.Stop()

	triggers := []StoredMessage{
		{Username: "TokenBot", Content: "机器人自己的发言不应触发", RoomID: "room"},
		{Username: "system", Content: "system notice here", RoomID: "room"},
		{Username: "alice", Content: "线程里的正常问题内容", RoomID: "room", ThreadID: "t1"},
		{Username: "alice", Content: "嗯嗯", RoomID: "room"},
		{Username: "alice", Content: "![image](/uploads/images/a.png)", RoomID: "room"},
		{Username: "alice", Content: "@bob 这个问题你怎么看呀", RoomID: "room"},
	}
	for _, trigger := range triggers {
		h.maybeAmbientReply(nil, trigger)
	}
	h.ambient.mu.Lock()
	defer h.ambient.mu.Unlock()
	if h.ambient.inFlight {
		t.Fatal("pre-filtered messages must not create work")
	}
	if len(h.ambient.last) != 0 {
		t.Fatalf("pre-filtered messages must not consume cooldown, got %v", h.ambient.last)
	}
	if len(h.ambient.pending) != 0 {
		t.Fatal("pre-filtered messages must not open a debounce window")
	}
}

// ambientStubLLM serves a fixed non-streaming chat completion response.
func ambientStubLLM(t *testing.T, status int, content string) *httptest.Server {
	t.Helper()
	srv, _ := ambientCountingStubLLM(t, status, content)
	return srv
}

// ambientStubCounter separates judge calls (non-streaming) from run execution
// calls (streaming) hitting the same stub endpoint.
type ambientStubCounter struct {
	judge  atomic.Int64
	stream atomic.Int64
}

func ambientCountingStubLLM(t *testing.T, status int, content string) (*httptest.Server, *ambientStubCounter) {
	t.Helper()
	counter := &ambientStubCounter{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"stream":true`) {
			counter.stream.Add(1)
		} else {
			counter.judge.Add(1)
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			fmt.Fprint(w, `{"error":{"message":"boom"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`, mustJSONString(t, content))
	}))
	return srv, counter
}

func mustJSONString(t *testing.T, s string) string {
	t.Helper()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

// newAmbientJudgeHub builds a hub with a real SQLite store and a stubbed
// model, with ambient mode enabled, for judge-path tests.
func newAmbientJudgeHub(t *testing.T, stub *httptest.Server) (*Hub, *store.Store) {
	t.Helper()
	t.Setenv("CHAT_BOT_AMBIENT", "true")
	s, err := store.New(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	h := New(s, &llm.Config{
		Provider: "openai",
		APIKey:   "test",
		Model:    "stub",
		BaseURL:  stub.URL,
		Timeout:  5 * time.Second,
	}, "TokenBot")
	t.Cleanup(func() {
		h.Stop()
		s.Close()
	})
	return h, s
}

func ambientTestClient(h *Hub, username, roomID string) *Client {
	return &Client{hub: h, username: username, send: make(chan []byte, 256), currentRoomID: roomID}
}

func TestEvaluateAmbientReplyEnqueuesRun(t *testing.T) {
	stub, counter := ambientCountingStubLLM(t, http.StatusOK, `{"action":"reply","reason":"direct question to the room"}`)
	defer stub.Close()
	h, s := newAmbientJudgeHub(t, stub)

	roomID, err := s.CreateRoom("general")
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	trigger, err := s.InsertMessage("alice", "有谁知道 Go 1.26 的发布日期吗", "", roomID, "", "", "")
	if err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}

	h.evaluateAmbient(ambientTestClient(h, "alice", roomID), trigger)

	// enqueueRun persists the run synchronously; the in-memory map entry is
	// transient because the scheduler executes runs right away.
	runs, err := s.RecentBotRuns(roomID, "", 10)
	if err != nil {
		t.Fatalf("RecentBotRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("a clear question must enqueue exactly one run, got %d", len(runs))
	}
	if runs[0].RequestedBy != "alice" || runs[0].RoomID != roomID {
		t.Fatalf("run owner = %q room = %q, want alice/%s", runs[0].RequestedBy, runs[0].RoomID, roomID)
	}
	if got := counter.judge.Load(); got != 1 {
		t.Fatalf("judge calls = %d, want 1", got)
	}
	h.ambient.mu.Lock()
	defer h.ambient.mu.Unlock()
	if got := len(h.ambient.replies[roomID]); got != 1 {
		t.Fatalf("accepted reply must consume one hourly quota slot, got %d", got)
	}
	if h.ambient.failures != 0 {
		t.Fatalf("successful judge must keep failures at 0, got %d", h.ambient.failures)
	}
}

func TestEvaluateAmbientIgnoreStaysSilent(t *testing.T) {
	stub := ambientStubLLM(t, http.StatusOK, `{"action":"ignore","reason":"casual chatter between people"}`)
	defer stub.Close()
	h, s := newAmbientJudgeHub(t, stub)

	roomID, err := s.CreateRoom("general")
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	trigger, err := s.InsertMessage("alice", "哈哈哈这个段子太逗了", "", roomID, "", "", "")
	if err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}

	h.evaluateAmbient(ambientTestClient(h, "alice", roomID), trigger)

	h.runMu.Lock()
	defer h.runMu.Unlock()
	if len(h.runs) != 0 {
		t.Fatalf("ignored messages must not enqueue runs, got %d", len(h.runs))
	}
	h.ambient.mu.Lock()
	defer h.ambient.mu.Unlock()
	if got := len(h.ambient.replies[roomID]); got != 0 {
		t.Fatalf("ignored decisions must not consume reply quota, got %d", got)
	}
}

func TestEvaluateAmbientFailureBacksOffWithoutQuota(t *testing.T) {
	stub := ambientStubLLM(t, http.StatusInternalServerError, "")
	defer stub.Close()
	h, s := newAmbientJudgeHub(t, stub)

	roomID, err := s.CreateRoom("general")
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	trigger, err := s.InsertMessage("alice", "有人能帮忙看看这个问题吗", "", roomID, "", "", "")
	if err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}
	// Reserve the way maybeAmbientReply does, then evaluate synchronously.
	if !h.reserveAmbientSlot(roomID) {
		t.Fatal("slot should be reserved")
	}
	h.evaluateAmbient(ambientTestClient(h, "alice", roomID), trigger)

	h.ambient.mu.Lock()
	defer h.ambient.mu.Unlock()
	if h.ambient.failures != 1 {
		t.Fatalf("failures = %d, want 1", h.ambient.failures)
	}
	if time.Until(h.ambient.backoffUntil) <= 0 {
		t.Fatal("a failure must arm the backoff gate")
	}
	if _, ok := h.ambient.last[roomID]; ok {
		t.Fatal("a failed judge must refund the room cooldown")
	}
	if len(h.ambient.replies[roomID]) != 0 {
		t.Fatal("a failed judge must not consume reply quota")
	}
}

// TestAmbientDebounceCollectsBurst proves a burst of messages collapses into
// exactly one decision, one run and one quota tick — the reply anchors the
// latest message of the batch.
func TestAmbientDebounceCollectsBurst(t *testing.T) {
	stub, counter := ambientCountingStubLLM(t, http.StatusOK, `{"action":"reply","reason":"question in the burst"}`)
	defer stub.Close()
	h, s := newAmbientJudgeHub(t, stub)
	h.ambient.policy.quiet = 40 * time.Millisecond
	h.ambient.policy.maxWait = 5 * time.Second

	roomID, err := s.CreateRoom("general")
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	authors := []string{"alice", "bob", "carol"}
	var last StoredMessage
	for i, author := range authors {
		m, err := s.InsertMessage(author, fmt.Sprintf("连续发言第%d条这个问题到底怎么解决", i), "", roomID, "", "", "")
		if err != nil {
			t.Fatalf("InsertMessage: %v", err)
		}
		last = m
		h.maybeAmbientReply(ambientTestClient(h, author, roomID), m)
	}

	h.ambient.mu.Lock()
	if len(h.ambient.pending) != 1 {
		t.Fatalf("burst must open exactly one window, got %d", len(h.ambient.pending))
	}
	p := h.ambient.pending[roomID]
	if p == nil || p.trigger.ID != last.ID {
		t.Fatal("the window must anchor the latest message of the burst")
	}
	if len(h.ambient.last) != 0 {
		t.Fatal("an open debounce window must not consume cooldown yet")
	}
	if h.ambient.inFlight {
		t.Fatal("an open debounce window must not start a judge call")
	}
	h.ambient.mu.Unlock()

	run := waitForAmbientRunRecord(t, s, roomID)
	if run.RequestedBy != "carol" {
		t.Fatalf("the reply must anchor the latest speaker of the burst, got %q", run.RequestedBy)
	}
	if got := counter.judge.Load(); got != 1 {
		t.Fatalf("a 3-message burst must cost exactly one judge call, got %d", got)
	}

	h.ambient.mu.Lock()
	defer h.ambient.mu.Unlock()
	if got := len(h.ambient.replies[roomID]); got != 1 {
		t.Fatalf("a 3-message burst must cost exactly one quota tick, got %d", got)
	}
}

// TestAmbientDebounceMaxDeadlinePinned proves the hard deadline is anchored at
// the first message of the burst and does not move when more messages arrive.
func TestAmbientDebounceMaxDeadlinePinned(t *testing.T) {
	stub := ambientStubLLM(t, http.StatusOK, `{"action":"ignore","reason":"never judged in this test"}`)
	defer stub.Close()
	h, _ := newAmbientJudgeHub(t, stub)
	h.ambient.policy.quiet = time.Hour // never fires on its own in this test
	h.ambient.policy.maxWait = time.Hour
	trigger := StoredMessage{Username: "alice", Content: "第一条消息内容", RoomID: "room"}
	h.maybeAmbientReply(nil, trigger)

	h.ambient.mu.Lock()
	first := h.ambient.pending["room"].maxDeadline
	h.ambient.mu.Unlock()

	h.maybeAmbientReply(nil, StoredMessage{Username: "bob", Content: "第二条消息内容", RoomID: "room"})
	h.maybeAmbientReply(nil, StoredMessage{Username: "carol", Content: "第三条消息内容", RoomID: "room"})

	h.ambient.mu.Lock()
	defer h.ambient.mu.Unlock()
	p := h.ambient.pending["room"]
	if !p.maxDeadline.Equal(first) {
		t.Fatalf("maxDeadline moved from %v to %v; busy rooms must not starve the judge", first, p.maxDeadline)
	}
	if p.trigger.Content != "第三条消息内容" {
		t.Fatalf("window must anchor the latest message, got %q", p.trigger.Content)
	}
}

// TestAmbientDebounceMaxWaitForcesFire proves the hard deadline caps the quiet
// window: with an effectively infinite quiet period the batch is still judged
// once maxWait elapses.
func TestAmbientDebounceMaxWaitForcesFire(t *testing.T) {
	stub := ambientStubLLM(t, http.StatusOK, `{"action":"reply","reason":"forced by max wait"}`)
	defer stub.Close()
	h, s := newAmbientJudgeHub(t, stub)
	h.ambient.policy.quiet = time.Hour
	h.ambient.policy.maxWait = 50 * time.Millisecond

	roomID, err := s.CreateRoom("general")
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	m, err := s.InsertMessage("alice", "这条消息需要等到聚合上限才判断", "", roomID, "", "", "")
	if err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}
	h.maybeAmbientReply(ambientTestClient(h, "alice", roomID), m)

	waitForAmbientRunRecord(t, s, roomID)
}

// TestAmbientDebounceWindowsArePerRoom proves concurrent bursts in two rooms
// keep independent windows and independent budgets.
func TestAmbientDebounceWindowsArePerRoom(t *testing.T) {
	stub := ambientStubLLM(t, http.StatusOK, `{"action":"ignore","reason":"never judged in this test"}`)
	defer stub.Close()
	h, _ := newAmbientJudgeHub(t, stub)
	h.ambient.policy.quiet = time.Hour
	h.maybeAmbientReply(nil, StoredMessage{Username: "alice", Content: "房间A的讨论内容", RoomID: "roomA"})
	h.maybeAmbientReply(nil, StoredMessage{Username: "bob", Content: "房间B的讨论内容", RoomID: "roomB"})

	h.ambient.mu.Lock()
	defer h.ambient.mu.Unlock()
	if len(h.ambient.pending) != 2 {
		t.Fatalf("two rooms must hold two independent windows, got %d", len(h.ambient.pending))
	}
}

// TestAmbientFireSinksWhenCoolingDown proves a batch that matures during the
// room cooldown sinks quietly instead of queueing a judge call — the next
// eligible message simply opens a fresh window.
func TestAmbientFireSinksWhenCoolingDown(t *testing.T) {
	h := &Hub{
		ambient:   newAmbientTestController(true, time.Minute, 6),
		runs:      map[string]*botRun{},
		runQueues: map[string]*conversationQueue{},
	}
	h.ambient.mu.Lock()
	h.ambient.last["room"] = time.Now()
	h.ambient.pending["room"] = &ambientPending{
		trigger: StoredMessage{Username: "alice", Content: "冷却期间成熟的一批消息", RoomID: "room"},
	}
	h.ambient.mu.Unlock()

	h.fireAmbient("room")

	h.ambient.mu.Lock()
	defer h.ambient.mu.Unlock()
	if len(h.ambient.pending) != 0 {
		t.Fatal("the fired window must be closed regardless of the outcome")
	}
	if h.ambient.inFlight {
		t.Fatal("a cooled-down room must not start a judge call")
	}
}

// TestAmbientFireSinksWhenRunIsActive proves explicit work wins: if a run owns
// the room when the window matures, the batch sinks.
func TestAmbientFireSinksWhenRunIsActive(t *testing.T) {
	h := &Hub{
		ambient:   newAmbientTestController(true, 0, 10),
		runs:      map[string]*botRun{"active": {mode: "public", record: store.BotRun{ID: "active", RoomID: "room", Status: "running"}}},
		runQueues: map[string]*conversationQueue{},
	}
	h.ambient.mu.Lock()
	h.ambient.pending["room"] = &ambientPending{
		trigger: StoredMessage{Username: "alice", Content: "显式请求进行中成熟的一批消息", RoomID: "room"},
	}
	h.ambient.mu.Unlock()

	h.fireAmbient("room")

	h.ambient.mu.Lock()
	defer h.ambient.mu.Unlock()
	if h.ambient.inFlight {
		t.Fatal("an active explicit run must suppress the ambient batch")
	}
	if len(h.ambient.replies["room"]) != 0 {
		t.Fatal("a sunk batch must not consume reply quota")
	}
}

// TestAmbientStopCancelsPending proves shutdown drops open windows without
// judging them.
func TestAmbientStopCancelsPending(t *testing.T) {
	stub := ambientStubLLM(t, http.StatusOK, `{"action":"reply","reason":"should never fire"}`)
	defer stub.Close()
	t.Setenv("CHAT_BOT_AMBIENT", "true")
	s, err := store.New(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer s.Close()
	h := New(s, &llm.Config{Provider: "openai", APIKey: "test", Model: "stub", BaseURL: stub.URL, Timeout: time.Second}, "TokenBot")
	h.ambient.policy.quiet = time.Hour

	h.maybeAmbientReply(nil, StoredMessage{Username: "alice", Content: "停机前尚未成熟的消息", RoomID: "room"})
	h.Stop()

	h.ambient.mu.Lock()
	defer h.ambient.mu.Unlock()
	if len(h.ambient.pending) != 0 {
		t.Fatal("Stop must close every open debounce window")
	}
	if h.ambient.inFlight {
		t.Fatal("Stop must not leave a judge call running from a cancelled window")
	}
}

// waitForAmbientRunRecord waits until the debounced batch has persisted its
// run. The in-memory run entry is transient (the scheduler executes and
// finishes it right away), so the durable record is the stable assertion.
func waitForAmbientRunRecord(t *testing.T, s *store.Store, roomID string) store.BotRun {
	t.Helper()
	// Generous deadline: the fire path is a timer plus a stub judge call plus a
	// durable enqueue, and a loaded CI host can stretch that chain well past a
	// few seconds without anything being wrong.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := s.RecentBotRuns(roomID, "", 10)
		if err != nil {
			t.Fatalf("RecentBotRuns: %v", err)
		}
		if len(runs) == 1 {
			return runs[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the debounced batch never produced a run")
	return store.BotRun{}
}
