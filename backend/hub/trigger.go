package hub

import (
	"strings"
	"time"
)

const replyTargetLimit = 200

// replyTargetContext renders the parent of a reply as one bounded line. It is
// omitted when the parent is missing, deleted, or outside the same
// conversation, so a stale reference can never pull another chat into the
// prompt. The model needs it because the reply target may sit outside the
// visible window.
func (h *Hub) replyTargetContext(trigger StoredMessage, private bool) string {
	if strings.TrimSpace(trigger.ReplyToID) == "" {
		return ""
	}
	parent, err := h.store.GetMessageByID(trigger.ReplyToID)
	if err != nil || parent.Deleted || parent.Content == "" {
		return ""
	}
	if !sameConversation(parent, trigger, h.botName, private) {
		return ""
	}
	text := strings.Join(strings.Fields(parent.Content), " ")
	runes := []rune(text)
	if len(runes) > replyTargetLimit {
		text = string(runes[:replyTargetLimit-1]) + "…"
	}
	date := time.UnixMilli(parent.Timestamp).Format("2006-01-02")
	return "[Reply target: @" + parent.Username + " · " + date + ": " + text + "]"
}

func sameConversation(parent, trigger StoredMessage, botName string, private bool) bool {
	if private {
		member := func(name string) bool { return name == trigger.Username || name == botName }
		return parent.RoomID == "" && parent.ToUser != "" && member(parent.Username) && member(parent.ToUser)
	}
	return parent.RoomID != "" && parent.RoomID == trigger.RoomID
}
