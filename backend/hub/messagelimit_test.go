package hub

import (
	"encoding/json"
	"testing"
)

func TestHubMessageLimitPerIP(t *testing.T) {
	t.Setenv("CHAT_MSG_PER_IP_PER_10S", "2")
	h := New(&mockStore{}, nil, "")

	if !h.allowClientMessage("1.2.3.4") || !h.allowClientMessage("1.2.3.4") {
		t.Fatal("first two messages should pass")
	}
	if h.allowClientMessage("1.2.3.4") {
		t.Fatal("third message from the same IP should be denied")
	}
	if !h.allowClientMessage("5.6.7.8") {
		t.Fatal("another IP must keep its own budget")
	}
	if !h.allowClientMessage("") {
		t.Fatal("an empty IP disables the limit")
	}
}

func TestHandleChatMessageDeniesExhaustedIP(t *testing.T) {
	t.Setenv("CHAT_MSG_PER_IP_PER_10S", "1")
	h := New(&mockStore{}, nil, "")
	c := &Client{
		hub:           h,
		username:      "alice",
		ip:            "1.1.1.1",
		send:          make(chan []byte, 8),
		currentRoomID: h.DefaultRoomID(),
	}

	c.handleChatMessage(Message{Type: "message", Content: "one"})
	c.handleChatMessage(Message{Type: "message", Content: "two"})

	select {
	case raw := <-c.send:
		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if msg.ErrorCode != "RATE_LIMITED" {
			t.Fatalf("error code = %q, want RATE_LIMITED", msg.ErrorCode)
		}
	default:
		t.Fatal("expected a rate-limit error")
	}
}
