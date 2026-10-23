package hub

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"tokendancechat/backend/llm"
	"tokendancechat/backend/websearch"
)

type fakeWebProvider struct {
	content                    string
	searchErr, fetchErr        error
	searchCalls, fetchCalls    int
	fetchStarted, fetchRelease chan struct{}
}

func (p *fakeWebProvider) Search(context.Context, string, int) ([]websearch.Hit, error) {
	p.searchCalls++
	if p.searchErr != nil {
		return nil, p.searchErr
	}
	return []websearch.Hit{{Title: "Official release", URL: "https://example.com/release", Summary: "Discovery only"}, {Title: "Unsafe", URL: "http://127.0.0.1/private"}}, nil
}
func (p *fakeWebProvider) Fetch(context.Context, string) (websearch.Page, error) {
	p.fetchCalls++
	if p.fetchErr != nil {
		return websearch.Page{}, p.fetchErr
	}
	if p.fetchStarted != nil {
		close(p.fetchStarted)
		<-p.fetchRelease
	}
	content := p.content
	if content == "" {
		content = strings.Repeat("Evidence. ", 700)
	}
	return websearch.Page{Title: "Official release", Content: content}, nil
}
func enableTestWeb(h *Hub, p *fakeWebProvider) {
	h.llmCfg.APIKey = "fake-model-key"
	h.llmCfg.WebSearch = &websearch.Config{Timeout: time.Second, DailyMicroCNY: 5000000}
	h.webProvider = p
	h.webStore = h.store.(webStore)
}
func toolNamed(t *testing.T, tools []llm.Tool, name string) llm.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("missing tool %s", name)
	return nil
}

func TestWebRunSearchReadCitationAndPublicProjection(t *testing.T) {
	h, s, model := runTestHub(t)
	p := &fakeWebProvider{}
	enableTestWeb(h, p)
	room, _ := s.GetRoomID("公共聊天")
	c := runClient(h, "alice", "", room)
	r := enqueueTestRun(t, c, "Find the release and verify it", false)
	call := nextCall(t, model)
	search := toolNamed(t, call.tools, "web_search")
	fetch := toolNamed(t, call.tools, "web_fetch")
	out, err := search.Execute(context.Background(), json.RawMessage(`{"query":"official release","limit":2}`))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Sources []struct {
			SourceID string `json:"source_id"`
			Status   string `json:"status"`
		} `json:"sources"`
	}
	if json.Unmarshal([]byte(out), &result) != nil || len(result.Sources) != 1 || result.Sources[0].Status != "discovered" {
		t.Fatalf("search source contract %s", out)
	}
	id := result.Sources[0].SourceID
	args, _ := json.Marshal(map[string]string{"source_id": id})
	out, err = fetch.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"truncated":true`) {
		t.Fatalf("missing truncation flag %s", out[:min(100, len(out))])
	}
	source, err := s.GetWebSource(r.ID, id)
	if err != nil || source.Status != "fetched" || len([]rune(source.Content)) != 5000 || len(source.ContentHash) != 64 {
		t.Fatalf("evidence snapshot %#v %v", source, err)
	}
	if _, err = fetch.Execute(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if p.fetchCalls != 1 {
		t.Fatal("same-run evidence refetched unnecessarily")
	}
	// Fragmented model references must not appear in public text deltas.
	for _, text := range []string{"Answer [", "[sou", "rce:", id, "]] done"} {
		if err = call.emit(llm.Event{Kind: "text", Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	finishCall(call, "Answer [[source:"+id+"]].")
	awaitStatus(t, s, r.ID, "completed")
	m, err := s.GetMessageByID(r.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.Content, "[1](<https://example.com/release>)") || !strings.Contains(string(m.Parts), `"cited":true`) {
		t.Fatalf("citation missing: %s %s", m.Content, m.Parts)
	}
	for len(c.send) > 0 {
		data := <-c.send
		if strings.Contains(string(data), "Discovery only") || strings.Contains(string(data), "Evidence.") || strings.Contains(string(data), "[[source:") {
			t.Fatalf("private evidence/marker leaked to public: %s", data)
		}
	}
}

func TestWebLateProviderResultCannotPublishOrRegister(t *testing.T) {
	h, s, model := runTestHub(t)
	p := &fakeWebProvider{fetchStarted: make(chan struct{}), fetchRelease: make(chan struct{})}
	enableTestWeb(h, p)
	room, _ := s.GetRoomID("公共聊天")
	c := runClient(h, "alice", "", room)
	r := enqueueTestRun(t, c, "read URL", false)
	call := nextCall(t, model)
	fetch := toolNamed(t, call.tools, "web_fetch")
	done := make(chan error, 1)
	go func() {
		_, err := fetch.Execute(context.Background(), json.RawMessage(`{"url":"https://example.com/release"}`))
		done <- err
	}()
	<-p.fetchStarted
	c.handleBotCancel(Message{RunID: r.ID})
	close(p.fetchRelease)
	if err := <-done; err == nil {
		t.Fatal("late provider result escaped cancellation fence")
	}
	finishCall(call, "late answer")
	awaitStatus(t, s, r.ID, "cancelled")
	if _, err := s.GetMessageByID(r.MessageID); err == nil {
		t.Fatal("cancelled final published")
	}
}

func TestCitationStreamChunkBoundaries(t *testing.T) {
	text := "A [[source:server-id]] B"
	for split := 0; split <= len(text); split++ {
		var s citationStream
		got := s.push(text[:split]) + s.push(text[split:])
		if got != "A  B" {
			t.Fatalf("split %d: %q", split, got)
		}
	}
	var stream citationStream
	got := stream.push("A [[source:"+strings.Repeat("x", 10000)) + stream.push("tail]") + stream.push("] B")
	if got != "A  B" || len(stream.pending) > 8 {
		t.Fatalf("long marker leaked or buffered: %q %d", got, len(stream.pending))
	}
}

func TestWebSnapshotFitsEncodedModelToolResult(t *testing.T) {
	h, s, model := runTestHub(t)
	p := &fakeWebProvider{content: strings.Repeat("\x00\"\\", 2000)}
	enableTestWeb(h, p)
	room, _ := s.GetRoomID("公共聊天")
	c := runClient(h, "alice", "", room)
	r := enqueueTestRun(t, c, "read escaped page", false)
	call := nextCall(t, model)
	out, err := toolNamed(t, call.tools, "web_fetch").Execute(context.Background(), json.RawMessage(`{"url":"https://example.com/escaped"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(out)) > websearch.MaxToolJSONRunes || !json.Valid([]byte(out)) {
		t.Fatal("model received truncated JSON")
	}
	var payload struct {
		SourceID  string `json:"source_id"`
		Content   string `json:"content"`
		Truncated bool   `json:"truncated"`
	}
	_ = json.Unmarshal([]byte(out), &payload)
	source, err := s.GetWebSource(r.ID, payload.SourceID)
	if err != nil || source.Content != payload.Content || source.Content == "" || !payload.Truncated {
		t.Fatalf("snapshot differs from what model read: %v", err)
	}
	finishCall(call, "done")
	awaitStatus(t, s, r.ID, "completed")
}

// Tool failures must give the model a stable code and a recovery hint without
// leaking provider/store internals into the conversation context.
func TestWebToolErrorsAreCodedAndActionable(t *testing.T) {
	h, s, model := runTestHub(t)
	p := &fakeWebProvider{}
	enableTestWeb(h, p)
	room, _ := s.GetRoomID("公共聊天")
	c := runClient(h, "alice", "", room)
	enqueueTestRun(t, c, "research this", false)
	call := nextCall(t, model)
	search := toolNamed(t, call.tools, "web_search")
	fetch := toolNamed(t, call.tools, "web_fetch")

	cases := []struct {
		name string
		tool llm.Tool
		args string
		code string
	}{
		{"search invalid json", search, `{"query":`, "invalid_arguments"},
		{"search unknown field", search, `{"query":"a","debug":true}`, "invalid_arguments"},
		{"search empty query", search, `{"query":"  "}`, "invalid_query"},
		{"search overlong query", search, `{"query":"` + strings.Repeat("q", 129) + `"}`, "invalid_query"},
		{"search overlong limit", search, `{"query":"a","limit":9}`, "invalid_query"},
		{"fetch invalid json", fetch, `{"url":`, "invalid_arguments"},
		{"fetch both targets", fetch, `{"source_id":"x","url":"https://example.com"}`, "invalid_target"},
		{"fetch no target", fetch, `{}`, "invalid_target"},
		{"fetch unsafe url", fetch, `{"url":"http://127.0.0.1/x"}`, "invalid_url"},
		{"fetch unknown source", fetch, `{"source_id":"nope"}`, "unknown_source"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.tool.Execute(context.Background(), json.RawMessage(tc.args)); err == nil || !strings.Contains(err.Error(), tc.code+":") {
				t.Fatalf("err = %v, want a coded %s error", err, tc.code)
			}
		})
	}
	if p.searchCalls != 0 || p.fetchCalls != 0 {
		t.Fatalf("invalid input reached the provider: %d %d", p.searchCalls, p.fetchCalls)
	}
}

func TestWebToolProviderAndBudgetErrorsAreCoded(t *testing.T) {
	t.Run("provider failure keeps its category", func(t *testing.T) {
		h, s, model := runTestHub(t)
		p := &fakeWebProvider{searchErr: &websearch.Error{Code: "provider_limit"}}
		enableTestWeb(h, p)
		room, _ := s.GetRoomID("公共聊天")
		c := runClient(h, "alice", "", room)
		enqueueTestRun(t, c, "research this", false)
		call := nextCall(t, model)
		_, err := toolNamed(t, call.tools, "web_search").Execute(context.Background(), json.RawMessage(`{"query":"release"}`))
		if err == nil || !strings.Contains(err.Error(), "web_search_failed:") || !strings.Contains(err.Error(), "provider_limit") {
			t.Fatalf("provider error not mapped: %v", err)
		}
	})
	t.Run("exhausted budget stops retries", func(t *testing.T) {
		h, s, model := runTestHub(t)
		p := &fakeWebProvider{}
		enableTestWeb(h, p)
		h.llmCfg.WebSearch.DailyMicroCNY = 0
		room, _ := s.GetRoomID("公共聊天")
		c := runClient(h, "alice", "", room)
		enqueueTestRun(t, c, "research this", false)
		call := nextCall(t, model)
		_, err := toolNamed(t, call.tools, "web_search").Execute(context.Background(), json.RawMessage(`{"query":"release"}`))
		if err == nil || !strings.Contains(err.Error(), "web_unavailable:") {
			t.Fatalf("budget error not mapped: %v", err)
		}
		if p.searchCalls != 0 {
			t.Fatal("provider called without a reservation")
		}
	})
}
