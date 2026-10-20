package hub

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tokendancechat/backend/llm"
)

// A guest can pick a fresh name at any time, so this proves the quota is
// enforced on the real trigger path, not just in the budget unit.
func TestBotDailyQuotaBlocksGuestSpam(t *testing.T) {
	t.Setenv("CHAT_BOT_DAILY_PER_USER", "2")
	t.Setenv("CHAT_BOT_DAILY_PER_IP", "0")
	t.Setenv("CHAT_BOT_DAILY_REPLIES", "0")
	t.Setenv("CHAT_BOT_DAILY_TOKENS", "0")
	t.Setenv("CHAT_BOT_MAX_CONCURRENT", "0")

	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer stub.Close()

	env := newE2EEnvWithLLM(&llm.Config{
		Provider: "openai",
		APIKey:   "test",
		Model:    "stub",
		BaseURL:  stub.URL,
		Timeout:  5 * time.Second,
	})
	defer env.Close()

	conn := env.Dial(t)
	defer conn.Close()
	conn.Send(Message{Type: "join", Username: "quota_guest"})
	conn.DrainJoin()

	for i := 0; i < 2; i++ {
		conn.Send(Message{Type: "message", Content: "@TokenBot ping"})
		waitForMessage(t, conn, 10*time.Second, func(m Message) bool {
			return m.Type == "message" && m.Username == "TokenBot"
		})
		time.Sleep(botCooldownWindow + 100*time.Millisecond)
	}

	conn.Send(Message{Type: "message", Content: "@TokenBot ping"})
	denied := waitForMessage(t, conn, 5*time.Second, func(m Message) bool {
		return m.Type == "system"
	})
	if !strings.Contains(denied.Content, "次数已用完") {
		t.Fatalf("expected quota denial, got %q", denied.Content)
	}
}

func waitForMessage(t *testing.T, conn *wsConn, timeout time.Duration, match func(Message) bool) Message {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatal("timed out waiting for message")
		}
		msg := conn.Recv(remaining)
		if match(msg) {
			return msg
		}
	}
}
