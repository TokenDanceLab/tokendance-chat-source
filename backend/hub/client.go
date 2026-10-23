package hub

import (
	"context"
	"encoding/json"

	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"tokendancechat/backend/llm"
	"tokendancechat/backend/store"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 8192

	// Rate limit: max messages per second per connection.
	maxMessagesPerSecond = 5

	// Application close code: another guest connection took over this handle.
	closeGuestTakenOver = 4001
)

// Client represents a single WebSocket connection.
type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	username string
	// authenticatedUsername is bound from the HttpOnly Chat session cookie at
	// the HTTP-to-WebSocket upgrade boundary. It is never supplied by the
	// client's join payload.
	authenticatedUsername         string
	authenticatedSubject          string
	authenticatedSessionExpiresAt int64
	// authenticatedSessionCurrent rechecks durable session state before join.
	// OIDC sessions supply this callback so logout generations cannot be
	// bypassed by an already-upgraded but not-yet-joined socket.
	authenticatedSessionCurrent func() bool
	// authenticatedSessionGeneration is the durable Account logout generation
	// bound to this OIDC socket. It lets a delayed logout event remove only
	// sessions that predate that event.
	authenticatedSessionGeneration int64
	// ip is the trusted client address, used for the per-IP demo budget.
	ip string
	// rate limiting
	msgTimestamps   []int64
	msgTimestampsMu sync.Mutex
	// current room
	currentRoomID string
	roomMu        sync.RWMutex

	// closed signals WritePump to stop without closing send. Direct sends can
	// race with a kick/unregister, and closing send while a ReadPump handler is
	// still running would panic; a signal channel keeps those sends harmless.
	closed    chan struct{}
	closeOnce sync.Once
}

func (c *Client) shutdownSend() {
	c.closeOnce.Do(func() {
		if c.closed != nil {
			close(c.closed)
		}
	})
}

// getCurrentRoomID returns the client's current room ID with read locking.
func (c *Client) getCurrentRoomID() string {
	c.roomMu.RLock()
	defer c.roomMu.RUnlock()
	return c.currentRoomID
}

// setCurrentRoomID sets the client's current room ID with write locking.
func (c *Client) setCurrentRoomID(roomID string) {
	c.roomMu.Lock()
	c.currentRoomID = roomID
	c.roomMu.Unlock()
}

// NewClient creates a new WebSocket client.
func NewClient(h *Hub, conn *websocket.Conn, ip ...string) *Client {
	defaultRoomID := h.DefaultRoomID()
	clientIP := ""
	if len(ip) > 0 {
		clientIP = ip[0]
	}
	return &Client{
		hub:           h,
		conn:          conn,
		send:          make(chan []byte, 256),
		closed:        make(chan struct{}),
		ip:            clientIP,
		currentRoomID: defaultRoomID,
	}
}

// SetAuthenticatedUsername binds this socket to a validated Chat session.
func (c *Client) SetAuthenticatedUsername(username string) {
	c.authenticatedUsername = username
}

// SetAuthenticatedSessionValidator installs a durable validity check for the
// authenticated session bound to this socket.
func (c *Client) SetAuthenticatedSessionValidator(validate func() bool) {
	c.authenticatedSessionCurrent = validate
}

// SetAuthenticatedSessionGeneration records the generation bound to this
// socket. Local sessions do not use this value; OIDC sessions always do.
func (c *Client) SetAuthenticatedSessionGeneration(generation int64) {
	c.authenticatedSessionGeneration = generation
}

func (c *Client) authenticatedSessionIsCurrent() bool {
	return (c.authenticatedSessionExpiresAt == 0 || time.Now().Unix() < c.authenticatedSessionExpiresAt) && (c.authenticatedSessionCurrent == nil || c.authenticatedSessionCurrent())
}

// ReadPump pumps messages from the WebSocket connection to the hub.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("websocket read error: %v", err)
			}
			break
		}

		var msg Message
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("invalid JSON from client: %v", err)
			continue
		}

		if msg.Type != "join" && !c.authenticatedSessionIsCurrent() {
			c.hub.SendToClient(c, Message{Type: "account_logout"})
			return
		}
		switch msg.Type {
		case "bot_cancel":
			c.handleBotCancel(msg)
		case "join":
			c.handleJoin(msg)
		case "message":
			c.handleChatMessage(msg)
		case "reaction":
			c.handleReaction(msg)
		case "message_edit":
			c.handleMessageEdit(msg)
		case "mark_read":
			// Broadcast read receipt to message senders so they know their
			// messages were seen. Context indicates which conversation was read.
			c.handleMarkRead(msg)
		case "message_delete":
			c.handleMessageDelete(msg)
		case "typing_start":
			c.handleTypingStart(msg)
		case "typing_stop":
			c.handleTypingStop(msg)
		case "block":
			c.handleBlock(msg)
		case "unblock":
			c.handleUnblock(msg)
		case "block_list":
			c.handleBlockList()
		case "pin_message":
			c.handlePinMessage(msg)
		case "unpin_message":
			c.handleUnpinMessage(msg)
		case "load_history":
			c.handleLoadHistory(msg)
		case "load_private":
			c.handleLoadPrivateHistory(msg)
		case "list_private_threads":
			c.handleListPrivateThreads(msg)
		case "thread_messages":
			c.handleThreadMessages(msg)
		case "notification_prefs_set":
			c.handleNotificationPrefsSet(msg)
		case "custom_emoji_add":
			c.handleCustomEmojiAdd(msg)
		case "custom_emoji_list":
			c.handleCustomEmojiList()
		case "custom_emoji_delete":
			c.handleCustomEmojiDelete(msg)
		case "translate_message":
			c.handleTranslateMessage(msg)
		case "profile_update":
			c.handleProfileUpdate(msg)
		case "profile_get":
			c.handleProfileGet(msg)
		case "poll_create":
			c.handlePollCreate(msg)
		case "poll_vote":
			c.handlePollVote(msg)
		case "poll_close":
			c.handlePollClose(msg)
		default:
			log.Printf("unknown message type: %s", msg.Type)
		}
	}
}

func (c *Client) handleJoin(msg Message) {
	if c.username != "" {
		return
	}
	username := msg.Username
	if !ValidateUsername(username) {
		errMsg, _ := json.Marshal(Message{
			Type:      "error",
			Content:   "invalid username: 1-20 chars, letters, digits, underscore, or Chinese",
			ErrorCode: "INVALID_USERNAME",
		})
		select {
		case c.send <- errMsg:
		default:
		}
		return
	}

	// Block reserved names and every alias the assistant answers to. Letting a
	// person join as the bot would let them post as the AI and, worse, inject
	// text that the transcript treats as a previous assistant turn.
	if IsReservedUsername(username) || IsAssistantIdentity(username, c.hub.BotName()) {
		errMsg, _ := json.Marshal(Message{
			Type:      "error",
			Content:   "username is reserved",
			ErrorCode: "RESERVED_USERNAME",
		})
		select {
		case c.send <- errMsg:
		default:
		}
		return
	}

	if c.authenticatedUsername != "" {
		if !c.authenticatedSessionIsCurrent() {
			c.sendJoinError("account sign-in required", "OIDC_REQUIRED")
			return
		}
		if username != c.authenticatedUsername {
			c.sendJoinError("authenticated session does not match this username", "AUTH_FAILED")
			return
		}
	} else {
		if _, err := c.hub.store.GetOIDCUserByUsername(username); err == nil {
			c.sendJoinError("account sign-in required", "OIDC_REQUIRED")
			return
		}
		registered, err := c.hub.store.UserExists(username)
		if err != nil {
			c.sendJoinError("authentication lookup failed", "AUTH_FAILED")
			return
		}
		if registered {
			c.sendJoinError("authentication required", "AUTH_REQUIRED")
			return
		}
	}

	c.username = username
	c.hub.SetLastSeen(c.username, time.Now().UnixMilli())
	c.hub.register <- c

	// Join default room.
	c.hub.JoinRoom(c.currentRoomID, c.username)

	// Send room-specific history to the joining client.
	historyMessages := c.hub.store.GetRoomMessages(c.currentRoomID, 100, 0)
	historyPayload, _ := json.Marshal(Message{
		Type:     "history",
		Messages: historyMessages,
		RoomID:   c.currentRoomID,
	})
	select {
	case c.send <- historyPayload:
	default:
	}

	// Send room list to the joining client.
	rooms := c.hub.ListRooms()
	roomListPayload, _ := json.Marshal(Message{
		Type:  "room_list",
		Rooms: rooms,
	})
	select {
	case c.send <- roomListPayload:
	default:
	}

	// Send notification preferences.
	notifPrefs := c.hub.ListNotificationPrefs(c.username)
	notifPrefsPayload, _ := json.Marshal(Message{
		Type:       "notification_prefs",
		NotifPrefs: notifPrefs,
	})
	select {
	case c.send <- notifPrefsPayload:
	default:
	}

	// Send user status list to the joining client.
	allUsers := c.hub.AllUserStatus()
	userStatusPayload, _ := json.Marshal(Message{
		Type:  "user_status",
		Users: allUsers,
	})
	select {
	case c.send <- userStatusPayload:
	default:
	}

	// Broadcast user_joined to all clients.
	now := time.Now().UnixMilli()
	joinMsg, _ := json.Marshal(Message{
		Type:      "user_joined",
		Username:  c.username,
		Online:    c.hub.onlineUsers(),
		Timestamp: now,
	})
	select {
	case c.hub.broadcast <- joinMsg:
	default:
	}

	// Broadcast updated user_status to all clients.
	statusBroadcast, _ := json.Marshal(Message{
		Type:  "user_status",
		Users: c.hub.AllUserStatus(),
	})
	select {
	case c.hub.broadcast <- statusBroadcast:
	default:
	}
}

func (c *Client) sendJoinError(content, code string) {
	errMsg, _ := json.Marshal(Message{
		Type:      "error",
		Content:   content,
		ErrorCode: code,
	})
	select {
	case c.send <- errMsg:
	default:
	}
}

// sendChatError rejects one sent message. When the client supplied a
// client_message_id the error carries it back, so the optimistic bubble can be
// marked "not sent" and retried instead of silently pretending delivery.
func (c *Client) sendChatError(content, code, clientMessageID string) {
	data, _ := json.Marshal(Message{
		Type:            "error",
		Content:         content,
		ErrorCode:       code,
		ClientMessageID: clientMessageID,
	})
	select {
	case c.send <- data:
	default:
	}
}

func (c *Client) handleChatMessage(msg Message) {
	if !c.authenticatedSessionIsCurrent() {
		c.hub.SendToClient(c, Message{Type: "account_logout"})
		return
	}
	if c.username == "" {
		c.sendChatError("you must join before sending messages", "NOT_JOINED", msg.ClientMessageID)
		return
	}

	// Sanitize content.
	content := sanitizeContent(msg.Content)
	if content == "" {
		c.sendChatError("message content cannot be empty", "EMPTY_MESSAGE", msg.ClientMessageID)
		return
	}

	// Rate limiting: max 5 messages per second.
	if !c.checkRateLimit() {
		c.sendChatError("rate limit exceeded: max 5 messages per second", "RATE_LIMITED", msg.ClientMessageID)
		return
	}
	if !c.hub.allowClientMessage(c.ip) {
		log.Printf("chat message rate limit: ip=%s user=%s", c.ip, c.username)
		c.sendChatError("发送过于频繁，请稍后再试", "RATE_LIMITED", msg.ClientMessageID)
		return
	}

	currentRoom := c.getCurrentRoomID()

	// Private DM to the assistant (TokenBot): never broadcast to the room,
	// streamed only back to the sender. This separates the public chat room
	// from a private 1:1 bot conversation. An unsupported recipient must never
	// fall back to public delivery of a message intended to be private.
	if msg.To != "" {
		if msg.To != c.hub.BotName() {
			c.sendChatError("private recipient is unavailable", "INVALID_RECIPIENT", msg.ClientMessageID)
			return
		}
		c.handlePrivateBotMessage(msg, content)
		return
	}
	if currentRoom == "" {
		c.sendError("room is unavailable", "INVALID_ROOM")
		return
	}
	if !c.validateMessageReferences(&msg, currentRoom, false) {
		return
	}

	// Save to store.
	storedMsg, err := c.insertActorMessage(content, msg.ReplyToID, currentRoom, "", msg.ThreadID)
	if err != nil {
		log.Printf("failed to insert message: %v", err)
		c.sendChatError("failed to save message, please try again", "SERVER_ERROR", msg.ClientMessageID)
		return
	}

	// Broadcast only to clients in the current room. Public room messages may
	// contain private context after a room switch, so global fanout is unsafe.
	broadcastMsg, _ := json.Marshal(Message{
		Type:            "message",
		ID:              storedMsg.ID,
		ClientMessageID: msg.ClientMessageID,
		Username:        storedMsg.Username,
		Content:         storedMsg.Content,
		Timestamp:       storedMsg.Timestamp,
		ReplyToID:       storedMsg.ReplyToID,
		ReplyToContent:  msg.ReplyToContent,
		ReplyToUser:     msg.ReplyToUser,
		ThreadID:        storedMsg.ThreadID,
		RoomID:          currentRoom,
		MentionAll:      containsAllMention(content),
	})
	c.hub.BroadcastToRoom(broadcastMsg, currentRoom)

	// Notify @mentioned users (skip self, assistants, and reserved @all/@everyone).
	if containsAllMention(content) {
		allNotify, _ := json.Marshal(Message{
			Type:       "mention_all",
			From:       c.username,
			Content:    content,
			MessageID:  storedMsg.ID,
			RoomID:     currentRoom,
			Timestamp:  storedMsg.Timestamp,
			MentionAll: true,
		})
		c.hub.BroadcastToRoom(allNotify, currentRoom)
	} else {
		for _, mention := range parseMentions(content) {
			if mention == c.username || mention == c.hub.BotName() {
				continue
			}
			notifyMsg, _ := json.Marshal(Message{
				Type:      "mention_notify",
				From:      c.username,
				Content:   content,
				MessageID: storedMsg.ID,
				RoomID:    currentRoom,
				Timestamp: storedMsg.Timestamp,
			})
			c.hub.SendToUserInRoom(mention, currentRoom, notifyMsg)
		}
	}

	// Check for @mentions and route the single bot (TokenBot).
	// Replying to the bot's own message addresses it, matching the way people
	// answer a specific message in a group instead of typing @ again.
	addressed := addressedToBot(content, msg.ReplyToUser, c.hub.BotName())
	if addressed && c.username != c.hub.BotName() && c.hub.LLMClient() != nil {
		c.hub.enqueueRun(c, storedMsg, false)
	} else if addressed && c.username != c.hub.BotName() && c.hub.LLMClient() == nil {
		// Bot mentioned but not configured — send error feedback
		systemMsg, _ := json.Marshal(Message{
			Type:     "system",
			Username: "system",
			Content:  "TokenBot is not configured on this server.",
			RoomID:   currentRoom,
		})
		c.hub.BroadcastToRoom(systemMsg, currentRoom)
	} else if !addressed && c.username != c.hub.BotName() {
		// No @mention: TokenBot may join on its own when ambient mode is on.
		c.hub.maybeAmbientReply(c, storedMsg)
	}
}

// handleReaction processes a reaction toggle request.
func (c *Client) handleReaction(msg Message) {
	if c.username == "" {
		return
	}
	if !c.checkRateLimit() {
		return
	}
	messageID := msg.ID
	if messageID == "" {
		messageID = msg.MessageID
	}
	emoji := msg.Emoji
	if messageID == "" || emoji == "" {
		return
	}

	reactions, err := c.hub.store.ToggleReaction(messageID, emoji, c.username)
	if err != nil {
		log.Printf("failed to toggle reaction: %v", err)
		return
	}

	reactionMsg, _ := json.Marshal(Message{
		Type:      "reaction_update",
		ID:        messageID,
		Reactions: reactions,
	})
	select {
	case c.hub.broadcast <- reactionMsg:
	default:
	}
}

// handleMessageEdit processes a message edit request.
func (c *Client) handleMessageEdit(msg Message) {
	if c.username == "" {
		return
	}
	messageID := msg.ID
	content := sanitizeContent(msg.Content)
	if messageID == "" || content == "" {
		return
	}

	stored, err := c.hub.store.GetMessageByID(messageID)
	if err != nil {
		log.Printf("message_edit: message not found: %v", err)
		return
	}
	if stored.Username != c.username {
		errMsg, _ := json.Marshal(Message{
			Type:      "error",
			Content:   "you can only edit your own messages",
			ErrorCode: "NOT_OWNER",
		})
		select {
		case c.send <- errMsg:
		default:
		}
		return
	}

	if !c.checkRateLimit() {
		return
	}

	updated, err := c.hub.store.UpdateMessage(messageID, content)
	if err != nil {
		log.Printf("failed to update message: %v", err)
		return
	}

	editMsg, _ := json.Marshal(Message{
		Type:      "message_edit",
		ID:        updated.ID,
		Username:  updated.Username,
		Content:   updated.Content,
		Timestamp: updated.Timestamp,
		Edited:    true,
	})
	c.hub.BroadcastToRoom(editMsg, updated.RoomID)
}

// checkRateLimit returns true if the message is allowed (within rate limit).
func (c *Client) checkRateLimit() bool {
	c.msgTimestampsMu.Lock()
	defer c.msgTimestampsMu.Unlock()

	now := time.Now().UnixMilli()
	oneSecondAgo := now - 1000

	// Filter out old timestamps.
	filtered := make([]int64, 0, len(c.msgTimestamps))
	for _, ts := range c.msgTimestamps {
		if ts > oneSecondAgo {
			filtered = append(filtered, ts)
		}
	}

	if len(filtered) >= maxMessagesPerSecond {
		c.msgTimestamps = filtered
		return false
	}

	filtered = append(filtered, now)
	c.msgTimestamps = filtered
	return true
}

// WritePump pumps messages from the hub to the WebSocket connection.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		// Notify hub to unregister on write failure.
		select {
		case c.hub.unregister <- c:
		default:
		}
		c.conn.Close()
	}()

	for {
		select {
		case <-c.closed:
			c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Printf("websocket write error: %v", err)
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// MaxContentLength is the protocol-wide message limit, enforced here and
// advertised to clients through GET /api/config.
const MaxContentLength = 2000

// mentionRegex matches @username patterns in message content.
var mentionRegex = regexp.MustCompile(`@([\p{Han}\p{L}\p{N}_]+)`)

// allMentionRegex matches @all / @everyone / @here / @channel mentions.
var allMentionRegex = regexp.MustCompile(`(?i)@(all|everyone|here|channel)\b`)

// parseMentions extracts all @mentioned usernames from content.
func parseMentions(content string) []string {
	matches := mentionRegex.FindAllStringSubmatch(content, -1)
	mentions := make([]string, 0, len(matches))
	seen := make(map[string]bool)
	for _, m := range matches {
		if len(m) > 1 {
			username := m[1]
			if !seen[username] {
				seen[username] = true
				mentions = append(mentions, username)
			}
		}
	}
	return mentions
}

// containsAllMention returns true if the content contains @all, @everyone, @here, or @channel.
func containsAllMention(content string) bool {
	return allMentionRegex.MatchString(content)
}

type assistantMentionTargets struct {
	TokenBot bool
}

func assistantMentionTarget(content, botName string) assistantMentionTargets {
	targets := assistantMentionTargets{}
	for _, mention := range parseMentions(content) {
		if isAssistantAlias(mention, botName, "bot", "tokenbot", "webuichat", "webuibot", "webui") {
			targets.TokenBot = true
		}
	}
	return targets
}

// addressedToBot reports whether a public message is meant for the assistant:
// either it mentions the bot, or it replies to one of the bot's messages.
func addressedToBot(content, replyToUser, botName string) bool {
	if assistantMentionTarget(content, botName).TokenBot {
		return true
	}
	return botName != "" && strings.EqualFold(replyToUser, botName)
}

func isAssistantAlias(mention, canonical string, aliases ...string) bool {
	if canonical != "" && strings.EqualFold(mention, canonical) {
		return true
	}
	for _, alias := range aliases {
		if strings.EqualFold(mention, alias) {
			return true
		}
	}
	return false
}

// IsAssistantIdentity reports whether a username belongs to the assistant.
func IsAssistantIdentity(username, botName string) bool {
	return isAssistantAlias(username, botName, "bot", "tokenbot", "webuichat", "webuibot", "webui")
}

func (c *Client) handleMessageDelete(msg Message) {
	if c.username == "" {
		return
	}
	messageID := msg.ID
	if messageID == "" {
		return
	}

	// Verify ownership: only the message author can delete.
	stored, err := c.hub.store.GetMessageByID(messageID)
	if err != nil {
		log.Printf("message_delete: message not found: %v", err)
		return
	}
	if stored.Username != c.username {
		errMsg, _ := json.Marshal(Message{
			Type:      "error",
			Content:   "you can only delete your own messages",
			ErrorCode: "NOT_OWNER",
		})
		select {
		case c.send <- errMsg:
		default:
		}
		return
	}

	if err := c.hub.store.MarkDeleted(messageID); err != nil {
		log.Printf("failed to mark message deleted: %v", err)
		return
	}

	// Broadcast deletion.
	delMsg, _ := json.Marshal(Message{
		Type:    "message_delete",
		ID:      messageID,
		Deleted: true,
	})
	select {
	case c.hub.broadcast <- delMsg:
	default:
	}
}

// Rate limited to once per 3 seconds per user.
func (c *Client) handleTypingStart(msg Message) {
	if c.username == "" {
		return
	}
	if !c.hub.ShouldBroadcastTyping(c.username) {
		return
	}
	c.hub.BroadcastTyping(c.username, "typing", msg.Context, c.getCurrentRoomID(), msg.To)
}

// handleTypingStop broadcasts that the user stopped typing.
func (c *Client) handleTypingStop(msg Message) {
	if c.username == "" {
		return
	}
	c.hub.BroadcastTyping(c.username, "typing_stop", msg.Context, c.getCurrentRoomID(), msg.To)
}

// handlePinMessage pins a message in the current room.
func (c *Client) handlePinMessage(msg Message) {
	if c.username == "" {
		return
	}
	messageID := msg.ID
	if messageID == "" {
		return
	}
	roomID := c.getCurrentRoomID()
	if err := c.hub.PinMessage(roomID, messageID, c.username); err != nil {
		log.Printf("pin error: %v", err)
		return
	}
	// Broadcast pin event.
	pinMsg, _ := json.Marshal(Message{
		Type:     "pinned",
		ID:       messageID,
		RoomID:   roomID,
		PinnedBy: c.username,
		PinnedAt: time.Now().UnixMilli(),
		Pinned:   true,
	})
	c.hub.BroadcastToRoom(pinMsg, roomID)
}

// handleUnpinMessage unpins a message in the current room.
func (c *Client) handleUnpinMessage(msg Message) {
	if c.username == "" {
		return
	}
	messageID := msg.ID
	if messageID == "" {
		return
	}
	roomID := c.getCurrentRoomID()
	c.hub.UnpinMessage(roomID, messageID)
	// Broadcast unpin event.
	unpinMsg, _ := json.Marshal(Message{
		Type:   "unpinned",
		ID:     messageID,
		RoomID: roomID,
		Pinned: false,
	})
	c.hub.BroadcastToRoom(unpinMsg, roomID)
}

// handleNotificationPrefsSet stores notification preferences for a conversation and
// also syncs the old muted_conversations table for backward compatibility.
func (c *Client) handleNotificationPrefsSet(msg Message) {
	if c.username == "" {
		return
	}
	key := msg.Key
	if key == "" {
		return
	}
	mutedUntil := msg.MutedUntil
	showPreview := true
	if msg.ShowPreview != nil {
		showPreview = *msg.ShowPreview
	}
	if err := c.hub.SetNotificationPrefs(c.username, key, mutedUntil, showPreview); err != nil {
		log.Printf("notification_prefs_set error: %v", err)
		return
	}

	// Echo back the updated prefs for this conversation.
	mutedUntilVal, showPreviewVal, _ := c.hub.GetNotificationPrefs(c.username, key)
	showPreviewBool := showPreviewVal
	confirm, _ := json.Marshal(Message{
		Type:        "notification_prefs",
		Key:         key,
		MutedUntil:  mutedUntilVal,
		ShowPreview: &showPreviewBool,
	})
	select {
	case c.send <- confirm:
	default:
	}
}

// handleLoadHistory sends older messages to the requesting client for pagination.
func (c *Client) handleLoadHistory(msg Message) {
	if c.username == "" {
		return
	}
	roomID := c.getCurrentRoomID()
	limit := 50
	before := msg.Timestamp
	if before <= 0 {
		return
	}
	messages := c.hub.store.GetRoomMessages(roomID, limit, before)
	historyPayload, _ := json.Marshal(Message{
		Type:       "history",
		Messages:   messages,
		RoomID:     roomID,
		Pagination: true,
	})
	select {
	case c.send <- historyPayload:
	default:
	}
}

// handleLoadPrivateHistory serves the caller's own 1:1 thread over the
// WebSocket. Guest identities exist only on this connection (never as an HTTP
// session), so the HTTP GET /api/messages?to= path can never authenticate
// them; the WS channel already knows who they are. The scope is fixed to
// "the connected user × the named peer", so nobody can read a thread they
// are not part of.
func (c *Client) handleLoadPrivateHistory(msg Message) {
	if c.username == "" {
		return
	}
	to := strings.TrimSpace(msg.To)
	if to == "" || to == c.username {
		return
	}
	messages := c.hub.store.GetMessagesBetweenThread(c.username, to, strings.TrimSpace(msg.ThreadID), 100)
	if messages == nil {
		messages = []StoredMessage{}
	}
	payload, _ := json.Marshal(Message{
		Type:     "private_history",
		To:       to,
		ThreadID: strings.TrimSpace(msg.ThreadID),
		Messages: messages,
	})
	select {
	case c.send <- payload:
	default:
	}
}

// PrivateThreadSummary is the sidebar row for one assistant lane.
type PrivateThreadSummary = store.PrivateThread

// handleListPrivateThreads returns the caller's assistant lanes: the main
// chat plus every side thread, newest activity first.
func (c *Client) handleListPrivateThreads(msg Message) {
	if c.username == "" {
		return
	}
	bot := strings.TrimSpace(msg.To)
	if bot == "" || bot == c.username {
		return
	}
	threads := c.hub.store.ListPrivateThreads(c.username, bot)
	if threads == nil {
		threads = []store.PrivateThread{}
	}
	payload, _ := json.Marshal(Message{
		Type:     "private_threads",
		To:       bot,
		Threads:  threads,
	})
	select {
	case c.send <- payload:
	default:
	}
}

// --- Block handlers ---

func (c *Client) handleBlock(msg Message) {
	if c.username == "" {
		return
	}
	blocked := msg.Username
	if blocked == "" || blocked == c.username {
		return
	}
	if err := c.hub.BlockUser(c.username, blocked); err != nil {
		log.Printf("block error: %v", err)
		return
	}
	confirm, _ := json.Marshal(Message{
		Type:     "block",
		Username: blocked,
	})
	select {
	case c.send <- confirm:
	default:
	}
}

func (c *Client) handleUnblock(msg Message) {
	if c.username == "" {
		return
	}
	blocked := msg.Username
	if blocked == "" {
		return
	}
	c.hub.UnblockUser(c.username, blocked)
	confirm, _ := json.Marshal(Message{
		Type:     "unblock",
		Username: blocked,
	})
	select {
	case c.send <- confirm:
	default:
	}
}

func (c *Client) handleBlockList() {
	if c.username == "" {
		return
	}
	blocked := c.hub.store.GetBlockedUsers(c.username)
	list, _ := json.Marshal(Message{
		Type:    "block_list",
		Blocked: blocked,
	})
	select {
	case c.send <- list:
	default:
	}
}

// handleMarkRead broadcasts a read receipt so message senders know their messages were seen.
func (c *Client) handleMarkRead(msg Message) {
	if c.username == "" {
		return
	}
	// Notify users who sent messages to this reader based on context.
	receipt := Message{
		Type:    "read_receipt",
		From:    c.username,
		Context: msg.Context,
		To:      msg.To,
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		return
	}
	// Broadcast to all so senders can see their messages were read.
	select {
	case c.hub.broadcast <- data:
	default:
	}
}

// handlePrivateBotMessage handles a DM to the assistant (To == BotName). The
// message is persisted as a private thread row (empty room_id, to_user=bot) and
// echoed only to the sender; it is never broadcast to the room.
func (c *Client) handlePrivateBotMessage(msg Message, content string) {
	if !c.validateMessageReferences(&msg, "", true) {
		return
	}
	storedMsg, err := c.insertActorMessage(content, msg.ReplyToID, "", c.hub.BotName(), msg.ThreadID)
	if err != nil {
		log.Printf("failed to insert private message: %v", err)
		c.sendChatError("failed to save message, please try again", "SERVER_ERROR", msg.ClientMessageID)
		return
	}

	// Echo the user's own message back privately (keeps multiple tabs in sync).
	c.hub.sendToActor(c.actorID(), Message{
		Type:            "message",
		ID:              storedMsg.ID,
		ClientMessageID: msg.ClientMessageID,
		Username:        storedMsg.Username,
		Content:         storedMsg.Content,
		Timestamp:       storedMsg.Timestamp,
		ReplyToID:       storedMsg.ReplyToID,
		ReplyToContent:  msg.ReplyToContent,
		ReplyToUser:     msg.ReplyToUser,
		ThreadID:        storedMsg.ThreadID,
		RoomID:          "",
		To:              c.hub.BotName(),
		Private:         true,
	})

	// Trigger the private bot response (per-user cooldown + CAS to avoid overlap).
	if c.hub.LLMClient() == nil {
		c.hub.SendToClient(c, Message{
			Type:    "system",
			Content: c.hub.BotName() + " is not configured on this server.",
			Private: true,
		})
		return
	}
	c.hub.enqueueRun(c, storedMsg, true)
}

const botErrorMessage = "Sorry, I could not generate a response. Please try again."

// logReply is the one structured line operators use to watch token cost and
// prompt-cache hit rate.
func logReply(mode, user, model, promptSHA string, messages int, result llm.Result, elapsed time.Duration) {
	hitRate := 0.0
	if result.Usage.PromptTokens > 0 {
		hitRate = float64(result.Usage.CachedTokens) / float64(result.Usage.PromptTokens)
	}
	log.Printf("bot reply mode=%s user=%s model=%s prompt=%s prompt_sha=%s messages=%d tool_calls=%d latency=%s tokens_in=%d cached=%d cache_hit=%.2f tokens_out=%d reasoning_tokens=%d",
		mode, user, model, TokenBotPromptVersion, promptSHA, messages, len(result.ToolCalls), elapsed.Round(time.Millisecond),
		result.Usage.PromptTokens, result.Usage.CachedTokens, hitRate, result.Usage.CompletionTokens, result.Usage.ReasoningTokens)
}

func usageTokens(result llm.Result) int64 {
	return result.Usage.PromptTokens + result.Usage.CompletionTokens
}

// botBudgetMessage keeps quota denials short and non-technical. The room sees
// only the requester's own system line, never a broadcast.
func botBudgetMessage(reason string) string {
	switch reason {
	case "busy":
		return "TokenBot 正在处理其他请求，请稍后再试。"
	case "user":
		return "你今天与 TokenBot 的对话次数已用完，明天再来。"
	default:
		return "今天的演示额度已用完，明天再来。"
	}
}

// sanitizeContent trims whitespace, strips null bytes, and enforces max length.
// Markup is preserved: rendering escapes it, and stripping tags here only
// corrupted code snippets that users pasted.
func sanitizeContent(content string) string {
	content = strings.ReplaceAll(content, "\x00", "")
	content = strings.TrimSpace(content)
	if len([]rune(content)) > MaxContentLength {
		content = string([]rune(content)[:MaxContentLength])
	}
	return content
}

// maxBotContentLength is the maximum char count for bot/agent response content.
const maxBotContentLength = 10000

// sanitizeBotContent strips null bytes and enforces the bot length cap.
// Markup is kept and escaped at render time, so code samples survive.
func sanitizeBotContent(content string) string {
	content = strings.ReplaceAll(content, "\x00", "")
	content = strings.TrimSpace(content)
	if len([]rune(content)) > maxBotContentLength {
		content = string([]rune(content)[:maxBotContentLength])
	}
	return content
}

// IsReservedUsername blocks system and infrastructure usernames.
func IsReservedUsername(username string) bool {
	lower := strings.ToLower(username)
	reserved := []string{"system", "server", "admin", "moderator", "mod", "root", "null", "undefined", "everyone", "all", "chat", "here", "channel"}
	for _, r := range reserved {
		if lower == r {
			return true
		}
	}
	return false
}

// --- Thread message handlers ---

// messageInConversation checks persisted routing, never client-supplied
// private flags or reply usernames. Private messages belong only to this
// user's conversation with the configured bot.
func (c *Client) messageInConversation(message StoredMessage, roomID string, private bool) bool {
	if message.ID == "" || message.Deleted {
		return false
	}
	if !private {
		return message.ToUser == "" && message.RoomID == roomID
	}
	bot := c.hub.BotName()
	return c.username != "" && bot != "" && message.RoomID == "" &&
		((message.Username == c.username && message.ToUser == bot) ||
			(message.Username == bot && message.ToUser == c.username))
}

// validateMessageReferences prevents cross-conversation thread/reply writes
// and derives reply metadata from an authorized stored message. Missing,
// deleted and inaccessible references produce the same content-free error.
func (c *Client) validateMessageReferences(msg *Message, roomID string, private bool) bool {
	msg.ReplyToUser, msg.ReplyToContent = "", ""
	for i, id := range []string{msg.ThreadID, msg.ReplyToID} {
		if id == "" {
			continue
		}
		// In a private 1:1, a thread_id with the "lane-" prefix names a
		// conversation lane (client-generated), not a parent message — lanes need
		// no reference check. The reply slot is always checked, even if it
		// carries the same value.
		if i == 0 && private && strings.HasPrefix(id, "lane-") {
			continue
		}
		parent, err := c.hub.store.GetMessageByID(id)
		if err != nil || !c.messageInConversation(parent, roomID, private) {
			c.sendError("message reference is unavailable", "MESSAGE_REFERENCE_UNAVAILABLE")
			return false
		}
		if id == msg.ReplyToID {
			msg.ReplyToUser, msg.ReplyToContent = parent.Username, parent.Content
		}
	}
	return true
}

// handleThreadMessages returns all replies in a thread for a given parent message.
func (c *Client) handleThreadMessages(msg Message) {
	if c.username == "" {
		return
	}
	parentMessageID := msg.ParentMessageID
	if parentMessageID == "" {
		parentMessageID = msg.ID
	}
	if parentMessageID == "" {
		return
	}
	parent, err := c.hub.store.GetMessageByID(parentMessageID)
	private := parent.ToUser != ""
	if err != nil || !c.messageInConversation(parent, c.getCurrentRoomID(), private) {
		c.sendError("thread is unavailable", "THREAD_UNAVAILABLE")
		return
	}
	threadMessages := make([]StoredMessage, 0)
	for _, child := range c.hub.store.GetThreadMessages(parentMessageID) {
		if child.ThreadID == parentMessageID && c.messageInConversation(child, parent.RoomID, private) {
			threadMessages = append(threadMessages, child)
		}
	}
	payload, _ := json.Marshal(Message{
		Type:            "thread_messages",
		ParentMessageID: parentMessageID,
		Messages:        threadMessages,
		RoomID:          parent.RoomID,
		Private:         private,
	})
	select {
	case c.send <- payload:
	default:
	}
}

// --- Profile handlers ---

func (c *Client) handleProfileUpdate(msg Message) {
	if c.username == "" {
		return
	}
	if err := c.hub.store.UpsertUserProfile(c.username, msg.DisplayName, msg.AvatarURL, msg.Bio, msg.Status, time.Now().UnixMilli()); err != nil {
		log.Printf("profile_update: upsert error: %v", err)
		return
	}
	// Broadcast updated user_status to all clients.
	statusMsg, _ := json.Marshal(Message{Type: "user_status", Users: c.hub.AllUserStatus()})
	select {
	case c.hub.broadcast <- statusMsg:
	default:
	}
}

func (c *Client) handleProfileGet(msg Message) {
	if c.username == "" {
		return
	}
	target := msg.Username
	if target == "" {
		target = c.username
	}
	profile, err := c.hub.store.GetUserProfile(target)
	if err != nil {
		profile = &store.UserProfile{Username: target}
	}
	resp, _ := json.Marshal(Message{
		Type: "profile_get", Username: profile.Username,
		DisplayName: profile.DisplayName, AvatarURL: profile.AvatarURL,
		Bio: profile.Bio, Status: profile.Status,
	})
	select {
	case c.send <- resp:
	default:
	}
}

// --- Poll handlers ---

func (c *Client) handlePollCreate(msg Message) {
	if c.username == "" || msg.Poll == nil || msg.Poll.Question == "" || len(msg.Poll.Options) < 2 {
		return
	}
	if !c.checkRateLimit() {
		return
	}
	roomID := c.getCurrentRoomID()
	pollID := msg.Poll.ID
	if pollID == "" {
		pollID = uuid.New().String()
	}
	poll := &Poll{
		ID:             pollID,
		RoomID:         roomID,
		Creator:        c.username,
		Question:       sanitizeContent(msg.Poll.Question),
		Options:        msg.Poll.Options,
		MultipleChoice: msg.Poll.MultipleChoice,
		IsAnonymous:    msg.Poll.IsAnonymous,
		IsClosed:       false,
		Votes:          make(map[int]int),
		Voters:         make(map[int][]string),
		CreatedAt:      time.Now().UnixMilli(),
	}
	for i := range poll.Options {
		poll.Options[i] = sanitizeContent(poll.Options[i])
	}
	if err := c.hub.store.CreatePoll(poll); err != nil {
		log.Printf("poll_create: failed: %v", err)
		return
	}
	broadcastMsg, _ := json.Marshal(Message{Type: "poll_created", ID: poll.ID, Username: c.username, RoomID: roomID, Poll: poll})
	c.hub.BroadcastToRoom(broadcastMsg, roomID)
}

func (c *Client) handlePollVote(msg Message) {
	if c.username == "" || msg.ID == "" {
		return
	}
	if !c.checkRateLimit() {
		return
	}
	if err := c.hub.store.VotePoll(msg.ID, c.username, msg.OptionIndex); err != nil {
		log.Printf("poll_vote: failed: %v", err)
		return
	}
	updated, err := c.hub.store.GetPoll(msg.ID)
	if err != nil {
		return
	}
	broadcastMsg, _ := json.Marshal(Message{Type: "poll_vote_update", ID: msg.ID, Poll: updated, RoomID: updated.RoomID})
	c.hub.BroadcastToRoom(broadcastMsg, updated.RoomID)
}

func (c *Client) handlePollClose(msg Message) {
	if c.username == "" || msg.ID == "" {
		return
	}
	poll, err := c.hub.store.GetPoll(msg.ID)
	if err != nil {
		return
	}
	if poll.Creator != c.username {
		errMsg, _ := json.Marshal(Message{Type: "error", Content: "only the poll creator can close the poll", ErrorCode: "NOT_OWNER"})
		select {
		case c.send <- errMsg:
		default:
		}
		return
	}
	if err := c.hub.store.ClosePoll(msg.ID); err != nil {
		return
	}
	updated, _ := c.hub.store.GetPoll(msg.ID)
	broadcastMsg, _ := json.Marshal(Message{Type: "poll_closed", ID: msg.ID, Poll: updated, RoomID: updated.RoomID})
	c.hub.BroadcastToRoom(broadcastMsg, updated.RoomID)
}

// sendError sends an error message to the current client.
func (c *Client) sendError(content, errorCode string) {
	errMsg, _ := json.Marshal(Message{
		Type:      "error",
		Content:   content,
		ErrorCode: errorCode,
	})
	select {
	case c.send <- errMsg:
	default:
	}
}

// --- Call signaling handlers ---

// Group call room routing: forward directly to the target user within the room.

// Verify sender and target are in the room.

// 1:1 call (original logic)

// Log the call start.

// Forward to target user as incoming call.

// Callee is offline — mark as missed.

// Group call room routing: forward answer SDP to the caller directly.

// In room context, the "from" field is the original caller.

// 1:1 call (original logic)

// Only the callee can accept.

// Forward answer SDP to caller.

// Only the callee can reject.

// Notify caller.

// Group call room: notify the specific peer.

// 1:1 call (original logic)

// Either party can end.

// Notify the other party.

// Group call room: relay to the target peer directly.

// 1:1 call (original logic)

// Either party can send ICE candidates.

// Relay to the other party.

// --- Call room handlers (multi-party group calls) ---

// Include the creator as a participant.

// Confirm to creator.

// Send invite to each participant (excluding creator).

// Already in the room — still send existing participants.

// Send existing participants to the joiner (excluding self).

// Notify existing participants about the new joiner.

// Notify remaining participants.

// handleCustomEmojiAdd handles uploading/registering a new custom emoji.
func (c *Client) handleCustomEmojiAdd(msg Message) {
	if c.username == "" {
		return
	}
	if !c.checkRateLimit() {
		return
	}

	name := strings.TrimSpace(msg.EmojiName)
	url := strings.TrimSpace(msg.EmojiURL)
	if name == "" || url == "" {
		errMsg, _ := json.Marshal(Message{
			Type:      "error",
			Content:   "emoji name and URL are required",
			ErrorCode: "INVALID_EMOJI",
		})
		select {
		case c.send <- errMsg:
		default:
		}
		return
	}

	// Validate name: alphanumeric + underscore, max 32 chars.
	if len(name) > 32 {
		errMsg, _ := json.Marshal(Message{
			Type:      "error",
			Content:   "emoji name must be 32 characters or fewer",
			ErrorCode: "INVALID_EMOJI",
		})
		select {
		case c.send <- errMsg:
		default:
		}
		return
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(name) {
		errMsg, _ := json.Marshal(Message{
			Type:      "error",
			Content:   "emoji name must contain only letters, digits, and underscores",
			ErrorCode: "INVALID_EMOJI",
		})
		select {
		case c.send <- errMsg:
		default:
		}
		return
	}

	roomID := c.getCurrentRoomID()
	if err := c.hub.store.AddCustomEmoji(name, url, c.username, roomID); err != nil {
		log.Printf("custom_emoji_add: error: %v", err)
		errMsg, _ := json.Marshal(Message{
			Type:      "error",
			Content:   "failed to add emoji (name may already exist)",
			ErrorCode: "EMOJI_ADD_FAILED",
		})
		select {
		case c.send <- errMsg:
		default:
		}
		return
	}

	// Broadcast the new emoji to all clients.
	broadcast, _ := json.Marshal(Message{
		Type:      "custom_emoji_added",
		EmojiName: name,
		EmojiURL:  url,
		Username:  c.username,
	})
	c.hub.broadcast <- broadcast

	// Also send updated list to the client.
	emojis, err := c.hub.store.ListCustomEmojis(roomID)
	if err != nil {
		log.Printf("custom_emoji_list: error: %v", err)
		emojis = []store.CustomEmoji{}
	}
	payload2, _ := json.Marshal(Message{
		Type:   "custom_emoji_list",
		Emojis: emojis,
	})
	select {
	case c.send <- payload2:
	default:
	}
}

// handleCustomEmojiList handles listing all custom emojis.
func (c *Client) handleCustomEmojiList() {
	if c.username == "" {
		return
	}

	roomID := c.getCurrentRoomID()
	emojis, err := c.hub.store.ListCustomEmojis(roomID)
	if err != nil {
		log.Printf("custom_emoji_list: error: %v", err)
		emojis = []store.CustomEmoji{}
	}
	payload, _ := json.Marshal(Message{
		Type:   "custom_emoji_list",
		Emojis: emojis,
	})
	select {
	case c.send <- payload:
	default:
	}
}

// handleCustomEmojiDelete handles deleting a custom emoji.
func (c *Client) handleCustomEmojiDelete(msg Message) {
	if c.username == "" {
		return
	}
	if !c.checkRateLimit() {
		return
	}

	name := strings.TrimSpace(msg.EmojiName)
	if name == "" {
		return
	}

	if err := c.hub.store.DeleteCustomEmoji(name, c.username); err != nil {
		log.Printf("custom_emoji_delete: error: %v", err)
		errMsg, _ := json.Marshal(Message{
			Type:      "error",
			Content:   err.Error(),
			ErrorCode: "EMOJI_DELETE_FAILED",
		})
		select {
		case c.send <- errMsg:
		default:
		}
		return
	}

	// Broadcast deletion to all clients.
	broadcast, _ := json.Marshal(Message{
		Type:      "custom_emoji_deleted",
		EmojiName: name,
		Username:  c.username,
	})
	c.hub.broadcast <- broadcast
}

// handleTranslateMessage translates a message using the LLM and sends the result back.
func (c *Client) handleTranslateMessage(msg Message) {
	if c.username == "" {
		return
	}
	text := strings.TrimSpace(msg.Content)
	if text == "" {
		return
	}
	targetLang := msg.To
	if targetLang == "" {
		targetLang = "Chinese"
	}
	client := c.hub.LLMClient()
	if client == nil {
		errMsg, _ := json.Marshal(Message{
			Type:      "translate_result",
			MessageID: msg.MessageID,
			Content:   "[Translation unavailable]",
			To:        targetLang,
		})
		select {
		case c.send <- errMsg:
		default:
		}
		return
	}
	// Translate asynchronously via goroutine.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		prompt := "Translate the following text to " + targetLang + ". Only output the translation, nothing else."
		resp, err := client.Chat(ctx, prompt, []llm.Message{{Role: "user", Content: text}})
		var resultMsg Message
		if err == nil && resp != "" {
			resultMsg = Message{
				Type:      "translate_result",
				MessageID: msg.MessageID,
				Content:   resp,
				To:        targetLang,
			}
		} else {
			resultMsg = Message{
				Type:      "translate_result",
				MessageID: msg.MessageID,
				Content:   "[Translation failed]",
				To:        targetLang,
			}
		}
		data, _ := json.Marshal(resultMsg)
		select {
		case c.send <- data:
		default:
		}
	}()
}

// --- Chat folder handlers ---

// Fetch items for each folder.

// --- Webhook handlers ---

// Check admin: user must be group owner or admin

// Strip secrets before sending to client.

func (c *Client) insertActorMessage(content, replyID, room, to, thread string) (StoredMessage, error) {
	if s, ok := c.hub.store.(interface {
		InsertActorMessage(string, string, string, string, string, string, string) (StoredMessage, error)
	}); ok {
		return s.InsertActorMessage(c.actorID(), c.username, content, replyID, room, to, thread)
	}
	return c.hub.store.InsertMessage(c.username, content, replyID, room, to, "", thread)
}
