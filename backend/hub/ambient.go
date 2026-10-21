package hub

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"
	"time"

	"tokendancechat/backend/llm"
)

// Ambient participation lets TokenBot decide, without an @mention, whether a
// public message deserves a reply. Explicit mentions always win; this router is
// intentionally small, rate-limited and disabled by default.
type ambientPolicy struct {
	enabled  bool
	cooldown time.Duration
	perHour  int
	timeout  time.Duration
}

func envFlag(name string, fallback bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func newAmbientPolicy() ambientPolicy {
	cooldown := envInt("CHAT_BOT_AMBIENT_COOLDOWN_SEC", 90)
	return ambientPolicy{
		enabled:  envFlag("CHAT_BOT_AMBIENT", false),
		cooldown: time.Duration(cooldown) * time.Second,
		perHour:  envInt("CHAT_BOT_AMBIENT_PER_HOUR", 6),
		timeout:  10 * time.Second,
	}
}

const ambientSystemPrompt = `You are the participation router for TokenBot, an AI member of a public chat room.
Decide whether TokenBot should reply to the LATEST message. Reply only when TokenBot clearly adds value:
- someone asks the room a question or requests help;
- a technical, product or factual discussion where TokenBot has useful knowledge;
- someone is talking to TokenBot indirectly.
Do NOT reply to greetings, thanks, reactions, jokes, casual chatter between people, messages addressed to a specific
other person, or messages the room can answer itself. Be conservative: most messages should be ignored.

Return ONLY strict JSON: {"action":"reply"|"ignore","reason":"brief reason"}`

// maybeAmbientReply is called after a public message is stored. It never blocks
// the sending client: the decision runs in the background.
func (h *Hub) maybeAmbientReply(c *Client, trigger StoredMessage) {
	if !h.ambient.enabled || h.llmClient == nil || trigger.Username == h.botName {
		return
	}
	if len([]rune(strings.TrimSpace(trigger.Content))) < 4 {
		return
	}
	if !h.reserveAmbientSlot(trigger.RoomID) {
		return
	}
	go h.evaluateAmbient(c, trigger)
}

// reserveAmbientSlot enforces cooldown, hourly pacing, one decision in flight
// and the "one owner per room" rule before any model tokens are spent.
func (h *Hub) reserveAmbientSlot(roomID string) bool {
	h.ambientMu.Lock()
	defer h.ambientMu.Unlock()
	if h.ambientInFlight || roomID == "" {
		return false
	}
	now := time.Now()
	if !h.ambientLast.IsZero() && now.Sub(h.ambientLast) < h.ambient.cooldown {
		return false
	}
	cutoff := now.Add(-time.Hour)
	kept := h.ambientTimes[:0]
	for _, at := range h.ambientTimes {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	h.ambientTimes = kept
	if len(h.ambientTimes) >= h.ambient.perHour {
		return false
	}
	h.runMu.Lock()
	defer h.runMu.Unlock()
	for _, run := range h.runs {
		if run.mode == "public" && run.record.RoomID == roomID &&
			(run.record.Status == "queued" || run.record.Status == "running") {
			return false
		}
	}
	h.ambientInFlight = true
	h.ambientLast = now
	return true
}

func (h *Hub) evaluateAmbient(c *Client, trigger StoredMessage) {
	defer func() {
		h.ambientMu.Lock()
		h.ambientInFlight = false
		h.ambientMu.Unlock()
	}()
	if c == nil || !c.authenticatedSessionIsCurrent() {
		return
	}

	messages := h.store.GetMessages(12, 0)
	lines := make([]string, 0, len(messages)+1)
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.RoomID != trigger.RoomID || message.Deleted || message.Content == "" {
			continue
		}
		content := []rune(strings.TrimSpace(message.Content))
		if len(content) > 280 {
			content = append(content[:280], '…')
		}
		lines = append(lines, message.Username+": "+string(content))
	}
	lines = append(lines, "LATEST (by "+trigger.Username+"): "+trigger.Content)

	ctx, cancel := context.WithTimeout(context.Background(), h.ambient.timeout)
	defer cancel()
	answer, err := h.llmClient.Chat(ctx, ambientSystemPrompt, []llm.Message{
		{Role: "user", Content: strings.Join(lines, "\n")},
	})
	if err != nil {
		h.ambientMu.Lock()
		h.ambientLast = time.Time{}
		h.ambientMu.Unlock()
		log.Printf("ambient selector failed: %v", err)
		return
	}
	action, reason := parseAmbientDecision(answer)
	log.Printf("ambient selector: room=%s author=%s action=%s reason=%s", trigger.RoomID, trigger.Username, action, clipLog(reason, 120))
	if action != "reply" {
		return
	}
	if !c.authenticatedSessionIsCurrent() {
		return
	}
	h.ambientMu.Lock()
	h.ambientTimes = append(h.ambientTimes, time.Now())
	h.ambientMu.Unlock()
	h.enqueueRun(c, trigger, false)
}

// parseAmbientDecision tolerates code fences and surrounding prose from smaller
// models while still requiring an explicit action.
func parseAmbientDecision(raw string) (action, reason string) {
	text := strings.TrimSpace(raw)
	if start := strings.Index(text, "{"); start >= 0 {
		if end := strings.LastIndex(text, "}"); end > start {
			text = text[start : end+1]
		}
	}
	var decision struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(text), &decision); err != nil {
		return "ignore", "unparseable"
	}
	action = strings.ToLower(strings.TrimSpace(decision.Action))
	if action != "reply" {
		action = "ignore"
	}
	reason = strings.TrimSpace(decision.Reason)
	if reason == "" {
		reason = "-"
	}
	return action, reason
}

// AmbientEnabled reports whether the router is active, for /api/config.
func (h *Hub) AmbientEnabled() bool { return h.ambient.enabled && h.llmClient != nil }
