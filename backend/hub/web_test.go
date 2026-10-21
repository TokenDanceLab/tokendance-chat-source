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
	searchCalls, fetchCalls    int
	fetchStarted, fetchRelease chan struct{}
}

func (p *fakeWebProvider) Search(context.Context, string, int) ([]websearch.Hit, error) {
	p.searchCalls++
	return []websearch.Hit{{Title: "Official release", URL: "https://example.com/release", Summary: "Discovery only"}, {Title: "Unsafe", URL: "http://127.0.0.1/private"}}, nil
}
func (p *fakeWebProvider) Fetch(context.Context, string) (websearch.Page, error) {
	p.fetchCalls++
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
