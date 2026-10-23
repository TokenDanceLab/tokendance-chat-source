package storetest

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"tokendancechat/backend/store"
)

func testRunLifecycle(t *testing.T, s Store) {
	room := "room-run-" + unique("x")
	trigger := publicMessage(t, s, "alice", room, "trigger")
	runID := unique("run")
	messageID := unique("msg")
	r := newRun(runID, messageID, trigger.ID, "local:alice", room, "", "")

	mustCreateRun(t, s, r, store.RunBudgetLimits{})
	reason, err := s.CreateBotRun(newRun(unique("run"), unique("msg"), trigger.ID, "local:alice", room, "", ""), store.RunBudgetLimits{})
	if err != nil || reason != "duplicate" {
		t.Fatalf("duplicate trigger = %q, err=%v", reason, err)
	}
	got, err := s.GetBotRun(runID)
	if err != nil {
		t.Fatalf("GetBotRun: %v", err)
	}
	if got.Status != "queued" || got.ThreadID != "" || len(got.Input) == 0 {
		t.Fatalf("queued run = %+v", got)
	}
	mustStartRun(t, s, runID)
	if err := s.StartBotRun(runID); !errors.Is(err, store.ErrRunUnavailable) {
		t.Fatalf("second StartBotRun err = %v", err)
	}

	// The frozen snapshot stops at the trigger's seq, even for later inserts.
	later := publicMessage(t, s, "bob", room, "later")
	snapshot, err := s.SnapshotBotHistory(trigger.ID, room, "alice", "TokenBot", "", false, 50)
	if err != nil {
		t.Fatalf("SnapshotBotHistory: %v", err)
	}
	if _, ok := findMessage(snapshot, trigger.ID); !ok {
		t.Fatal("snapshot lost the trigger")
	}
	if _, ok := findMessage(snapshot, later.ID); ok {
		t.Fatal("snapshot included a message after the trigger")
	}

	parts := json.RawMessage(`[{"type":"text","text":"ignored"}]`)
	final, created, err := s.CompleteBotRun(runID, "final answer", parts, 42, true)
	if err != nil || !created {
		t.Fatalf("CompleteBotRun = created=%v err=%v", created, err)
	}
	if final.ID != messageID || final.RunID != runID || final.Content != "final answer" || final.Timestamp == 0 {
		t.Fatalf("final message = %+v", final)
	}
	again, created, err := s.CompleteBotRun(runID, "other", nil, 1, true)
	if err != nil || created || again.ID != messageID {
		t.Fatalf("duplicate CompleteBotRun = %+v created=%v err=%v", again, created, err)
	}
	completed, _ := s.GetBotRun(runID)
	if completed.Status != "completed" || completed.ChargedTokens != 42 {
		t.Fatalf("completed run = %+v", completed)
	}
	finals := 0
	for _, m := range s.GetRoomMessages(room, 100, 0) {
		if m.RunID == runID {
			finals++
		}
	}
	if finals != 1 {
		t.Fatalf("run has %d final messages, want 1", finals)
	}
	recent, err := s.RecentBotRuns(room, "local:alice", 10)
	if err != nil {
		t.Fatalf("RecentBotRuns: %v", err)
	}
	found := false
	for _, r := range recent {
		if r.ID == runID {
			found = true
		}
	}
	if !found {
		t.Fatalf("RecentBotRuns = %+v", recent)
	}

	// Budget rejections: the first run already consumes one unit of each scope.
	checkReason := func(name string, r store.BotRun, limits store.RunBudgetLimits, want string) {
		t.Helper()
		got, err := s.CreateBotRun(r, limits)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != want {
			t.Fatalf("%s reason = %q, want %q", name, got, want)
		}
	}
	t2 := publicMessage(t, s, "alice", room, "trigger-2")
	checkReason("per-user", newRun(unique("run"), unique("msg"), t2.ID, "local:alice", room, "", ""),
		store.RunBudgetLimits{PerUser: 1}, "user")
	t3 := publicMessage(t, s, "bob", room, "trigger-3")
	checkReason("per-ip", newRun(unique("run"), unique("msg"), t3.ID, "local:bob", room, "", ""),
		store.RunBudgetLimits{PerUser: 10, PerIP: 1}, "ip")
	t4 := publicMessage(t, s, "carol", room, "trigger-4")
	checkReason("tokens", newRun(unique("run"), unique("msg"), t4.ID, "local:carol", room, "", ""),
		store.RunBudgetLimits{PerUser: 10, PerIP: 10, Tokens: 1}, "global")
	t5 := publicMessage(t, s, "dave", room, "trigger-5")
	checkReason("replies", newRun(unique("run"), unique("msg"), t5.ID, "local:dave", room, "", ""),
		store.RunBudgetLimits{PerUser: 10, PerIP: 10, Replies: 1}, "global")

	// Expired sessions are rejected at admission.
	t6 := publicMessage(t, s, "erin", room, "trigger-6")
	expired := newRun(unique("run"), unique("msg"), t6.ID, "oidc:expired-sub", room, "", "")
	expired.SessionExpiresAt = time.Now().Add(-time.Minute).Unix()
	if reason, err := s.CreateBotRun(expired, store.RunBudgetLimits{PerUser: 10, PerIP: 10}); reason != "auth" || !errors.Is(err, store.ErrRunUnavailable) {
		t.Fatalf("expired session = %q, err=%v", reason, err)
	}

	// Interrupt marks in-flight runs failed; queued runs surrender their charge.
	t7 := publicMessage(t, s, "frank", room, "trigger-7")
	queuedID := unique("run-q")
	mustCreateRun(t, s, newRun(queuedID, unique("msg"), t7.ID, "local:frank", room, "", ""), store.RunBudgetLimits{PerUser: 10, PerIP: 10})
	t8 := publicMessage(t, s, "gina", room, "trigger-8")
	runningID := unique("run-r")
	mustCreateRun(t, s, newRun(runningID, unique("msg"), t8.ID, "local:gina", room, "", ""), store.RunBudgetLimits{PerUser: 10, PerIP: 10})
	mustStartRun(t, s, runningID)
	if err := s.InterruptBotRuns(); err != nil {
		t.Fatalf("InterruptBotRuns: %v", err)
	}
	queuedRun, _ := s.GetBotRun(queuedID)
	runningRun, _ := s.GetBotRun(runningID)
	if queuedRun.Status != "failed" || queuedRun.Reason != "interrupted" || queuedRun.ChargedTokens != 0 {
		t.Fatalf("interrupted queued run = %+v", queuedRun)
	}
	if runningRun.Status != "failed" || runningRun.ChargedTokens != 0 {
		t.Fatalf("interrupted running run = %+v", runningRun)
	}
}

func testRunBudgetConcurrency(t *testing.T, s Store) {
	room := "room-conc-" + unique("x")
	triggers := make([]string, 12)
	runs := make([]store.BotRun, 12)
	for i := range triggers {
		m := publicMessage(t, s, "alice", room, "trigger")
		triggers[i] = m.ID
		runs[i] = newRun(unique("run"), unique("msg"), m.ID, "local:user", room, "", "")
	}
	limits := store.RunBudgetLimits{Replies: 5}

	var (
		wg                 sync.WaitGroup
		mu                 sync.Mutex
		accepted, rejected int
		errs               []error
	)
	for i := range runs {
		wg.Add(1)
		go func(r store.BotRun) {
			defer wg.Done()
			reason, err := s.CreateBotRun(r, limits)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				errs = append(errs, err)
			case reason == "":
				accepted++
			case reason == "global":
				rejected++
			default:
				errs = append(errs, errors.New("unexpected reason "+reason))
			}
		}(runs[i])
	}
	wg.Wait()
	if len(errs) != 0 {
		t.Fatalf("concurrent CreateBotRun errors: %v", errs)
	}
	if accepted != 5 || rejected != 7 {
		t.Fatalf("accepted=%d rejected=%d, want 5/7", accepted, rejected)
	}
}

func testRunBurstAndWeb(t *testing.T, s Store) {
	room := "room-web-" + unique("x")
	trigger := publicMessage(t, s, "alice", room, "research this")
	runID := unique("run")
	messageID := unique("msg")
	r := newRun(runID, messageID, trigger.ID, "local:alice", room, "", "")
	mustCreateRun(t, s, r, store.RunBudgetLimits{})
	mustStartRun(t, s, runID)

	for i := 1; i <= 3; i++ {
		id := unique("call")
		if err := s.ReserveWebCall(runID, id, unique("tc"), "web_search", 1_000_000); err != nil {
			t.Fatalf("ReserveWebCall #%d: %v", i, err)
		}
	}
	if err := s.ReserveWebCall(runID, unique("call"), unique("tc"), "web_search", 1_000_000); err == nil {
		t.Fatal("fourth web call accepted")
	}
	if err := s.ReserveWebCall(runID, unique("call"), unique("tc"), "web_fetch", 1); err == nil {
		t.Fatal("exhausted monetary budget accepted")
	}

	src := store.WebSource{
		ID: "src-1", RunID: runID, URL: "https://example.com/a", Title: "A",
		Status: "fetched", Content: "body", ContentHash: "hash",
		RetrievedAt: time.Now().UnixMilli(),
	}
	saved, err := s.SaveWebSource(src)
	if err != nil {
		t.Fatalf("SaveWebSource: %v", err)
	}
	if saved.ID != "src-1" {
		t.Fatalf("saved source = %+v", saved)
	}
	discovered := src
	discovered.Status = "discovered"
	if old, err := s.SaveWebSource(discovered); err != nil || old.Status != "fetched" || old.Content != "body" {
		t.Fatalf("discovery overwrote fetched evidence: %+v, err=%v", old, err)
	}
	gotSource, err := s.GetWebSource(runID, "src-1")
	if err != nil || gotSource.Content != "body" || gotSource.RetrievedAt == 0 {
		t.Fatalf("GetWebSource = %+v, err=%v", gotSource, err)
	}

	contents := []string{"first [[source:src-1]]", "second"}
	burst, created, err := s.CompleteBotRunSplit(runID, contents, json.RawMessage(`[{"type":"text","text":"draft"}]`), 10, true)
	if err != nil || !created {
		t.Fatalf("CompleteBotRunSplit = created=%v err=%v", created, err)
	}
	if len(burst) != 2 {
		t.Fatalf("burst = %d messages", len(burst))
	}
	if burst[0].ID != messageID {
		t.Fatalf("first burst id = %q, want %q", burst[0].ID, messageID)
	}
	if !strings.Contains(burst[0].Content, "[1](<https://example.com/a>)") {
		t.Fatalf("citation not resolved: %q", burst[0].Content)
	}
	if burst[1].Content != "second" || burst[1].ID == burst[0].ID {
		t.Fatalf("second burst = %+v", burst[1])
	}
	if !strings.Contains(string(burst[1].Parts), "source_id") || !strings.Contains(string(burst[1].Parts), `"cited":true`) {
		t.Fatalf("source DTO not attached to the last segment: %s", burst[1].Parts)
	}

	// Later burst segments persist the derived run link.
	stored := 0
	for _, m := range s.GetRoomMessages(room, 100, 0) {
		if m.RunID == runID || strings.HasPrefix(m.RunID, runID+"#") {
			stored++
		}
	}
	if stored != 2 {
		t.Fatalf("stored burst messages = %d, want 2", stored)
	}

	replay, created, err := s.CompleteBotRunSplit(runID, contents, nil, 10, true)
	if err != nil || created || len(replay) != 1 || replay[0].ID != messageID {
		t.Fatalf("burst replay = %+v created=%v err=%v", replay, created, err)
	}

	if err := s.SettleWebCall(saved.ID, "fetched", false); err != nil {
		t.Fatalf("SettleWebCall: %v", err)
	}
	if err := s.SettleWebCall(saved.ID, "fetched", false); err != nil {
		t.Fatalf("SettleWebCall repeat must be a no-op: %v", err)
	}
	if _, err := s.SaveWebSource(src); !errors.Is(err, store.ErrRunUnavailable) {
		t.Fatalf("source saved after completion: %v", err)
	}
	if err := s.ReserveWebCall(runID, unique("call"), unique("tc"), "web_search", 1_000_000); !errors.Is(err, store.ErrRunUnavailable) {
		t.Fatalf("reservation after completion: %v", err)
	}
}

func testPublicationFence(t *testing.T, s Store) {
	room := "room-fence-" + unique("x")

	trigger := publicMessage(t, s, "alice", room, "publish")
	runID := unique("run")
	mustCreateRun(t, s, newRun(runID, unique("msg"), trigger.ID, "local:alice", room, "", ""), store.RunBudgetLimits{})
	mustStartRun(t, s, runID)
	called := false
	if err := s.WithBotRunPublication(runID, func() { called = true }); err != nil || !called {
		t.Fatalf("valid publication = %v, called=%v", err, called)
	}

	if err := s.FinishBotRun(runID, "cancelled", "user", 0, true); err != nil {
		t.Fatalf("FinishBotRun: %v", err)
	}
	called = false
	if err := s.WithBotRunPublication(runID, func() { called = true }); !errors.Is(err, store.ErrRunUnavailable) || called {
		t.Fatalf("cancelled run publication = %v, called=%v", err, called)
	}

	// A logout cancels older-generation OIDC runs before publication.
	sub := "sub-fence-" + unique("x")
	trigger2 := publicMessage(t, s, "alice", room, "publish oidc")
	oidcRunID := unique("run")
	oidcRun := newRun(oidcRunID, unique("msg"), trigger2.ID, "oidc:"+sub, room, "", "")
	oidcRun.SessionGeneration = 0
	mustCreateRun(t, s, oidcRun, store.RunBudgetLimits{})
	mustStartRun(t, s, oidcRunID)
	now := time.Now()
	if _, err := s.ApplyOIDCBackchannelLogout(sub, "jti-"+unique("x"), "claims", now, now.Add(time.Hour)); err != nil {
		t.Fatalf("logout: %v", err)
	}
	called = false
	if err := s.WithBotRunPublication(oidcRunID, func() { called = true }); !errors.Is(err, store.ErrRunUnavailable) || called {
		t.Fatalf("revoked publication = %v, called=%v", err, called)
	}

	// A run created at the current generation still publishes.
	trigger3 := publicMessage(t, s, "alice", room, "publish current")
	currentID := unique("run")
	current := newRun(currentID, unique("msg"), trigger3.ID, "oidc:"+sub, room, "", "")
	current.SessionGeneration = 1
	mustCreateRun(t, s, current, store.RunBudgetLimits{})
	mustStartRun(t, s, currentID)
	called = false
	if err := s.WithBotRunPublication(currentID, func() { called = true }); err != nil || !called {
		t.Fatalf("current-generation publication = %v, called=%v", err, called)
	}
}
