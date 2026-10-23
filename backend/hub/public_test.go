package hub

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"tokendancechat/backend/llm"
)

func TestPublicStreamBoundary(t *testing.T) {
	secretTool := &llm.ToolCall{ID: "secret-id", Name: "lookup", Arguments: "secret input", Result: "secret result"}
	parts := []llm.Part{
		{Type: "reasoning", Text: "secret thinking"},
		{Type: "tool-call", Arguments: "secret input", Result: "secret result"},
		{Type: "text", Text: "answer", ToolCallID: "secret-id", Arguments: "secret input", Result: "secret result"},
	}
	for _, roomID := range []string{"room-1"} {
		t.Run("room="+roomID, func(t *testing.T) {
			h := New(&mockStore{}, nil, "TokenBot")
			member := &Client{hub: h, username: "alice", send: make(chan []byte, 10), currentRoomID: roomID}
			other := &Client{hub: h, username: "bob", send: make(chan []byte, 10), currentRoomID: "room-2"}
			h.clients[member], h.clients[other] = true, true
			output := member.send
			if roomID == "" {
				output = h.broadcast
			}

			for _, kind := range []string{"reasoning", "tool_call", "tool_result", "future-event", ""} {
				h.BroadcastStreamEventToRoom("TokenBot", "turn-1", llm.Event{Kind: kind, Text: "secret", ToolCall: secretTool, Parts: parts}, roomID)
				select {
				case raw := <-output:
					t.Fatalf("%q event crossed public boundary: %s", kind, raw)
				default:
				}
			}

			for _, kind := range []string{"text", "done", "error"} {
				event := llm.Event{Kind: kind, Text: "answer", ToolCall: secretTool, Parts: parts}
				if kind == "done" {
					event.Text = "secret final payload"
				}
				h.BroadcastStreamEventToRoom("TokenBot", "turn-1", event, roomID)
				select {
				case raw := <-output:
					if strings.Contains(string(raw), "secret") {
						t.Fatalf("%q leaked a hidden field: %s", kind, raw)
					}
					var msg Message
					if err := json.Unmarshal(raw, &msg); err != nil {
						t.Fatal(err)
					}
					if msg.Kind != kind || msg.StreamID != "turn-1" || msg.Private || msg.ToolCall != nil || msg.Done != (kind != "text") {
						t.Fatalf("unexpected %q stream message: %+v", kind, msg)
					}
					if kind == "done" {
						if !reflect.DeepEqual(msg.Parts, []llm.Part{{Type: "text", Text: "answer"}}) || msg.Content != "" {
							t.Fatalf("unexpected final snapshot: %+v", msg)
						}
					} else if msg.Content != "answer" || len(msg.Parts) != 0 {
						t.Fatalf("unexpected %q public payload: %+v", kind, msg)
					}
				default:
					t.Fatalf("missing %q stream event", kind)
				}
			}
			select {
			case raw := <-other.send:
				t.Fatalf("event crossed room boundary: %s", raw)
			default:
			}
		})
	}
}

func TestPrivateStreamPreservesReasoningAndTools(t *testing.T) {
	h := New(&mockStore{}, nil, "TokenBot")
	owner := &Client{hub: h, username: "alice", send: make(chan []byte, 10)}
	other := &Client{hub: h, username: "bob", send: make(chan []byte, 10)}
	h.clients[owner], h.clients[other] = true, true
	tool := &llm.ToolCall{ID: "call-1", Name: "lookup", Arguments: "private input", Result: "private result"}
	parts := []llm.Part{{Type: "reasoning", Text: "private thinking"}, {Type: "tool-call", Arguments: "private input", Result: "private result"}, {Type: "text", Text: "answer"}}
	for _, kind := range []string{"reasoning", "tool_call", "tool_result", "text", "done"} {
		event := llm.Event{Kind: kind, Text: "private delta", ToolCall: tool, Parts: parts}
		h.SendStreamEventToClient(owner, "TokenBot", "private-turn", event, true)
		select {
		case raw := <-owner.send:
			var msg Message
			if err := json.Unmarshal(raw, &msg); err != nil {
				t.Fatal(err)
			}
			if !msg.Private || msg.Kind != kind || msg.Content != event.Text || !reflect.DeepEqual(msg.ToolCall, tool) || !reflect.DeepEqual(msg.Parts, parts) {
				t.Fatalf("private event changed: %+v", msg)
			}
		default:
			t.Fatalf("missing private %q event", kind)
		}
	}
	if len(other.send) != 0 || len(h.broadcast) != 0 {
		t.Fatal("private stream reached another recipient")
	}
}

func TestPublishAssistantMessagePublicChannel(t *testing.T) {
	ms := &mockStore{}
	h := New(ms, nil, "TokenBot")
	parts := []llm.Part{{Type: "reasoning", Text: "secret thinking"}, {Type: "text", Text: "answer", Result: "secret result"}}
	member := &Client{hub: h, username: "alice", send: make(chan []byte, 10), currentRoomID: "room-1"}
	h.clients[member] = true
	stored, err := h.PublishAssistantMessage("TokenBot", "answer", "room-1", parts)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored.Parts) != `[{"type":"text","text":"answer"}]` {
		t.Fatalf("public store input = %s", stored.Parts)
	}
	select {
	case raw := <-member.send:
		if strings.Contains(string(raw), "secret") {
			t.Fatalf("public broadcast leaked hidden data: %s", raw)
		}
		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatal(err)
		}
		if msg.Content != "answer" || !reflect.DeepEqual(msg.Parts, []llm.Part{{Type: "text", Text: "answer"}}) {
			t.Fatalf("public broadcast = %+v", msg)
		}
	default:
		t.Fatal("missing public broadcast")
	}
}

func TestPublishAssistantMessageRejectsEmptyRoom(t *testing.T) {
	ms := &mockStore{}
	h := New(ms, nil, "TokenBot")
	if _, err := h.PublishAssistantMessage("TokenBot", "secret", "", nil); err == nil {
		t.Fatal("empty room accepted")
	}
	if len(ms.messages) != 0 || len(h.broadcast) != 0 {
		t.Fatal("empty room persisted or broadcast")
	}
}
