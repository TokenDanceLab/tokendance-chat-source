package hub

import "tokendancechat/backend/llm"

// historyLookbackFactor is how far behind the current window the store is
// read. The extra room lets the window grow by appending instead of shifting.
const historyLookbackFactor = 2

// history returns the model input for one conversation, oldest first.
//
// The database is the only memory. A parallel in-process buffer can drift
// from what users see, cannot survive a restart, and can leak one room or one
// private thread into another.
func (h *Hub) history(mode, roomID, username string) []llm.Message {
	window := h.contextMessages()
	lookback := window * historyLookbackFactor

	var stored []StoredMessage
	if mode == "private" {
		stored = h.store.GetMessagesBetween(username, h.botName, lookback)
	} else {
		stored = h.store.GetRoomMessages(roomID, lookback, 0)
	}
	stored = h.windowFromAnchor(conversationKey(mode, roomID, username), stored, window)

	messages := make([]llm.Message, 0, len(stored))
	for _, message := range stored {
		if message.Deleted || message.Username == "system" || message.Content == "" {
			continue
		}
		role := "user"
		if message.Username == h.botName {
			role = "assistant"
		}
		messages = append(messages, llm.Message{
			Role:      role,
			Content:   message.Content,
			Username:  message.Username,
			Timestamp: message.Timestamp,
		})
	}
	return messages
}

// windowFromAnchor keeps the model input append-only between compactions.
// Providers match cached prefixes from the start of the request, so a window
// that slides on every message re-prices the whole transcript each turn. The
// anchor is the oldest row already in the request; it only moves when it
// falls out of the lookback, and that compaction costs one cache miss.
func (h *Hub) windowFromAnchor(key string, stored []StoredMessage, window int) []StoredMessage {
	start := -1
	h.historyAnchorMu.Lock()
	if anchor := h.historyAnchor[key]; anchor != "" {
		for i := range stored {
			if stored[i].ID == anchor {
				start = i
				break
			}
		}
	}
	if start < 0 {
		start = len(stored) - window
		if start < 0 {
			start = 0
		}
		if start < len(stored) {
			h.historyAnchor[key] = stored[start].ID
		}
	}
	h.historyAnchorMu.Unlock()
	return stored[start:]
}

// conversationKey is the memory boundary. Public rooms share one key; every
// user's 1:1 thread with the bot is its own.
func conversationKey(mode, roomID, username string) string {
	if mode == "private" {
		return "dm:" + username
	}
	return "room:" + roomID
}

func (h *Hub) contextMessages() int {
	if h.llmCfg != nil && h.llmCfg.ContextMessages > 0 {
		return h.llmCfg.ContextMessages
	}
	return llm.DefaultContextMessages
}
