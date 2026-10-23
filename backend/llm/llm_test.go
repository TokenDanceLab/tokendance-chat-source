package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openai/openai-go/shared"
)

// testClient points the SDK at a local OpenAI-compatible stub.
func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return New(Config{
		APIKey:  "test-key",
		Model:   "test-model",
		BaseURL: server.URL,
	})
}

func writeSSE(w http.ResponseWriter, chunks ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, chunk := range chunks {
		fmt.Fprintf(w, "data: %s\n\n", chunk)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func TestNewDefaults(t *testing.T) {
	client := New(Config{})
	if client.Provider() != "openai" {
		t.Errorf("provider = %q, want openai", client.Provider())
	}
	if client.MaxTokens() != DefaultMaxTokens {
		t.Errorf("max tokens = %d, want %d", client.MaxTokens(), DefaultMaxTokens)
	}
	if client.MaxToolRounds() != DefaultMaxToolRounds {
		t.Errorf("max tool rounds = %d, want %d", client.MaxToolRounds(), DefaultMaxToolRounds)
	}
}

func TestChatReturnsContent(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":" OK "}}]}`)
	})

	got, err := client.Chat(context.Background(), "system", []Message{{Role: "user", Content: "ping"}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got != "OK" {
		t.Errorf("content = %q, want OK", got)
	}
}

func TestChatRejectsReasoningOnlyReply(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"","reasoning_content":"thinking"}}]}`)
	})

	_, err := client.Chat(context.Background(), "system", []Message{{Role: "user", Content: "ping"}})
	if err == nil || !strings.Contains(err.Error(), "empty message content") {
		t.Fatalf("err = %v, want empty message content", err)
	}
}

func TestChatStreamEmitsTextAndReasoningParts(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeSSE(w,
			`{"choices":[{"delta":{"reasoning_content":"plan "}}]}`,
			`{"choices":[{"delta":{"reasoning_content":"the answer"}}]}`,
			`{"choices":[{"delta":{"content":"Hello"}}]}`,
			`{"choices":[{"delta":{"content":" world"}}]}`,
		)
	})

	var kinds []string
	result, err := client.ChatStream(context.Background(), "system", []Message{{Role: "user", Content: "hi"}}, nil, func(event Event) error {
		kinds = append(kinds, event.Kind)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if result.Text != "Hello world" {
		t.Errorf("text = %q", result.Text)
	}
	if len(result.Parts) != 2 || result.Parts[0].Type != "reasoning" || result.Parts[1].Type != "text" {
		t.Fatalf("parts = %+v", result.Parts)
	}
	if result.Parts[0].Text != "plan the answer" {
		t.Errorf("reasoning = %q", result.Parts[0].Text)
	}
	if want := []string{"reasoning", "reasoning", "text", "text", "done"}; strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Errorf("events = %v, want %v", kinds, want)
	}
}

func TestChatStreamPropagatesHandlerError(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeSSE(w, `{"choices":[{"delta":{"content":"first"}}]}`)
	})

	abort := errors.New("client went away")
	var events int
	_, err := client.ChatStream(context.Background(), "system", []Message{{Role: "user", Content: "hi"}}, nil, func(Event) error {
		events++
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatalf("err = %v, want %v", err, abort)
	}
	if events != 1 {
		t.Errorf("handler called %d times, want 1", events)
	}
}

type stubTool struct {
	name    string
	output  string
	execErr error
	calls   int
}

func (s *stubTool) Name() string        { return s.name }
func (s *stubTool) Description() string { return "stub" }
func (s *stubTool) Parameters() shared.FunctionParameters {
	return shared.FunctionParameters{"type": "object", "properties": map[string]any{}}
}
func (s *stubTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	s.calls++
	return s.output, s.execErr
}

func TestChatStreamExecutesParallelToolCallsSequentially(t *testing.T) {
	var requests atomic.Int32
	var secondRequestBody map[string]any

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		round := requests.Add(1)
		if round == 1 {
			// Some OpenAI-compatible providers ignore parallel_tool_calls=false.
			writeSSE(w,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"a\"}"}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"b\"}"}}]}}]}`,
			)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&secondRequestBody); err != nil {
			t.Errorf("decode second request: %v", err)
		}
		writeSSE(w, `{"choices":[{"delta":{"content":"Both done."}}]}`)
	})

	tool := &stubTool{name: "lookup", output: `{"ok":true}`}
	result, err := client.ChatStream(context.Background(), "system", []Message{{Role: "user", Content: "two lookups"}}, []Tool{tool}, nil)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want 2", requests.Load())
	}
	if tool.calls != 2 {
		t.Fatalf("tool calls = %d, want 2 executed sequentially", tool.calls)
	}
	if result.Text != "Both done." {
		t.Errorf("text = %q", result.Text)
	}

	messages, _ := secondRequestBody["messages"].([]any)
	if len(messages) < 2 {
		t.Fatalf("follow-up messages = %d", len(messages))
	}
	last, _ := messages[len(messages)-1].(map[string]any)
	previous, _ := messages[len(messages)-2].(map[string]any)
	if last["role"] != "tool" || previous["role"] != "tool" {
		t.Fatalf("follow-up did not carry both tool results: %+v", messages)
	}
}

func TestChatStreamExecutesToolAndContinues(t *testing.T) {
	var requests atomic.Int32
	var secondRequestBody map[string]any

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		round := requests.Add(1)
		if round == 1 {
			writeSSE(w,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":"}}]}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"weather\"}"}}]}}]}`,
			)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&secondRequestBody); err != nil {
			t.Errorf("decode second request: %v", err)
		}
		writeSSE(w, `{"choices":[{"delta":{"content":"Sunny."}}]}`)
	})

	tool := &stubTool{name: "lookup", output: `{"temp":21}`}
	var toolEvents []Event
	result, err := client.ChatStream(context.Background(), "system", []Message{{Role: "user", Content: "weather?"}}, []Tool{tool}, func(event Event) error {
		if strings.HasPrefix(event.Kind, "tool_") {
			toolEvents = append(toolEvents, event)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want 2", requests.Load())
	}
	if tool.calls != 1 {
		t.Errorf("tool calls = %d, want 1", tool.calls)
	}
	if result.Text != "Sunny." {
		t.Errorf("text = %q", result.Text)
	}
	if len(toolEvents) != 2 || toolEvents[0].Kind != "tool_call" || toolEvents[1].Kind != "tool_result" {
		t.Fatalf("tool events = %+v", toolEvents)
	}
	if toolEvents[0].ToolCall.Arguments != `{"q":"weather"}` {
		t.Errorf("assembled arguments = %q", toolEvents[0].ToolCall.Arguments)
	}
	if toolEvents[1].ToolCall.Result != `{"temp":21}` || toolEvents[1].ToolCall.Status != "complete" {
		t.Errorf("tool result = %+v", toolEvents[1].ToolCall)
	}

	messages, _ := secondRequestBody["messages"].([]any)
	last, _ := messages[len(messages)-1].(map[string]any)
	if last["role"] != "tool" || last["content"] != `{"temp":21}` {
		t.Errorf("follow-up request did not carry the tool result: %+v", last)
	}
}

func TestChatStreamStopsAtToolLimit(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeSSE(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"loop","arguments":"{}"}}]}}]}`)
	})
	tool := &stubTool{name: "loop", output: "again"}

	_, err := client.ChatStream(context.Background(), "system", []Message{{Role: "user", Content: "go"}}, []Tool{tool}, nil)
	if err == nil || !strings.Contains(err.Error(), "tool loop limit") {
		t.Fatalf("err = %v, want tool loop limit", err)
	}
	if tool.calls != DefaultMaxToolRounds {
		t.Errorf("tool calls = %d, want %d", tool.calls, DefaultMaxToolRounds)
	}
}

func TestChatStreamBoundsToolLoopGrowth(t *testing.T) {
	var requests atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeSSE(w,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"big","arguments":"{}"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"big","arguments":"{}"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":2,"id":"call_3","type":"function","function":{"name":"big","arguments":"{}"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":3,"id":"call_4","type":"function","function":{"name":"big","arguments":"{}"}}]}}]}`,
		)
	})
	// CJK output makes each 8,000-rune capped result ~24KB on the wire, so the
	// second round of four parallel calls crosses the growth budget.
	tool := &stubTool{name: "big", output: strings.Repeat("字", 9000)}

	_, err := client.ChatStream(context.Background(), "system", []Message{{Role: "user", Content: "go"}}, []Tool{tool}, nil)
	if err == nil || !strings.Contains(err.Error(), "growth limit") {
		t.Fatalf("err = %v, want growth limit", err)
	}
	if requests.Load() != 2 {
		t.Errorf("requests = %d, want 2 (the second round trips the cap)", requests.Load())
	}
	if tool.calls >= 8 {
		t.Errorf("tool calls = %d, want the round aborted before all 8 calls", tool.calls)
	}
}

func TestChatStreamUnknownToolGetsActionableError(t *testing.T) {
	var requests atomic.Int32
	var secondRequestBody map[string]any
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			writeSSE(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"teleport","arguments":"{}"}}]}}]}`)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&secondRequestBody); err != nil {
			t.Errorf("decode second request: %v", err)
		}
		writeSSE(w, `{"choices":[{"delta":{"content":"I cannot do that."}}]}`)
	})

	var toolEvents []Event
	result, err := client.ChatStream(context.Background(), "system", []Message{{Role: "user", Content: "go"}}, []Tool{&stubTool{name: "lookup", output: "ok"}}, func(event Event) error {
		if strings.HasPrefix(event.Kind, "tool_") {
			toolEvents = append(toolEvents, event)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if result.Text != "I cannot do that." {
		t.Errorf("text = %q", result.Text)
	}
	if len(toolEvents) != 2 || toolEvents[1].ToolCall.Status != "error" {
		t.Fatalf("tool events = %+v", toolEvents)
	}
	if got := toolEvents[1].ToolCall.Result; !strings.Contains(got, "unknown_tool") || !strings.Contains(got, `"teleport"`) {
		t.Errorf("unknown tool result not actionable: %q", got)
	}
	messages, _ := secondRequestBody["messages"].([]any)
	last, _ := messages[len(messages)-1].(map[string]any)
	content, _ := last["content"].(string)
	if last["role"] != "tool" || !strings.Contains(content, "unknown_tool") {
		t.Errorf("follow-up request did not carry the actionable error: %+v", last)
	}
}

func TestSpeakerLineLabelsAuthorAndTime(t *testing.T) {
	sent := time.Date(2026, 10, 20, 14, 30, 0, 0, time.FixedZone("HKT", 8*60*60)).UnixMilli()
	line := speakerLine(Message{Role: "user", Username: "alice", Content: "几点？", Timestamp: sent})
	if !strings.HasPrefix(line, "@alice · 2026-10-20 14:30\n") || !strings.HasSuffix(line, "几点？") {
		t.Fatalf("speaker line = %q", line)
	}
	if got := speakerLine(Message{Role: "user", Content: "plain"}); got != "plain" {
		t.Errorf("unlabelled message = %q", got)
	}
}

func TestChatStreamCapturesTokenUsage(t *testing.T) {
	t.Run("openai cached tokens", func(t *testing.T) {
		client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			writeSSE(w,
				`{"choices":[{"delta":{"content":"ok"}}]}`,
				`{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":80}}}`,
			)
		})
		result, err := client.ChatStream(context.Background(), "system", []Message{{Role: "user", Content: "hi"}}, nil, nil)
		if err != nil {
			t.Fatalf("ChatStream: %v", err)
		}
		if result.Usage.PromptTokens != 100 || result.Usage.CachedTokens != 80 || result.Usage.CompletionTokens != 10 {
			t.Fatalf("usage = %+v", result.Usage)
		}
	})

	t.Run("deepseek cache hit field", func(t *testing.T) {
		client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			writeSSE(w,
				`{"choices":[{"delta":{"content":"ok"}}]}`,
				`{"choices":[],"usage":{"prompt_tokens":64,"completion_tokens":8,"prompt_cache_hit_tokens":48}}`,
			)
		})
		result, err := client.ChatStream(context.Background(), "system", []Message{{Role: "user", Content: "hi"}}, nil, nil)
		if err != nil {
			t.Fatalf("ChatStream: %v", err)
		}
		if result.Usage.CachedTokens != 48 || result.Usage.PromptTokens != 64 {
			t.Fatalf("usage = %+v", result.Usage)
		}
	})

	t.Run("tool rounds add up", func(t *testing.T) {
		var calls atomic.Int32
		client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) == 1 {
				writeSSE(w,
					`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}}]}`,
					`{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":60}}}`,
				)
				return
			}
			writeSSE(w,
				`{"choices":[{"delta":{"content":"done"}}]}`,
				`{"choices":[],"usage":{"prompt_tokens":120,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":90}}}`,
			)
		})
		result, err := client.ChatStream(context.Background(), "system", []Message{{Role: "user", Content: "go"}}, []Tool{&stubTool{name: "lookup", output: "ok"}}, nil)
		if err != nil {
			t.Fatalf("ChatStream: %v", err)
		}
		if result.Usage.PromptTokens != 220 || result.Usage.CachedTokens != 150 || result.Usage.CompletionTokens != 12 {
			t.Fatalf("usage must sum both rounds, got %+v", result.Usage)
		}
	})
}

func TestChatStreamUsageRequiresCompleteNonnegativeCounts(t *testing.T) {
	tests := []struct {
		name, usage string
		known       bool
	}{
		{"both counters", `{"prompt_tokens":10,"completion_tokens":3}`, true},
		{"explicit zero counters", `{"prompt_tokens":0,"completion_tokens":0}`, true},
		{"missing completion", `{"prompt_tokens":10}`, false},
		{"missing prompt", `{"completion_tokens":3}`, false},
		{"null completion", `{"prompt_tokens":10,"completion_tokens":null}`, false},
		{"negative prompt", `{"prompt_tokens":-1,"completion_tokens":3}`, false},
		{"negative completion", `{"prompt_tokens":10,"completion_tokens":-1}`, false},
		{"fractional completion", `{"prompt_tokens":10,"completion_tokens":1.5}`, false},
		{"string completion", `{"prompt_tokens":10,"completion_tokens":"3"}`, false},
		{"overflow combined count", `{"prompt_tokens":9223372036854775807,"completion_tokens":1}`, false},
		{"empty usage", `{}`, false},
		{"null usage", `null`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
				writeSSE(w, `{"choices":[{"delta":{"content":"answer"}}]}`, `{"choices":[],"usage":`+tc.usage+`}`)
			})
			result, err := client.ChatStream(context.Background(), "system", []Message{{Role: "user", Content: "hello"}}, nil, nil)
			if err != nil {
				if tc.known {
					t.Fatal(err)
				}
				return
			}
			if result.UsageKnown != tc.known {
				t.Fatalf("UsageKnown=%v for %s", result.UsageKnown, tc.usage)
			}
		})
	}
}

func TestChatStreamPartialToolRoundKeepsAllUsageUnknown(t *testing.T) {
	var calls atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			writeSSE(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}}]}`, `{"choices":[],"usage":{"prompt_tokens":10}}`)
			return
		}
		writeSSE(w, `{"choices":[{"delta":{"content":"answer"}}]}`, `{"choices":[],"usage":{"prompt_tokens":20,"completion_tokens":3}}`)
	})
	result, err := client.ChatStream(context.Background(), "system", []Message{{Role: "user", Content: "hello"}}, []Tool{&stubTool{name: "lookup", output: "ok"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.UsageKnown {
		t.Fatal("complete final round hid a partial earlier round")
	}
}

func TestChatSerializesImagesAsUserContentParts(t *testing.T) {
	const dataURL = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	var body map[string]any
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	})

	_, err := client.Chat(context.Background(), "system", []Message{
		{Role: "user", Content: "what is this?", Username: "alice", Images: []string{dataURL}},
		{Role: "assistant", Content: "an image", Images: []string{"data:image/png;base64,ignored"}},
		{Role: "user", Content: "no image here"},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if _, ok := body["messages"].([]any); !ok {
		t.Fatalf("messages = %T", body["messages"])
	}
	messages := body["messages"].([]any)
	if len(messages) != 4 {
		t.Fatalf("messages = %d, want 4", len(messages))
	}

	withImage, _ := messages[1].(map[string]any)
	parts, ok := withImage["content"].([]any)
	if !ok {
		t.Fatalf("user content = %T, want array", withImage["content"])
	}
	if len(parts) != 2 {
		t.Fatalf("parts = %d, want text + image", len(parts))
	}
	text, _ := parts[0].(map[string]any)
	if text["type"] != "text" || text["text"] != "@alice\nwhat is this?" {
		t.Errorf("text part = %+v", text)
	}
	imagePart, _ := parts[1].(map[string]any)
	imageURL, _ := imagePart["image_url"].(map[string]any)
	if imagePart["type"] != "image_url" || imageURL["url"] != dataURL {
		t.Errorf("image part = %+v", imagePart)
	}

	// Assistant turns stay plain text even if images were set by mistake.
	assistant, _ := messages[2].(map[string]any)
	if got, ok := assistant["content"].(string); !ok || got != "an image" {
		t.Errorf("assistant content = %#v, want string", assistant["content"])
	}
	// User messages without images keep the compact string form.
	plain, _ := messages[3].(map[string]any)
	if got, ok := plain["content"].(string); !ok || got != "no image here" {
		t.Errorf("plain user content = %#v, want string", plain["content"])
	}
}
