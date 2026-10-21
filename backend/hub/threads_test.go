package hub

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"tokendancechat/backend/store"
)

func newThreadTestHub(t *testing.T) (*Hub, *store.Store) {
	t.Helper()
	t.Setenv("CHAT_SKIP_SEED", "true")
	s, err := store.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return New(s, nil, "TokenBot"), s
}

func threadTestClient(h *Hub, username, roomID string) *Client {
	c := &Client{hub: h, username: username, currentRoomID: roomID, send: make(chan []byte, 20)}
	h.clients[c] = true
	return c
}

func insertThreadFixture(t *testing.T, s *store.Store, username, roomID, toUser, threadID string) StoredMessage {
	t.Helper()
	m, err := s.InsertMessageWithParts(username, "fixture-secret-content", roomID, toUser, threadID,
		json.RawMessage(`[{"type":"reasoning","text":"fixture-secret-reasoning"},{"type":"text","text":"visible"}]`))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func readThreadTestMessage(t *testing.T, c *Client) (Message, string) {
	t.Helper()
	select {
	case raw := <-c.send:
		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatal(err)
		}
		return msg, string(raw)
	default:
		t.Fatal("expected a message")
		return Message{}, ""
	}
}

func TestThreadReadAuthorizesRealParent(t *testing.T) {
	tests := []struct {
		name, author, room, to string
		deleted, missing       bool
		allowed, private       bool
	}{
		{name: "current public room", author: "bob", room: "room-1", allowed: true},
		{name: "different public room", author: "alice", room: "room-2"},
		{name: "own private outgoing", author: "alice", to: "TokenBot", allowed: true, private: true},
		{name: "own private incoming", author: "TokenBot", to: "alice", allowed: true, private: true},
		{name: "other private outgoing", author: "bob", to: "TokenBot"},
		{name: "other private incoming", author: "TokenBot", to: "bob"},
		{name: "not a bot conversation", author: "alice", to: "bob"},
		{name: "private row with room", author: "TokenBot", to: "alice", room: "room-1"},
		{name: "deleted", author: "bob", room: "room-1", deleted: true},
		{name: "missing", missing: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, s := newThreadTestHub(t)
			alice := threadTestClient(h, "alice", "room-1")
			bob := threadTestClient(h, "bob", "room-2")
			parentID := "nonexistent"
			if !tt.missing {
				parent := insertThreadFixture(t, s, tt.author, tt.room, tt.to, "")
				parentID = parent.ID
				insertThreadFixture(t, s, tt.author, tt.room, tt.to, parentID)
				if tt.deleted {
					if err := s.MarkDeleted(parentID); err != nil {
						t.Fatal(err)
					}
				}
			}
			// Forged routing cannot override the persisted parent or session user.
			alice.handleThreadMessages(Message{ParentMessageID: parentID, Username: "bob", RoomID: tt.room, Private: !tt.private, ReplyToUser: "alice", To: "TokenBot"})
			got, raw := readThreadTestMessage(t, alice)
			if tt.allowed {
				if got.Type != "thread_messages" || len(got.Messages) != 1 || got.Private != tt.private || got.ParentMessageID != parentID {
					t.Fatalf("authorized thread = %+v", got)
				}
				if tt.private != strings.Contains(raw, "fixture-secret-reasoning") {
					t.Fatalf("incorrect parts boundary: %s", raw)
				}
			} else if got.Type != "error" || got.ErrorCode != "THREAD_UNAVAILABLE" || strings.Contains(raw, "fixture-secret") || len(got.Messages) != 0 || got.ParentMessageID != "" {
				t.Fatalf("inaccessible parent response = %s", raw)
			}
			if len(bob.send) != 0 || len(h.broadcast) != 0 {
				t.Fatal("thread response reached another recipient")
			}
		})
	}
}

func TestThreadChildrenStayInParentConversation(t *testing.T) {
	for _, private := range []bool{false, true} {
		name := "public"
		if private {
			name = "private"
		}
		t.Run(name, func(t *testing.T) {
			h, s := newThreadTestHub(t)
			alice := threadTestClient(h, "alice", "room-1")
			bob := threadTestClient(h, "bob", "room-2")
			room, to := "room-1", ""
			if private {
				room, to = "", "TokenBot"
			}
			parent := insertThreadFixture(t, s, "alice", room, to, "")
			first := insertThreadFixture(t, s, "alice", room, to, parent.ID)
			otherAuthor, otherTo := "bob", ""
			if private {
				otherAuthor, otherTo = "TokenBot", "alice"
			}
			second := insertThreadFixture(t, s, otherAuthor, room, otherTo, parent.ID)
			// Emulate legacy rows with malicious cross-scope thread IDs.
			insertThreadFixture(t, s, "bob", "room-2", "", parent.ID)
			insertThreadFixture(t, s, "bob", "", "TokenBot", parent.ID)
			insertThreadFixture(t, s, "TokenBot", "", "bob", parent.ID)
			insertThreadFixture(t, s, "TokenBot", "room-1", "alice", parent.ID)
			if private {
				insertThreadFixture(t, s, "alice", "room-1", "", parent.ID)
			} else {
				insertThreadFixture(t, s, "TokenBot", "", "alice", parent.ID)
			}
			deleted := insertThreadFixture(t, s, "alice", room, to, parent.ID)
			if err := s.MarkDeleted(deleted.ID); err != nil {
				t.Fatal(err)
			}
			wantIDs := map[string]bool{first.ID: true, second.ID: true}
			assertChildren := func(messages []StoredMessage) {
				t.Helper()
				gotIDs := make(map[string]bool)
				for _, msg := range messages {
					gotIDs[msg.ID] = true
				}
				if !reflect.DeepEqual(gotIDs, wantIDs) {
					t.Fatalf("thread included cross-scope children: got %v, want %v", gotIDs, wantIDs)
				}
			}
			assertChildren(s.GetThreadMessages(parent.ID))
			if count := s.GetThreadReplyCount(parent.ID); count != 2 {
				t.Fatalf("count includes cross-scope children: %d", count)
			}
			alice.handleThreadMessages(Message{ID: parent.ID})
			got, _ := readThreadTestMessage(t, alice)
			assertChildren(got.Messages)
			if len(bob.send) != 0 || len(h.broadcast) != 0 {
				t.Fatal("thread response reached another recipient")
			}
			if err := s.MarkDeleted(parent.ID); err != nil {
				t.Fatal(err)
			}
			if len(s.GetThreadMessages(parent.ID)) != 0 || s.GetThreadReplyCount(parent.ID) != 0 {
				t.Fatal("deleted parent still exposes children")
			}
		})
	}
}

func TestThreadWritesRejectCrossConversationReferences(t *testing.T) {
	tests := []struct {
		name, author, room, to string
		private, deleted       bool
	}{
		{name: "public to other room", author: "bob", room: "room-2"},
		{name: "public to own private", author: "TokenBot", to: "alice"},
		{name: "public to other private", author: "TokenBot", to: "bob"},
		{name: "private to public", author: "bob", room: "room-1", private: true},
		{name: "private to other outgoing", author: "bob", to: "TokenBot", private: true},
		{name: "private to other incoming", author: "TokenBot", to: "bob", private: true},
		{name: "private to non-bot", author: "alice", to: "bob", private: true},
		{name: "deleted public", author: "bob", room: "room-1", deleted: true},
		{name: "deleted private", author: "TokenBot", to: "alice", private: true, deleted: true},
		{name: "missing public"},
		{name: "missing private", private: true},
	}
	for _, tt := range tests {
		for _, field := range []string{"thread", "reply"} {
			t.Run(tt.name+"/"+field, func(t *testing.T) {
				h, s := newThreadTestHub(t)
				alice := threadTestClient(h, "alice", "room-1")
				bob := threadTestClient(h, "bob", "room-1")
				parentID := "nonexistent"
				if tt.author != "" {
					parent := insertThreadFixture(t, s, tt.author, tt.room, tt.to, "")
					parentID = parent.ID
					if tt.deleted {
						if err := s.MarkDeleted(parentID); err != nil {
							t.Fatal(err)
						}
					}
				}
				count := s.TotalMessages()
				msg := Message{Content: "injected", Username: "bob", RoomID: tt.room, Private: !tt.private, ReplyToUser: "TokenBot", ReplyToContent: "forged"}
				if tt.private {
					msg.To = "TokenBot"
				}
				if field == "thread" {
					msg.ThreadID = parentID
				} else {
					msg.ReplyToID = parentID
				}
				alice.handleChatMessage(msg)
				got, raw := readThreadTestMessage(t, alice)
				if got.Type != "error" || got.ErrorCode != "MESSAGE_REFERENCE_UNAVAILABLE" || strings.Contains(raw, "fixture-secret") {
					t.Fatalf("rejected write returned data: %s", raw)
				}
				if s.TotalMessages() != count || len(bob.send) != 0 || len(h.broadcast) != 0 || len(alice.send) != 0 {
					t.Fatal("rejected write persisted or broadcast a message")
				}
			})
		}
	}
}

func TestThreadWritesPreserveLegitimateRepliesAndDeriveMetadata(t *testing.T) {
	for _, private := range []bool{false, true} {
		name := "public"
		if private {
			name = "private"
		}
		t.Run(name, func(t *testing.T) {
			h, s := newThreadTestHub(t)
			alice := threadTestClient(h, "alice", "room-1")
			bob := threadTestClient(h, "bob", "room-1")
			otherRoom := threadTestClient(h, "charlie", "room-2")
			author, room, to := "bob", "room-1", ""
			if private {
				author, room, to = "TokenBot", "", "alice"
			}
			parent := insertThreadFixture(t, s, author, room, to, "")
			msg := Message{Content: "reply", ThreadID: parent.ID, ReplyToID: parent.ID, ReplyToUser: "forged author", ReplyToContent: "forged text", Private: !private, RoomID: "room-2"}
			if private {
				msg.To = "TokenBot"
			}
			alice.handleChatMessage(msg)
			got, _ := readThreadTestMessage(t, alice)
			if got.Type != "message" || got.ThreadID != parent.ID || got.ReplyToID != parent.ID || got.Private != private || got.ReplyToUser != author || got.ReplyToContent != parent.Content || got.Username != "alice" {
				t.Fatalf("legitimate reply changed or accepted forged metadata: %+v", got)
			}
			stored, err := s.GetMessageByID(got.ID)
			if err != nil || stored.ThreadID != parent.ID || stored.ReplyToID != parent.ID || stored.RoomID != room {
				t.Fatalf("stored reply = %+v, error = %v", stored, err)
			}
			if private && (stored.ToUser != "TokenBot" || len(bob.send) != 0) {
				t.Fatal("private reply escaped its conversation")
			}
			if !private && len(bob.send) != 1 {
				t.Fatal("public reply did not reach its room")
			}
			if len(otherRoom.send) != 0 || len(h.broadcast) != 0 {
				t.Fatal("reply reached another room")
			}
		})
	}
}

func TestReplyUsernameCannotForgeBotAddressing(t *testing.T) {
	h, s := newThreadTestHub(t)
	alice := threadTestClient(h, "alice", "room-1")
	alice.handleChatMessage(Message{Content: "hello", ReplyToUser: "TokenBot", ReplyToContent: "forged reply"})
	got, _ := readThreadTestMessage(t, alice)
	if got.Type != "message" || got.ReplyToUser != "" || got.ReplyToContent != "" || len(alice.send) != 0 || s.TotalMessages() != 1 {
		t.Fatalf("forged metadata affected message or bot addressing: %+v", got)
	}
}

func TestUnknownPrivateRecipientNeverFallsBackToPublic(t *testing.T) {
	for _, recipient := range []string{"bob", "OtherBot", "tokenbot", " TokenBot "} {
		t.Run(recipient, func(t *testing.T) {
			h, s := newThreadTestHub(t)
			alice := threadTestClient(h, "alice", "room-1")
			bob := threadTestClient(h, "bob", "room-1")
			otherRoom := threadTestClient(h, "charlie", "room-2")
			alice.handleChatMessage(Message{Content: "private-secret-content", To: recipient, Private: true, ReplyToUser: "TokenBot"})
			got, raw := readThreadTestMessage(t, alice)
			if got.Type != "error" || got.ErrorCode != "INVALID_RECIPIENT" || strings.Contains(raw, "private-secret") {
				t.Fatalf("unexpected rejection: %s", raw)
			}
			if s.TotalMessages() != 0 || len(bob.send) != 0 || len(otherRoom.send) != 0 || len(h.broadcast) != 0 || len(alice.send) != 0 {
				t.Fatal("unknown private recipient persisted or broadcast the message")
			}
		})
	}
}

func TestReplyReferenceCannotBypassValidThreadScope(t *testing.T) {
	h, s := newThreadTestHub(t)
	alice := threadTestClient(h, "alice", "room-1")
	publicParent := insertThreadFixture(t, s, "bob", "room-1", "", "")
	privateParent := insertThreadFixture(t, s, "TokenBot", "", "bob", "")
	before := s.TotalMessages()
	alice.handleChatMessage(Message{Content: "reply", ThreadID: publicParent.ID, ReplyToID: privateParent.ID})
	got, raw := readThreadTestMessage(t, alice)
	if got.Type != "error" || got.ErrorCode != "MESSAGE_REFERENCE_UNAVAILABLE" || strings.Contains(raw, "fixture-secret") || s.TotalMessages() != before {
		t.Fatalf("valid thread allowed an unauthorized reply reference: %s", raw)
	}
}
