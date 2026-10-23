package hub

import (
	"path/filepath"
	"testing"

	"tokendancechat/backend/llm"
	"tokendancechat/backend/store"
)

func newHistoryHub(t *testing.T, contextMessages int) (*Hub, *store.Store) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, &llm.Config{ContextMessages: contextMessages}, "TokenBot"), st
}

func TestHistoryIsScopedToConversation(t *testing.T) {
	h, st := newHistoryHub(t, 20)

	st.InsertMessage("alice", "room-a hello", "", "room-a", "", "", "")
	st.InsertMessage("bob", "room-b hello", "", "room-b", "", "", "")
	st.InsertMessage("TokenBot", "room-a reply", "", "room-a", "", "", "")
	st.InsertMessage("carol", "carol private", "", "", "TokenBot", "", "")
	st.InsertMessage("TokenBot", "carol reply", "", "", "carol", "", "")
	st.InsertMessage("dave", "dave private", "", "", "TokenBot", "", "")

	roomA := h.history("public", "room-a", "")
	if len(roomA) != 2 || roomA[0].Content != "room-a hello" || roomA[1].Role != "assistant" {
		t.Fatalf("room-a history = %+v", roomA)
	}
	if roomA[0].Timestamp == 0 {
		t.Error("history must carry message timestamps for the transcript labels")
	}
	for _, message := range roomA {
		if message.Username == "bob" || message.Content == "room-b hello" {
			t.Errorf("room-b message leaked into room-a history: %+v", message)
		}
	}

	private := h.history("private", "", "carol")
	if len(private) != 2 || private[0].Role != "user" || private[1].Role != "assistant" {
		t.Fatalf("carol history = %+v", private)
	}
	for _, message := range private {
		if message.Username == "dave" {
			t.Errorf("another user's DM leaked into carol's history: %+v", message)
		}
	}
}

func TestHistoryHonoursContextWindow(t *testing.T) {
	h, st := newHistoryHub(t, 2)
	for _, content := range []string{"one", "two", "three"} {
		st.InsertMessage("alice", content, "", "room-1", "", "", "")
	}

	history := h.history("public", "room-1", "")
	if len(history) != 2 || history[0].Content != "two" || history[1].Content != "three" {
		t.Fatalf("history = %+v, want the last two messages", history)
	}
}

// A sliding window would re-price the whole transcript on every new message.
// The window must stay append-only until it overflows, then re-anchor once.
func TestHistoryKeepsPrefixAppendOnlyUntilCompaction(t *testing.T) {
	h, st := newHistoryHub(t, 2)
	st.InsertMessage("alice", "one", "", "room-1", "", "", "")
	st.InsertMessage("bob", "two", "", "room-1", "", "", "")

	first := h.history("public", "room-1", "")
	if len(first) != 2 {
		t.Fatalf("first history = %+v, want two messages", first)
	}

	st.InsertMessage("carol", "three", "", "room-1", "", "", "")
	second := h.history("public", "room-1", "")
	if len(second) != 3 {
		t.Fatalf("history should grow by appending, got %+v", second)
	}
	for i := range first {
		if first[i].Content != second[i].Content {
			t.Fatalf("prefix changed between turns: %q -> %q", first[i].Content, second[i].Content)
		}
	}

	st.InsertMessage("dave", "four", "", "room-1", "", "", "")
	st.InsertMessage("erin", "five", "", "room-1", "", "", "")
	compacted := h.history("public", "room-1", "")
	if len(compacted) != 2 || compacted[0].Content != "four" || compacted[1].Content != "five" {
		t.Fatalf("compacted history = %+v, want the last two messages", compacted)
	}

	st.InsertMessage("frank", "six", "", "room-1", "", "", "")
	if again := h.history("public", "room-1", ""); len(again) != 3 || again[0].Content != "four" {
		t.Fatalf("history should append after compaction, got %+v", again)
	}
}
