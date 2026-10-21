// Package llm provides the TokenBot model client on top of the official
// OpenAI Go SDK. It targets OpenAI-compatible chat-completions endpoints
// (OpenAI, DeepSeek, gateways) and exposes text, reasoning and tool-call
// events for the assistant-ui message parts.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
	"tokendancechat/backend/websearch"
)

const (
	DefaultMaxTokens       = 4096
	DefaultTimeout         = 90 * time.Second
	DefaultMaxToolRounds   = 3
	DefaultContextMessages = 20
	maxToolResultRunes     = 8000
	// MaxRoundGrowthBytes bounds new assistant/tool input carried to the next request.
	MaxRoundGrowthBytes    = 131072
	maxAssistantRoundBytes = 65536
	maxToolArgumentsBytes  = 8192
	// Providers may ignore parallel_tool_calls=false. Execute a bounded number
	// of calls sequentially instead of failing an otherwise valid reply.
	maxToolCallsPerRound = 4
)

// Config holds model connection and generation settings.
type Config struct {
	WebSearch       *websearch.Config
	Provider        string
	APIKey          string
	Model           string
	BaseURL         string
	MaxTokens       int
	Temperature     float64
	TopP            float64
	ContextMessages int
	Timeout         time.Duration
	MaxToolRounds   int
	ReasoningEffort string
}

// Message is one item of conversation history.
type Message struct {
	Role     string `json:"role"`
	Content  string `json:"content"`
	Username string `json:"username,omitempty"`
	// Timestamp is when a chat message was sent (Unix ms). It is rendered in
	// the model input so history stays self-describing while the prompt prefix
	// itself stays static.
	Timestamp int64 `json:"timestamp,omitempty"`
}

// Part is the transport shape shared by the WebSocket protocol and the
// assistant-ui runtime. It intentionally mirrors assistant-ui's message parts.
type Part struct {
	SourceID   string `json:"source_id,omitempty"`
	Title      string `json:"title,omitempty"`
	URL        string `json:"url,omitempty"`
	Cited      bool   `json:"cited,omitempty"`
	Type       string `json:"type"`
	Text       string `json:"text,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	Arguments  string `json:"arguments,omitempty"`
	Result     string `json:"result,omitempty"`
	Status     string `json:"status,omitempty"`
}

// ToolCall is a model-requested function call and its local execution state.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
	Result    string `json:"result,omitempty"`
	Status    string `json:"status,omitempty"`
}

// Event is one stream update. Text events carry deltas; tool events carry the
// complete call so the UI can render a stable card.
type Event struct {
	Kind     string    `json:"kind"`
	Text     string    `json:"text,omitempty"`
	ToolCall *ToolCall `json:"tool_call,omitempty"`
	Parts    []Part    `json:"parts,omitempty"`
}

// EventHandler receives stream events in order.
type EventHandler func(Event) error

// Tool is a bounded function the model may call.
type Tool interface {
	Name() string
	Description() string
	Parameters() shared.FunctionParameters
	Execute(ctx context.Context, arguments json.RawMessage) (string, error)
}

// Result is the complete assistant turn, including reasoning and tools.
type Result struct {
	Text       string
	UsageKnown bool
	Parts      []Part
	ToolCalls  []ToolCall
	Usage      Usage
}

// Usage reports token accounting. CachedTokens is whatever the provider
// reports (OpenAI prompt_tokens_details.cached_tokens, DeepSeek
// prompt_cache_hit_tokens); it is the basis of the cache hit rate metric.
type Usage struct {
	PromptTokens     int64
	CachedTokens     int64
	CompletionTokens int64
	ReasoningTokens  int64
}

// Client wraps the official OpenAI Go SDK.
type Client struct {
	cfg Config
	sdk openai.Client
}

// New creates a client for an OpenAI-compatible endpoint.
func New(cfg Config) *Client {
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = DefaultMaxTokens
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.ContextMessages <= 0 {
		cfg.ContextMessages = DefaultContextMessages
	}
	if cfg.MaxToolRounds <= 0 {
		cfg.MaxToolRounds = DefaultMaxToolRounds
	}
	if cfg.Provider == "" {
		cfg.Provider = "openai"
	}

	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		option.WithMaxRetries(0),
		option.WithRequestTimeout(cfg.Timeout),
	}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		opts = append(opts, option.WithBaseURL(strings.TrimRight(cfg.BaseURL, "/")))
	}

	return &Client{cfg: cfg, sdk: openai.NewClient(opts...)}
}

// Model returns the configured model id.
func (c *Client) Model() string { return c.cfg.Model }

// Provider returns the configured provider id.
func (c *Client) Provider() string { return c.cfg.Provider }

// MaxToolRounds returns the configured tool loop limit.
func (c *Client) MaxToolRounds() int { return c.cfg.MaxToolRounds }

// MaxTokens returns the configured output token cap.
func (c *Client) MaxTokens() int { return c.cfg.MaxTokens }

// Chat returns a non-streaming text response. It is kept for focused tests;
// the product path uses ChatStream.
func (c *Client) Chat(ctx context.Context, systemPrompt string, messages []Message) (string, error) {
	params := c.newParams(systemPrompt, messages, nil)
	resp, err := c.sdk.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("openai chat: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("openai: no choices in response")
	}
	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	if content == "" {
		return "", errors.New("openai: empty message content")
	}
	return content, nil
}

// ChatStream runs the model, executes tool calls, and emits assistant-ui parts.
func (c *Client) ChatStream(
	ctx context.Context,
	systemPrompt string,
	messages []Message,
	tools []Tool,
	onEvent EventHandler,
) (Result, error) {
	params := c.newParams(systemPrompt, messages, tools)
	result := Result{UsageKnown: true}

	for round := 0; ; round++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		turn, err := c.streamTurn(ctx, &params, &result, onEvent)
		if err != nil {
			return result, err
		}
		if len(turn.ToolCalls) == 0 {
			if strings.TrimSpace(result.Text) == "" {
				return result, errors.New("openai: empty message content")
			}
			if err := emit(onEvent, Event{Kind: "done", Parts: result.Parts}); err != nil {
				return result, err
			}
			return result, nil
		}
		if len(turn.ToolCalls) > maxToolCallsPerRound {
			return result, fmt.Errorf("too many tool calls in one round (%d > %d)", len(turn.ToolCalls), maxToolCallsPerRound)
		}
		if round >= c.cfg.MaxToolRounds {
			return result, fmt.Errorf("tool loop limit reached (%d)", c.cfg.MaxToolRounds)
		}

		assistant := openai.AssistantMessage(turn.Text)
		assistant.OfAssistant.ToolCalls = make([]openai.ChatCompletionMessageToolCallParam, 0, len(turn.ToolCalls))
		for _, call := range turn.ToolCalls {
			assistant.OfAssistant.ToolCalls = append(assistant.OfAssistant.ToolCalls, openai.ChatCompletionMessageToolCallParam{
				ID: call.ID,
				Function: openai.ChatCompletionMessageToolCallFunctionParam{
					Name:      call.Name,
					Arguments: call.Arguments,
				},
			})
		}
		params.Messages = append(params.Messages, assistant)

		for i := range turn.ToolCalls {
			call := turn.ToolCalls[i]
			call.Status = "running"
			result.ToolCalls = append(result.ToolCalls, call)
			result.Parts = append(result.Parts, Part{
				Type:       "tool-call",
				ToolCallID: call.ID,
				ToolName:   call.Name,
				Arguments:  call.Arguments,
				Status:     call.Status,
			})
			if err := emit(onEvent, Event{Kind: "tool_call", ToolCall: &call}); err != nil {
				return result, err
			}

			if err := ctx.Err(); err != nil {
				return result, err
			}
			output, execErr := executeTool(ctx, tools, call)
			if err := ctx.Err(); err != nil {
				return result, err
			}
			if execErr != nil {
				call.Status = "error"
				call.Result = "Error: " + execErr.Error()
			} else {
				call.Status = "complete"
				call.Result = output
			}
			result.ToolCalls[len(result.ToolCalls)-1] = call
			updateToolPart(result.Parts, call)
			if err := emit(onEvent, Event{Kind: "tool_result", ToolCall: &call}); err != nil {
				return result, err
			}
			params.Messages = append(params.Messages, openai.ToolMessage(call.Result, call.ID))
		}
	}
}

type turnResult struct {
	Text      string
	ToolCalls []ToolCall
}

func (c *Client) streamTurn(
	ctx context.Context,
	params *openai.ChatCompletionNewParams,
	result *Result,
	onEvent EventHandler,
) (turnResult, error) {
	stream := c.sdk.Chat.Completions.NewStreaming(ctx, *params)
	defer stream.Close()

	var turn turnResult
	toolCalls := make(map[int64]*ToolCall)
	var roundUsage Usage
	roundBytes := 0
	roundUsageKnown := false
	roundSucceeded := false
	defer func() {
		if !roundUsageKnown || !roundSucceeded {
			result.UsageKnown = false
		}
	}()

	for stream.Next() {
		if err := ctx.Err(); err != nil {
			return turn, err
		}
		chunk := stream.Current()
		if present, valid := captureUsage(&roundUsage, chunk); present {
			roundUsageKnown = valid
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		roundBytes += len(delta.Content) + len(reasoningDelta(chunk))
		if roundBytes > maxAssistantRoundBytes {
			return turn, errors.New("assistant output limit exceeded")
		}

		if delta.Content != "" {
			turn.Text += delta.Content
			result.Text += delta.Content
			appendPart(&result.Parts, "text", delta.Content)
			if err := emit(onEvent, Event{Kind: "text", Text: delta.Content}); err != nil {
				return turn, err
			}
		}
		if reasoning := reasoningDelta(chunk); reasoning != "" {
			appendPart(&result.Parts, "reasoning", reasoning)
			if err := emit(onEvent, Event{Kind: "reasoning", Text: reasoning}); err != nil {
				return turn, err
			}
		}

		for _, call := range delta.ToolCalls {
			roundBytes += len(call.ID) + len(call.Function.Name) + len(call.Function.Arguments)
			if roundBytes > maxAssistantRoundBytes {
				return turn, errors.New("assistant output limit exceeded")
			}
			if call.Index < 0 {
				return turn, errors.New("invalid tool call index")
			}
			cur := toolCalls[call.Index]
			if cur == nil {
				cur = &ToolCall{}
				toolCalls[call.Index] = cur
			}
			if call.ID != "" {
				cur.ID = call.ID
				if len(cur.ID) > 256 {
					return turn, errors.New("tool ID limit exceeded")
				}
			}
			if call.Function.Name != "" {
				cur.Name += call.Function.Name
				if len(cur.Name) > 128 {
					return turn, errors.New("tool name limit exceeded")
				}
			}
			if call.Function.Arguments != "" {
				cur.Arguments += call.Function.Arguments
				if len(cur.Arguments) > maxToolArgumentsBytes {
					return turn, errors.New("tool arguments limit exceeded")
				}
			}
		}
	}
	// Usage is reported per request. A tool loop makes several requests in one
	// assistant turn, so add each round instead of keeping only the last one.
	result.Usage.PromptTokens += roundUsage.PromptTokens
	result.Usage.CachedTokens += roundUsage.CachedTokens
	result.Usage.CompletionTokens += roundUsage.CompletionTokens
	result.Usage.ReasoningTokens += roundUsage.ReasoningTokens
	if err := stream.Err(); err != nil {
		return turn, fmt.Errorf("openai stream: %w", err)
	}

	roundSucceeded = true
	indexes := make([]int64, 0, len(toolCalls))
	for index := range toolCalls {
		indexes = append(indexes, index)
	}
	sort.Slice(indexes, func(i, j int) bool { return indexes[i] < indexes[j] })
	for _, index := range indexes {
		call := *toolCalls[index]
		if call.ID == "" {
			call.ID = fmt.Sprintf("call_%d", index)
		}
		if call.Name == "" {
			continue
		}
		turn.ToolCalls = append(turn.ToolCalls, call)
	}
	return turn, nil
}

func (c *Client) newParams(systemPrompt string, messages []Message, tools []Tool) openai.ChatCompletionNewParams {
	apiMessages := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages)+1)
	apiMessages = append(apiMessages, openai.SystemMessage(systemPrompt))
	for _, message := range messages {
		if message.Role == "assistant" {
			apiMessages = append(apiMessages, openai.AssistantMessage(message.Content))
			continue
		}
		apiMessages = append(apiMessages, openai.UserMessage(speakerLine(message)))
	}

	params := openai.ChatCompletionNewParams{
		Model:     openai.ChatModel(c.cfg.Model),
		Messages:  apiMessages,
		MaxTokens: openai.Int(int64(c.cfg.MaxTokens)),
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		},
	}
	if c.cfg.Temperature > 0 {
		params.Temperature = openai.Float(c.cfg.Temperature)
	}
	if c.cfg.TopP > 0 {
		params.TopP = openai.Float(c.cfg.TopP)
	}
	if c.cfg.ReasoningEffort != "" {
		params.ReasoningEffort = shared.ReasoningEffort(c.cfg.ReasoningEffort)
	}
	if len(tools) > 0 {
		// A hint only: some OpenAI-compatible providers emit parallel calls
		// regardless, and the loop executes them sequentially with a bound.
		params.ParallelToolCalls = openai.Bool(false)
		params.Tools = make([]openai.ChatCompletionToolParam, 0, len(tools))
		for _, tool := range tools {
			params.Tools = append(params.Tools, openai.ChatCompletionToolParam{
				Function: shared.FunctionDefinitionParam{
					Name:        tool.Name(),
					Description: openai.String(tool.Description()),
					Parameters:  tool.Parameters(),
				},
			})
		}
	}
	return params
}

// speakerLine labels a chat message with its author and send time. Only the
// new tail of the transcript changes between calls, so the stored prefix —
// and with it the provider's prompt cache — stays stable.
func speakerLine(message Message) string {
	if message.Username == "" {
		return message.Content
	}
	header := "@" + message.Username
	if message.Timestamp > 0 {
		sent := time.UnixMilli(message.Timestamp).In(time.FixedZone("HKT", 8*60*60))
		header += " · " + sent.Format("2006-01-02 15:04")
	}
	return header + "\n" + message.Content
}

// captureUsage distinguishes absent/partial JSON usage from explicit zeroes.
// SDK numeric defaults alone cannot prove that a provider reported both costs.
// present=false ignores ordinary stream chunks whose usage is absent/null.
func captureUsage(total *Usage, chunk openai.ChatCompletionChunk) (present, valid bool) {
	var envelope struct {
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(chunk.RawJSON()), &envelope); err != nil {
		return true, false
	}
	if len(envelope.Usage) == 0 || string(envelope.Usage) == "null" {
		return false, false
	}
	var reported struct {
		PromptTokens         *int64 `json:"prompt_tokens"`
		CompletionTokens     *int64 `json:"completion_tokens"`
		PromptCacheHitTokens int64  `json:"prompt_cache_hit_tokens"`
	}
	if err := json.Unmarshal(envelope.Usage, &reported); err != nil {
		return true, false
	}
	if reported.PromptTokens == nil || reported.CompletionTokens == nil || *reported.PromptTokens < 0 || *reported.CompletionTokens < 0 || *reported.PromptTokens > math.MaxInt64-*reported.CompletionTokens {
		return true, false
	}
	total.PromptTokens = *reported.PromptTokens
	total.CompletionTokens = *reported.CompletionTokens
	total.ReasoningTokens = max(0, chunk.Usage.CompletionTokensDetails.ReasoningTokens)
	total.CachedTokens = max(0, chunk.Usage.PromptTokensDetails.CachedTokens)
	if total.CachedTokens == 0 {
		total.CachedTokens = max(0, reported.PromptCacheHitTokens)
	}
	return true, true
}

type toolCallContextKey struct{}

// ToolCallID is supplied by the trusted model-loop dispatcher, not tool JSON.
func ToolCallID(ctx context.Context) string {
	id, _ := ctx.Value(toolCallContextKey{}).(string)
	return id
}

func executeTool(ctx context.Context, tools []Tool, call ToolCall) (string, error) {
	ctx = context.WithValue(ctx, toolCallContextKey{}, call.ID)
	for _, tool := range tools {
		if tool.Name() != call.Name {
			continue
		}
		arguments := json.RawMessage(call.Arguments)
		if len(arguments) == 0 || !json.Valid(arguments) {
			arguments = json.RawMessage(`{}`)
		}
		output, err := tool.Execute(ctx, arguments)
		if err != nil {
			return "", err
		}
		runes := []rune(output)
		if len(runes) > maxToolResultRunes {
			output = string(runes[:maxToolResultRunes]) + "\n…"
		}
		return output, nil
	}
	return "", fmt.Errorf("unknown tool %q", call.Name)
}

// reasoningDelta reads the vendor-specific reasoning field. The SDK models
// only OpenAI fields, so DeepSeek/Gateway extensions are read from the raw
// chunk payload instead of ExtraFields (which this struct does not capture).
func reasoningDelta(chunk openai.ChatCompletionChunk) string {
	var payload struct {
		Choices []struct {
			Delta struct {
				ReasoningContent string `json:"reasoning_content"`
				Reasoning        string `json:"reasoning"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(chunk.RawJSON()), &payload); err != nil || len(payload.Choices) == 0 {
		return ""
	}
	delta := payload.Choices[0].Delta
	if delta.ReasoningContent != "" {
		return delta.ReasoningContent
	}
	return delta.Reasoning
}

func appendPart(parts *[]Part, kind, text string) {
	if text == "" {
		return
	}
	if len(*parts) > 0 {
		last := &(*parts)[len(*parts)-1]
		if last.Type == kind {
			last.Text += text
			return
		}
	}
	*parts = append(*parts, Part{Type: kind, Text: text})
}

func updateToolPart(parts []Part, call ToolCall) {
	for i := range parts {
		if parts[i].Type == "tool-call" && parts[i].ToolCallID == call.ID {
			parts[i].Result = call.Result
			parts[i].Status = call.Status
			return
		}
	}
}

func emit(handler EventHandler, event Event) error {
	if handler == nil {
		return nil
	}
	return handler(event)
}
