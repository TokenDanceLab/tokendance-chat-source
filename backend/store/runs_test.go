package store

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func testBotRun(t *testing.T, s *Store, actor, day string, reserve int64) BotRun {
	t.Helper()
	room, err := s.GetRoomID("公共聊天")
	if err != nil {
		t.Fatal(err)
	}
	trigger, err := s.InsertMessage("alice", "test trigger", "", room, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return BotRun{ID: uuid.NewString(), MessageID: uuid.NewString(), TriggerMessageID: trigger.ID, ActorID: actor, RequestedBy: "alice", RoomID: room, BotName: "TokenBot", Status: "queued", BudgetDay: day, IPKey: "127.0.0.1", Input: json.RawMessage(`[]`), ReservedTokens: reserve, CreatedAt: time.Now().UnixMilli()}
}
func newRunStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("CHAT_SKIP_SEED", "true")
	s, err := New(filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func admitRun(t *testing.T, s *Store, r BotRun, limits RunBudgetLimits) {
	t.Helper()
	reason, err := s.CreateBotRun(r, limits)
	if err != nil || reason != "" {
		t.Fatalf("admission %q %v", reason, err)
	}
}

func TestBotRunIdempotentFinalAndStableActor(t *testing.T) {
	s := newRunStore(t)
	r := testBotRun(t, s, "oidc:stable-account", "2026-10-21", 1000)
	admitRun(t, s, r, RunBudgetLimits{})
	if reason, err := s.CreateBotRun(r, RunBudgetLimits{}); err != nil || reason != "duplicate" {
		t.Fatalf("admission retry: %q %v", reason, err)
	}
	if err := s.StartBotRun(r.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		m, created, err := s.CompleteBotRun(r.ID, "answer", json.RawMessage(`[{"type":"reasoning","text":"secret"},{"type":"text","text":"answer","result":"secret"}]`), 30, true)
		if err != nil {
			t.Fatal(err)
		}
		if created != (i == 0) || m.ID != r.MessageID || m.RunID != r.ID {
			t.Fatalf("delivery retry %d: %+v %v", i, m, created)
		}
		if string(m.Parts) != `[{"type":"text","text":"answer"}]` {
			t.Fatalf("public projection: %s", m.Parts)
		}
	}
	rows := s.GetRoomMessages(r.RoomID, 100, 0)
	count := 0
	for _, m := range rows {
		if m.RunID == r.ID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%d final deliveries", count)
	}
	trigger, err := s.GetMessageByID(r.TriggerMessageID)
	if err != nil || trigger.ActorID != r.ActorID {
		t.Fatalf("stable actor missing: %+v %v", trigger, err)
	}
	raw, _ := json.Marshal(trigger)
	if string(raw) == "" || containsRaw(raw, "stable-account") {
		t.Fatal("private actor exposed by message JSON")
	}
	if err := s.FinishBotRun(r.ID, "cancelled", "late", 0, true); err != nil {
		t.Fatal(err)
	}
	stored, _ := s.GetBotRun(r.ID)
	if stored.Status != "completed" || stored.ChargedTokens != 30 {
		t.Fatalf("terminal state regressed: %+v", stored)
	}
}
func containsRaw(raw []byte, needle string) bool {
	for i := 0; i+len(needle) <= len(raw); i++ {
		if string(raw[i:i+len(needle)]) == needle {
			return true
		}
	}
	return false
}

func TestBotRunBudgetReservationsSurviveRestartAndMidnight(t *testing.T) {
	t.Setenv("CHAT_SKIP_SEED", "true")
	path := filepath.Join(t.TempDir(), "runs.db")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	first := testBotRun(t, s, "oidc:A", "2026-10-21", 600)
	admitRun(t, s, first, RunBudgetLimits{Tokens: 1000})
	if err := s.StartBotRun(first.ID); err != nil {
		t.Fatal(err)
	}
	second := testBotRun(t, s, "oidc:B", "2026-10-21", 600)
	if reason, err := s.CreateBotRun(second, RunBudgetLimits{Tokens: 1000}); err != nil || reason != "global" {
		t.Fatalf("inflight budget exceeded: %q %v", reason, err)
	}
	// Next day has a separate ledger; settling yesterday cannot eat/refund today.
	tomorrow := testBotRun(t, s, "oidc:A", "2026-10-22", 700)
	admitRun(t, s, tomorrow, RunBudgetLimits{Tokens: 1000})
	if _, _, err := s.CompleteBotRun(first.ID, "answer", nil, 100, true); err != nil {
		t.Fatal(err)
	}
	admitRun(t, s, second, RunBudgetLimits{Tokens: 1000})
	if err := s.StartBotRun(second.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.InterruptBotRuns(); err != nil {
		t.Fatal(err)
	}
	interrupted, _ := s.GetBotRun(second.ID)
	queued, _ := s.GetBotRun(tomorrow.ID)
	if interrupted.Status != "failed" || interrupted.Reason != "interrupted" || interrupted.ChargedTokens != 600 {
		t.Fatalf("lost running reservation: %+v", interrupted)
	}
	if queued.Status != "failed" || queued.ChargedTokens != 0 {
		t.Fatalf("queued restart charge: %+v", queued)
	}
	third := testBotRun(t, s, "oidc:C", "2026-10-21", 400)
	if reason, err := s.CreateBotRun(third, RunBudgetLimits{Tokens: 1000}); err != nil || reason != "global" {
		t.Fatalf("restart reset budget: %q %v", reason, err)
	}
	if _, _, err := s.CompleteBotRun(second.ID, "late final", nil, 1, true); err == nil {
		t.Fatal("interrupted run published")
	}
}

func TestBotRunUnknownUsageAndSubjectBudget(t *testing.T) {
	s := newRunStore(t)
	r := testBotRun(t, s, "oidc:subject", "2026-10-21", 500)
	admitRun(t, s, r, RunBudgetLimits{PerUser: 1})
	if err := s.StartBotRun(r.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CompleteBotRun(r.ID, "answer", nil, 0, false); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetBotRun(r.ID)
	if got.ChargedTokens != 500 {
		t.Fatal("unknown usage bypassed reservation")
	}
	renamed := testBotRun(t, s, "oidc:subject", "2026-10-21", 500)
	renamed.RequestedBy = "new-display-name"
	if reason, err := s.CreateBotRun(renamed, RunBudgetLimits{PerUser: 1}); err != nil || reason != "user" {
		t.Fatalf("changed handle bypassed subject quota: %q %v", reason, err)
	}
}

func TestBotRunLogoutTransactionFencesPersistentDelivery(t *testing.T) {
	s := newRunStore(t)
	r := testBotRun(t, s, "oidc:subject", "2026-10-21", 500)
	admitRun(t, s, r, RunBudgetLimits{})
	if err := s.StartBotRun(r.ID); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := s.ApplyOIDCBackchannelLogout("subject", "jti", "hash", now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CompleteBotRun(r.ID, "late answer", nil, 1, true); err == nil {
		t.Fatal("logout did not fence final delivery")
	}
	called := false
	if err := s.WithBotRunPublication(r.ID, func() { called = true }); err == nil || called {
		t.Fatal("logout did not fence live publication")
	}
	stale := testBotRun(t, s, "oidc:subject", "2026-10-21", 1)
	if _, err := s.CreateBotRun(stale, RunBudgetLimits{}); err == nil {
		t.Fatal("stale session admitted")
	}
}

func TestBotRunSnapshotSameTimestampCutoffAndScope(t *testing.T) {
	s := newRunStore(t)
	room, _ := s.GetRoomID("公共聊天")
	first, _ := s.InsertMessage("alice", "original A", "", room, "", "", "")
	later, _ := s.InsertMessage("bob", "later B", "", room, "", "", "")
	private, _ := s.InsertMessage("alice", "private A", "", "", "TokenBot", "", "")
	for _, id := range []string{first.ID, later.ID, private.ID} {
		if _, err := s.db.Exec("UPDATE messages SET timestamp=42 WHERE id=?", id); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := s.SnapshotBotHistory(first.ID, room, "alice", "TokenBot", "", false, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot) != 1 || snapshot[0].ID != first.ID {
		t.Fatalf("same-timestamp message crossed cutoff: %+v", snapshot)
	}
	dm, err := s.SnapshotBotHistory(private.ID, "", "alice", "TokenBot", "", true, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(dm) != 1 || dm[0].ID != private.ID {
		t.Fatalf("private snapshot scope: %+v", dm)
	}
}

func TestCompleteBotRunSplitBurst(t *testing.T) {
	s := newRunStore(t)
	r := testBotRun(t, s, "oidc:burst-actor", "2026-10-23", 100)
	admitRun(t, s, r, RunBudgetLimits{})
	if err := s.StartBotRun(r.ID); err != nil {
		t.Fatal(err)
	}
	msgs, created, err := s.CompleteBotRunSplit(r.ID, []string{"收到！", "答案是 42。", "来源见末尾"}, nil, 42, true)
	if err != nil || !created {
		t.Fatalf("split complete: %v %v", msgs, err)
	}
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3", len(msgs))
	}
	if msgs[0].ID != r.MessageID {
		t.Fatalf("first segment must reuse the stream draft id, got %s", msgs[0].ID)
	}
	if msgs[1].ID == r.MessageID || msgs[2].ID == r.MessageID {
		t.Fatal("later segments must be fresh message ids")
	}
	if msgs[0].ReplyToID != r.TriggerMessageID || msgs[1].ReplyToID != "" || msgs[2].ReplyToID != "" {
		t.Fatalf("reply anchors: %q %q %q", msgs[0].ReplyToID, msgs[1].ReplyToID, msgs[2].ReplyToID)
	}
	for i, m := range msgs {
		if m.Content != []string{"收到！", "答案是 42。", "来源见末尾"}[i] {
			t.Errorf("content %d = %q", i, m.Content)
		}
		if m.RunID != r.ID {
			t.Errorf("message %d missing run link", i)
		}
	}
	// Idempotent replay inserts nothing and returns the run-linked first
	// message; the burst rows stay exactly three.
	replay, created, err := s.CompleteBotRunSplit(r.ID, []string{"收到！", "答案是 42。", "来源见末尾"}, nil, 42, true)
	if err != nil || created {
		t.Fatalf("replay: %v created=%v", err, created)
	}
	if len(replay) != 1 || replay[0].ID != msgs[0].ID {
		t.Fatalf("replay must return the first message without rebroadcasting: %+v", replay)
	}
	rows := s.GetRoomMessages(r.RoomID, 100, 0)
	count := 0
	for _, m := range rows {
		if m.RunID == r.ID || strings.HasPrefix(m.RunID, r.ID+"#") {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("%d burst deliveries, want exactly 3", count)
	}
	run, err := s.GetBotRun(r.ID)
	if err != nil || run.Status != "completed" || run.ChargedTokens != 42 {
		t.Fatalf("run not settled: %+v %v", run, err)
	}
}

func TestCompleteBotRunSplitSingleSegmentDelegates(t *testing.T) {
	s := newRunStore(t)
	r := testBotRun(t, s, "oidc:single-actor", "2026-10-23", 100)
	admitRun(t, s, r, RunBudgetLimits{})
	if err := s.StartBotRun(r.ID); err != nil {
		t.Fatal(err)
	}
	msgs, created, err := s.CompleteBotRunSplit(r.ID, []string{"整条回复"}, nil, 7, true)
	if err != nil || !created || len(msgs) != 1 {
		t.Fatalf("single: %v %v %d", msgs, err, len(msgs))
	}
	if msgs[0].ID != r.MessageID || msgs[0].Content != "整条回复" {
		t.Fatalf("single mismatch: %+v", msgs[0])
	}
}

func TestPrivateThreadsLaneIsolationAndList(t *testing.T) {
	s := newRunStore(t)
	// Main lane ("")
	if _, err := s.InsertMessage("alice", "主聊天第一条", "", "", "TokenBot", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertMessage("TokenBot", "主聊天回复", "", "", "alice", "", ""); err != nil {
		t.Fatal(err)
	}
	// Side lane
	if _, err := s.InsertMessage("alice", "旁聊：帮我分析 CSV", "", "", "TokenBot", "", "pm-lane-1"); err != nil {
		t.Fatal(err)
	}
	// Another user's main lane must never leak in
	if _, err := s.InsertMessage("bob", "bob 的主聊天", "", "", "TokenBot", "", ""); err != nil {
		t.Fatal(err)
	}

	main := s.GetMessagesBetweenThread("alice", "TokenBot", "", 50)
	if len(main) != 2 {
		t.Fatalf("main lane = %d messages, want 2 (got %+v)", len(main), main)
	}
	side := s.GetMessagesBetweenThread("alice", "TokenBot", "pm-lane-1", 50)
	if len(side) != 1 || side[0].Content != "旁聊：帮我分析 CSV" {
		t.Fatalf("side lane = %+v", side)
	}

	threads := s.ListPrivateThreads("alice", "TokenBot")
	if len(threads) != 2 {
		t.Fatalf("threads = %+v", threads)
	}
	if threads[0].ThreadID != "" || threads[0].Count != 2 {
		t.Fatalf("main lane first: %+v", threads[0])
	}
	if threads[1].ThreadID != "pm-lane-1" || threads[1].Preview != "旁聊：帮我分析 CSV" {
		t.Fatalf("side lane summary: %+v", threads[1])
	}
}

func TestBotRunCarriesThreadIntoFinal(t *testing.T) {
	s := newRunStore(t)
	trigger, err := s.InsertMessage("alice", "lane 请求", "", "", "TokenBot", "", "pm-lane-9")
	if err != nil {
		t.Fatal(err)
	}
	r := BotRun{ID: uuid.NewString(), MessageID: uuid.NewString(), TriggerMessageID: trigger.ID, ActorID: "oidc:lane-actor", RequestedBy: "alice", RoomID: "", ToUser: "alice", BotName: "TokenBot", ThreadID: "pm-lane-9", Status: "queued", BudgetDay: "2026-10-23", IPKey: "127.0.0.1", Input: json.RawMessage(`[]`), ReservedTokens: 100, CreatedAt: time.Now().UnixMilli()}
	admitRun(t, s, r, RunBudgetLimits{})
	if err := s.StartBotRun(r.ID); err != nil {
		t.Fatal(err)
	}
	m, created, err := s.CompleteBotRun(r.ID, "lane answer", nil, 10, true)
	if err != nil || !created {
		t.Fatal(err)
	}
	if m.ThreadID != "pm-lane-9" {
		t.Fatalf("final landed in wrong lane: %q", m.ThreadID)
	}
	side := s.GetMessagesBetweenThread("alice", "TokenBot", "pm-lane-9", 10)
	found := false
	for _, msg := range side {
		if msg.ID == m.ID {
			found = true
		}
	}
	if !found {
		var tid string
		_ = s.db.QueryRow("SELECT COALESCE(thread_id,'<null>') FROM messages WHERE id=?", m.ID).Scan(&tid)
		t.Fatalf("final not visible in its lane; db thread_id=%q run.ThreadID=%q", tid, r.ThreadID)
	}
	main := s.GetMessagesBetweenThread("alice", "TokenBot", "", 50)
	for _, msg := range main {
		if msg.ID == m.ID {
			t.Fatal("lane final leaked into main chat")
		}
	}
}
