package store

import (
	"encoding/json"
	"github.com/google/uuid"
	"path/filepath"
	"testing"
	"time"
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
	snapshot, err := s.SnapshotBotHistory(first.ID, room, "alice", "TokenBot", false, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot) != 1 || snapshot[0].ID != first.ID {
		t.Fatalf("same-timestamp message crossed cutoff: %+v", snapshot)
	}
	dm, err := s.SnapshotBotHistory(private.ID, "", "alice", "TokenBot", true, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(dm) != 1 || dm[0].ID != private.ID {
		t.Fatalf("private snapshot scope: %+v", dm)
	}
}
