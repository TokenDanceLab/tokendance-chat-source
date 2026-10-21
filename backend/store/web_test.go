package store

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"tokendancechat/backend/llm"
	"tokendancechat/backend/websearch"
)

func TestWebMoneyReservationRestartAndDay(t *testing.T) {
	t.Setenv("CHAT_SKIP_SEED", "true")
	path := filepath.Join(t.TempDir(), "web.db")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	r := testBotRun(t, s, "local:alice", "2026-10-21", 100)
	admitRun(t, s, r, RunBudgetLimits{})
	if err = s.StartBotRun(r.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveWebCall(r.ID, "call-1", "model-call-1", "web_search", 10000); err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveWebCall(r.ID, "call-2", "model-call-2", "web_fetch", 10000); err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveWebCall(r.ID, "denied", "denied", "web_search", 10000); err == nil {
		t.Fatal("daily money limit bypassed")
	}
	if err = s.SettleWebCall("call-1", "empty", true); err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveWebCall(r.ID, "call-3", "model-call-3", "web_search", 10000); err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveWebCall(r.ID, "call-4", "model-call-4", "web_search", 100000); err == nil {
		t.Fatal("per-run call cap bypassed")
	}
	if err = s.FinishBotRun(r.ID, "cancelled", "requested", 0, false); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.InterruptBotRuns(); err != nil {
		t.Fatal(err)
	}
	var charged int64
	if err = s.db.QueryRow("SELECT SUM(charged_micro_cny) FROM bot_web_calls").Scan(&charged); err != nil || charged != 10000 {
		t.Fatalf("unknown charge lost across restart: %d %v", charged, err)
	}
	next := testBotRun(t, s, "local:bob", "2026-10-22", 100)
	admitRun(t, s, next, RunBudgetLimits{})
	_ = s.StartBotRun(next.ID)
	if err = s.ReserveWebCall(next.ID, "next-day", "next-day", "web_search", 5000); err != nil {
		t.Fatal(err)
	}
	old := testBotRun(t, s, "local:bob", "2026-10-21", 100)
	admitRun(t, s, old, RunBudgetLimits{})
	_ = s.StartBotRun(old.ID)
	if err = s.ReserveWebCall(old.ID, "old-day", "old-day", "web_search", 10000); err == nil {
		t.Fatal("old budget day reset")
	}
}

func TestWebEvidenceFinalProjectionAndScope(t *testing.T) {
	s := newRunStore(t)
	a := testBotRun(t, s, "local:alice", "2026-10-21", 100)
	admitRun(t, s, a, RunBudgetLimits{})
	_ = s.StartBotRun(a.ID)
	b := testBotRun(t, s, "local:bob", "2026-10-21", 100)
	admitRun(t, s, b, RunBudgetLimits{})
	_ = s.StartBotRun(b.ID)
	for _, source := range []WebSource{
		{ID: "read", RunID: a.ID, URL: "https://example.com/read", Title: "Read title", Status: "fetched", Content: "private snapshot", ContentHash: "hash"},
		{ID: "candidate", RunID: a.ID, URL: "https://example.com/candidate", Title: "Candidate", Status: "discovered"},
		{ID: "failed", RunID: a.ID, URL: "https://example.com/failed", Title: "Failed", Status: "failed", ErrorCode: "safe-code"},
		{ID: "foreign", RunID: b.ID, URL: "https://example.org/other-user", Title: "Other user", Status: "fetched", Content: "foreign snapshot"},
	} {
		if _, err := s.SaveWebSource(source); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.GetWebSource(a.ID, "foreign"); err == nil {
		t.Fatal("cross-run evidence readable")
	}
	content := "Fact [[source:read]]. [[source:candidate]] [[source:foreign]] [[source:invented]]. Ordinary [link](https://ordinary.example.com)."
	raw := json.RawMessage(`[{"type":"source","source_id":"forged","url":"https://forged.example.com","status":"fetched","cited":true},{"type":"reasoning","text":"secret reasoning"},{"type":"text","text":"ignored tool draft","arguments":"secret args"}]`)
	m, created, err := s.CompleteBotRun(a.ID, content, raw, 12, true)
	if err != nil || !created {
		t.Fatalf("complete %v %v", created, err)
	}
	if !strings.Contains(m.Content, "[1](<https://example.com/read>)") || strings.Contains(m.Content, "[[source:") {
		t.Fatalf("citations not resolved: %s", m.Content)
	}
	for _, secret := range []string{"private snapshot", "foreign snapshot", "forged", "reasoning", "secret args", "other-user", "content_hash"} {
		if strings.Contains(string(m.Parts), secret) {
			t.Errorf("leaked/forged metadata: %s", secret)
		}
	}
	var parts []llm.Part
	_ = json.Unmarshal(m.Parts, &parts)
	sources := 0
	for _, part := range parts {
		if part.Type == "source" {
			sources++
			if part.Cited != (part.SourceID == "read") {
				t.Fatalf("incorrect citation evidence %#v", part)
			}
		}
	}
	if sources != 3 {
		t.Fatalf("%d sources", sources)
	}
	history, err := s.GetMessageByID(m.ID)
	if err != nil || string(history.Parts) != string(m.Parts) {
		t.Fatalf("source history mismatch: %s %v", history.Parts, err)
	}
	if _, err = s.SaveWebSource(WebSource{ID: "late", RunID: a.ID, URL: "https://example.com/late", Status: "fetched"}); err == nil {
		t.Fatal("source inserted after final")
	}
}

func TestNoEvidenceOutsideRunAndAfterCancel(t *testing.T) {
	s := newRunStore(t)
	raw := json.RawMessage(`[{"type":"source","source_id":"forged","title":"bad","url":"https://example.com","status":"fetched"},{"type":"text","text":"ordinary"}]`)
	m, err := s.InsertMessageWithParts("TokenBot", "ordinary", "", "", "", raw)
	if err != nil || strings.Contains(string(m.Parts), "source") {
		t.Fatalf("non-run evidence: %s %v", m.Parts, err)
	}
	// An older row carrying source-shaped JSON still cannot claim provenance.
	_, _ = s.db.Exec("UPDATE messages SET parts=? WHERE id=?", string(raw), m.ID)
	old, err := s.GetMessageByID(m.ID)
	if err != nil || strings.Contains(string(old.Parts), "source") {
		t.Fatalf("old source forgery: %s %v", old.Parts, err)
	}
	r := testBotRun(t, s, "local:alice", "2026-10-21", 100)
	admitRun(t, s, r, RunBudgetLimits{})
	_ = s.StartBotRun(r.ID)
	_ = s.FinishBotRun(r.ID, "cancelled", "requested", 0, false)
	if err = s.ReserveWebCall(r.ID, "late", "late", "web_fetch", websearch.PriceMicroCNY); err == nil {
		t.Fatal("cancelled run can spend")
	}
	if _, err = s.SaveWebSource(WebSource{ID: "late", RunID: r.ID, URL: "https://example.com", Status: "fetched"}); err == nil {
		t.Fatal("cancelled run saved evidence")
	}
}

func TestFinalNeverRestoresMalformedCitationMarkers(t *testing.T) {
	s := newRunStore(t)
	for _, marker := range []string{"[[source:unfinished", "[[source:line\nbreak]]", "[[source:" + strings.Repeat("x", 300) + "]]"} {
		r := testBotRun(t, s, "local:alice", "2026-10-21", 100)
		admitRun(t, s, r, RunBudgetLimits{})
		_ = s.StartBotRun(r.ID)
		raw, _ := json.Marshal([]llm.Part{{Type: "text", Text: "Answer " + marker}})
		m, _, err := s.CompleteBotRun(r.ID, "Answer "+marker, raw, 1, true)
		if err != nil || m.Content != "Answer " || strings.Contains(string(m.Parts), "[[source:") {
			t.Fatalf("marker restored in final %q %s %v", m.Content, m.Parts, err)
		}
	}
}
