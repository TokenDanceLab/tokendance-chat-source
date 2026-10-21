package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/openai/openai-go/shared"
	"tokendancechat/backend/llm"
	"tokendancechat/backend/store"
)

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
	return "Get the current date and time. Use when the user asks for the time or date, or when a relative date needs an absolute anchor."
}

func (currentTimeTool) Parameters() shared.FunctionParameters {
	return shared.FunctionParameters{
		"type": "object",
		"properties": map[string]any{
			"timezone": map[string]any{
				"type":        "string",
				"description": "IANA timezone, for example Asia/Hong_Kong. Defaults to Asia/Hong_Kong.",
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
		return "", errors.New("timezone is too long")
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return "", fmt.Errorf("unknown timezone %q", timezone)
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
	return "Search public room messages by keyword. Use when the user asks what was said earlier or wants a summary of public discussion. Never use it for private conversations."
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
		return "", errors.New("public message search is unavailable")
	}
	results, err := t.store.SearchMessages(query, t.roomID, limit)
	if err != nil {
		return "", err
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
	return "Search this 1:1 conversation with the user by keyword. Use when the user refers to something discussed earlier. Never returns other users' messages or public rooms."
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
		return "", errors.New("private message search is unavailable")
	}
	results, err := t.store.SearchPrivateMessages(t.user, t.bot, query, limit)
	if err != nil {
		return "", err
	}
	return renderSearchHits(query, results), nil
}

func searchParameters() shared.FunctionParameters {
	return shared.FunctionParameters{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Keyword or phrase to search for.",
				"minLength":   1,
				"maxLength":   100,
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of messages to return, between 1 and 10.",
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
		return "", 0, errors.New("invalid search arguments")
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return "", 0, errors.New("query is required")
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
