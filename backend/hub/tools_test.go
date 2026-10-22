package hub

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"tokendancechat/backend/llm"
	"tokendancechat/backend/store"
)

func TestSearchToolReturnsOnlyPublicMessages(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer st.Close()

	if _, err := st.InsertMessage("alice", "needle in the public room", "", "room-1", "", "", ""); err != nil {
		t.Fatalf("insert public: %v", err)
	}
	if _, err := st.InsertMessage("alice", "needle in a private dm", "", "", "bob", "", ""); err != nil {
		t.Fatalf("insert private: %v", err)
	}

	tool := &searchMessagesTool{store: st, roomID: "room-1"}
	output, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"needle","limit":5}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(output, "public room") {
		t.Errorf("expected the public hit in %s", output)
	}
	if strings.Contains(output, "private dm") {
		t.Errorf("private message leaked into tool output: %s", output)
	}
}

func TestPrivateSearchToolReturnsOnlyOwnThread(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer st.Close()

	if _, err := st.InsertMessage("alice", "needle from alice", "", "", "TokenBot", "", ""); err != nil {
		t.Fatalf("insert alice: %v", err)
	}
	if _, err := st.InsertMessage("TokenBot", "needle reply to alice", "", "", "alice", "", ""); err != nil {
		t.Fatalf("insert bot reply: %v", err)
	}
	if _, err := st.InsertMessage("bob", "needle from bob", "", "", "TokenBot", "", ""); err != nil {
		t.Fatalf("insert bob: %v", err)
	}
	if _, err := st.InsertMessage("alice", "needle in the public room", "", "room-1", "", "", ""); err != nil {
		t.Fatalf("insert public: %v", err)
	}

	tool := &searchPrivateMessagesTool{store: st, user: "alice", bot: "TokenBot"}
	output, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"needle","limit":10}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(output, "needle from alice") || !strings.Contains(output, "needle reply to alice") {
		t.Errorf("expected alice's thread in %s", output)
	}
	for _, leak := range []string{"needle from bob", "public room"} {
		if strings.Contains(output, leak) {
			t.Errorf("%q leaked into the private search: %s", leak, output)
		}
	}
}

func TestCurrentTimeTool(t *testing.T) {
	tool := currentTimeTool{}
	output, err := tool.Execute(context.Background(), json.RawMessage(`{"timezone":"UTC"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var payload struct {
		Timezone string `json:"timezone"`
		ISO      string `json:"iso"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Timezone != "UTC" || !strings.HasSuffix(payload.ISO, "Z") {
		t.Errorf("unexpected payload: %s", output)
	}

	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"timezone":"Mars/Base"}`)); err == nil {
		t.Error("expected an error for an unknown timezone")
	}
}

func TestPrivateChatDoesNotGetPublicSearch(t *testing.T) {
	ms := &mockStore{}
	h := New(ms, nil, "TokenBot")

	private := h.botTools("private", "", "alice")
	if got := strings.Join(toolNames(private), ","); got != "get_current_time,search_private_messages" {
		t.Fatalf("private tools = %s", got)
	}
	if anonymous := h.botTools("private", "", ""); len(anonymous) != 1 {
		t.Fatalf("anonymous private tools = %v, want only get_current_time", toolNames(anonymous))
	}
	public := h.botTools("public", "room-1", "")
	if got := strings.Join(toolNames(public), ","); got != "get_current_time,search_public_messages" {
		t.Fatalf("public tools = %s", got)
	}
}

func toolNames(tools []llm.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name())
	}
	return names
}
