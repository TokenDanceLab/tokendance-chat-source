package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/openai/openai-go/shared"
	"tokendancechat/backend/llm"
	"tokendancechat/backend/store"
)

// toolError renders a uniform model-facing failure: a stable snake_case code
// plus one short sentence saying what is wrong and how to recover. Tool
// results go back to the provider, so internal store/provider details are
// masked here and stay in server logs.
func toolError(code, msg string) error {
	return fmt.Errorf("%s: %s", code, msg)
}

// botTools returns the model tools allowed for the conversation. Each mode
// gets search over exactly the transcript the user can see: the room, or
// their own thread with the bot.
func (h *Hub) botTools(mode, roomID, username string) []llm.Tool {
	tools := []llm.Tool{currentTimeTool{}}
	if mode == "public" && roomID != "" {
		tools = append(tools, &searchMessagesTool{store: h.store, roomID: roomID})
	}
	if mode == "private" && username != "" {
		tools = append(tools, &searchPrivateMessagesTool{store: h.store, user: username, bot: h.botName})
	}
	return tools
}

type currentTimeTool struct{}

func (currentTimeTool) Name() string { return "get_current_time" }

func (currentTimeTool) Description() string {
	return "Get the current date and time as JSON with timezone, iso, date, and weekday fields. Use when the user asks about the current time or date, or when a relative date such as \"tomorrow\" needs an absolute anchor. Do not use it for historical or static facts you already know."
}

func (currentTimeTool) Parameters() shared.FunctionParameters {
	return shared.FunctionParameters{
		"type": "object",
		"properties": map[string]any{
			"timezone": map[string]any{
				"type":        "string",
				"description": "IANA timezone name, for example Asia/Hong_Kong. Defaults to Asia/Hong_Kong.",
			},
		},
		"additionalProperties": false,
	}
}

func (currentTimeTool) Execute(_ context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Timezone string `json:"timezone"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &args)
	}
	timezone := strings.TrimSpace(args.Timezone)
	if timezone == "" {
		timezone = "Asia/Hong_Kong"
	}
	if len(timezone) > 64 {
		return "", toolError("invalid_timezone", "timezone name is too long; use an IANA name such as Asia/Hong_Kong")
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return "", toolError("invalid_timezone", fmt.Sprintf("unknown timezone %q; use an IANA name such as Asia/Hong_Kong", timezone))
	}
	now := time.Now().In(location)
	payload := map[string]any{
		"timezone": timezone,
		"iso":      now.Format(time.RFC3339),
		"date":     now.Format("2006-01-02"),
		"weekday":  now.Weekday().String(),
	}
	data, _ := json.Marshal(payload)
	return string(data), nil
}

type searchMessagesTool struct {
	store  Store
	roomID string
}

func (*searchMessagesTool) Name() string { return "search_public_messages" }

func (*searchMessagesTool) Description() string {
	return "Keyword search over older public messages in this room. Use when the answer depends on room history that is no longer in your visible context; do not use it for messages already visible above, and never for private conversations. Returns JSON with up to limit matching messages (username, content excerpt, send time), ranked by relevance."
}

func (*searchMessagesTool) Parameters() shared.FunctionParameters {
	return searchParameters()
}

func (t *searchMessagesTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	query, limit, err := parseSearchArgs(raw)
	if err != nil {
		return "", err
	}
	if t == nil || t.store == nil || t.roomID == "" {
		return "", toolError("search_unavailable", "public message search is not configured for this run; answer from the visible transcript")
	}
	results, err := t.store.SearchMessages(query, t.roomID, limit)
	if err != nil {
		return "", toolError("search_unavailable", "message search failed on the server; continue from the visible transcript")
	}
	return renderSearchHits(query, results), nil
}

type searchPrivateMessagesTool struct {
	store Store
	user  string
	bot   string
}

func (*searchPrivateMessagesTool) Name() string { return "search_private_messages" }

func (*searchPrivateMessagesTool) Description() string {
	return "Keyword search over older messages in this 1:1 conversation. Use when the answer depends on earlier discussion that is no longer in your visible context; do not use it for messages already visible above. Never returns other users' messages or public rooms. Returns JSON with up to limit matches (username, content excerpt, send time), ranked by relevance."
}

func (*searchPrivateMessagesTool) Parameters() shared.FunctionParameters {
	return searchParameters()
}

func (t *searchPrivateMessagesTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	query, limit, err := parseSearchArgs(raw)
	if err != nil {
		return "", err
	}
	if t == nil || t.store == nil || t.user == "" || t.bot == "" {
		return "", toolError("search_unavailable", "private message search is not configured for this run; answer from the visible transcript")
	}
	results, err := t.store.SearchPrivateMessages(t.user, t.bot, query, limit)
	if err != nil {
		return "", toolError("search_unavailable", "message search failed on the server; continue from the visible transcript")
	}
	return renderSearchHits(query, results), nil
}

func searchParameters() shared.FunctionParameters {
	return shared.FunctionParameters{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Keyword or phrase to search for, 1-100 characters.",
				"minLength":   1,
				"maxLength":   100,
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of messages to return, between 1 and 10. Defaults to 5.",
				"minimum":     1,
				"maximum":     10,
			},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	}
}

func parseSearchArgs(raw json.RawMessage) (string, int, error) {
	var args struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", 0, toolError("invalid_arguments", `arguments must be a JSON object like {"query":"keyword","limit":5}`)
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return "", 0, toolError("missing_query", "provide a non-empty query string")
	}
	if len([]rune(query)) > 100 {
		query = string([]rune(query)[:100])
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}
	return query, limit, nil
}

func renderSearchHits(query string, results []store.SearchResult) string {
	type hit struct {
		Username  string `json:"username"`
		Content   string `json:"content"`
		Timestamp int64  `json:"timestamp"`
	}
	hits := make([]hit, 0, len(results))
	for _, result := range results {
		content := []rune(result.Content)
		if len(content) > 240 {
			content = append(content[:240], '…')
		}
		hits = append(hits, hit{
			Username:  result.Username,
			Content:   string(content),
			Timestamp: result.Timestamp,
		})
	}
	data, _ := json.Marshal(map[string]any{
		"query":    query,
		"messages": hits,
	})
	return string(data)
}

// sendMessageTool lets the bot post a short interim message before its final
// answer. It deliberately writes no run_id: the store keeps one final message
// per run, while interim messages read as ordinary bot messages.
type sendMessageTool struct {
	hub *Hub
	run *botRun
}

func (*sendMessageTool) Name() string { return "send_message" }

func (*sendMessageTool) Description() string {
	return "Post one short interim message to the conversation before your final answer. Use it only for a distinct progress update on a multi-step task: at most once per run in a public room, twice in a private chat, 600 characters maximum. Never split a normal answer across messages, never repeat the final answer, and never use it for filler or questions. Everyone in a public room can see it."
}

func (*sendMessageTool) Parameters() shared.FunctionParameters {
	return shared.FunctionParameters{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{
				"type":        "string",
				"description": "Short user-facing progress update, plain text or Markdown. Maximum 600 characters.",
			},
		},
		"required":             []string{"content"},
		"additionalProperties": false,
	}
}

func (t *sendMessageTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", toolError("invalid_arguments", `arguments must be a JSON object like {"content":"..."}`)
	}
	content := sanitizeBotContent(strings.TrimSpace(args.Content))
	if content == "" {
		return "", toolError("missing_content", "provide non-empty content")
	}
	if runes := len([]rune(content)); runes > 600 {
		return "", toolError("content_too_long", fmt.Sprintf("content is %d characters; the maximum is 600, shorten it", runes))
	}
	t.hub.runMu.Lock()
	defer t.hub.runMu.Unlock()
	if ctx.Err() != nil || !t.hub.runPublishableLocked(t.run) {
		return "", toolError("run_inactive", "this run has been cancelled or is no longer publishable; stop calling tools and end your turn")
	}
	limit := 2
	if t.run.mode == "public" {
		limit = 1
	}
	if t.run.interim >= limit {
		return "", toolError("interim_limit", "the interim message limit for this run is reached; do not call send_message again and put everything else in your final answer")
	}
	m, err := t.hub.store.InsertMessage(t.hub.botName, content, t.run.record.TriggerMessageID, t.run.record.RoomID, t.run.record.ToUser, "", "")
	if err != nil {
		return "", toolError("send_failed", "the message could not be stored; continue to your final answer")
	}
	t.run.interim++
	t.hub.sendRunLocked(t.run, Message{
		Type:           "message",
		ID:             m.ID,
		MessageID:      m.ID,
		Username:       m.Username,
		Content:        m.Content,
		Timestamp:      m.Timestamp,
		ReplyToID:      m.ReplyToID,
		ReplyToUser:    t.run.record.RequestedBy,
		ReplyToContent: t.run.trigger.Content,
		RoomID:         m.RoomID,
		Private:        t.run.mode == "private",
		To:             t.run.record.ToUser,
	})
	return `{"ok":true}`, nil
}
