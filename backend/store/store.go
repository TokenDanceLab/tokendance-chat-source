package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"fmt"
	"golang.org/x/crypto/bcrypt"
	"log"
	"math/big"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"tokendancechat/backend/llm"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// StoredMessage is the message model returned by the store.
type StoredMessage struct {
	ID        string              `json:"id"`
	RunID     string              `json:"run_id,omitempty"`
	ActorID   string              `json:"-"`
	Username  string              `json:"username"`
	Content   string              `json:"content"`
	Timestamp int64               `json:"timestamp"`
	ReplyToID string              `json:"reply_to_id,omitempty"`
	RoomID    string              `json:"room_id,omitempty"`
	Deleted   bool                `json:"deleted"`
	Edited    bool                `json:"edited"`
	ToUser    string              `json:"to,omitempty"`
	GroupName string              `json:"group,omitempty"`
	ThreadID  string              `json:"thread_id,omitempty"`
	Reactions map[string][]string `json:"reactions,omitempty"`
	// Parts contains public text parts or the full assistant-ui parts for a
	// private reply. Plain user and system messages leave it empty.
	Parts json.RawMessage `json:"parts,omitempty"`
}

// StoredRoom is the room model returned by the store.
type StoredRoom struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// NotificationPref is the per-conversation notification preference record.
type NotificationPref struct {
	Key         string `json:"key"`
	MutedUntil  int64  `json:"muted_until"`
	ShowPreview bool   `json:"show_preview"`
}

// UserProfile stores user profile data.
type UserProfile struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	Bio         string `json:"bio"`
	Status      string `json:"status"`
	LastSeen    int64  `json:"last_seen"`
}

// Poll represents a poll.
type Poll struct {
	ID             string           `json:"id"`
	RoomID         string           `json:"room_id"`
	Creator        string           `json:"creator"`
	Question       string           `json:"question"`
	Options        []string         `json:"options"`
	MultipleChoice bool             `json:"multiple_choice"`
	IsAnonymous    bool             `json:"is_anonymous"`
	IsClosed       bool             `json:"is_closed"`
	Votes          map[int]int      `json:"votes"`
	Voters         map[int][]string `json:"voters"`
	CreatedAt      int64            `json:"created_at"`
}

// CustomEmoji represents a custom emoji uploaded by a user.
type CustomEmoji struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Uploader  string `json:"uploader"`
	RoomID    string `json:"room_id"`
	CreatedAt int64  `json:"created_at"`
}

// User represents a registered user account.
type User struct {
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	InvitedBy    string `json:"invited_by"`
	CreatedAt    int64  `json:"created_at"`
}

// InviteCodeRecord represents an invitation code for new user registration.
type InviteCodeRecord struct {
	Code      string `json:"code"`
	Creator   string `json:"creator"`
	MaxUses   int    `json:"max_uses"`
	UseCount  int    `json:"use_count"`
	CreatedAt int64  `json:"created_at"`
}

// Store handles SQLite message persistence.
type Store struct {
	db            *sql.DB
	mu            sync.RWMutex
	totalMessages atomic.Int64
}

// New creates a new Store and initializes the database.
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, err
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}

	log.Println("store: database initialized")

	if err := s.seedWelcomeMessages(); err != nil {
		log.Printf("store: seed welcome messages: %v", err)
		// Non-fatal: continue with empty DB.
	}

	return s, nil
}

// seedWelcomeMessages inserts a few friendly messages into the public room
// when the database is brand new, so the first visitor sees content.
// Set CHAT_SKIP_SEED=true to skip (used in tests).
func (s *Store) seedWelcomeMessages() error {
	if os.Getenv("CHAT_SKIP_SEED") == "true" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	// Welcome rows belong to the same public room the WebSocket history query
	// uses. Leaving room_id empty made a fresh deployment look empty even
	// though the seed rows existed.
	var defaultRoomID string
	if err := s.db.QueryRow("SELECT id FROM rooms WHERE name = ?", "公共聊天").Scan(&defaultRoomID); err != nil {
		return err
	}

	now := time.Now().UnixMilli()
	seeds := []struct {
		id      string
		content string
		offset  int64
	}{
		{"welcome-1", "👋 欢迎来到 TokenDanceChat！这是 TokenDance 的实时公共聊天室。", 0},
		{"welcome-2", "你可以在这里体验 Markdown 渲染、表情反应、全文搜索、消息编辑与删除，以及 @TokenBot AI 助手。", 1000},
		{"welcome-3", "试试在输入框按 ↑ 键编辑上一条消息。", 2000},
		{"welcome-4", "在输入框 @TokenBot 可以召唤 AI 助手，试试和它聊聊吧。", 3000},
	}

	for _, seed := range seeds {
		ts := now + seed.offset
		if _, err := s.db.Exec(
			"INSERT INTO messages (id, username, content, timestamp, reply_to_id, room_id, to_user, thread_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			seed.id, "TokenBot", seed.content, ts, "", defaultRoomID, "", "",
		); err != nil {
			return err
		}
	}

	s.totalMessages.Add(int64(len(seeds)))
	log.Printf("store: seeded %d welcome messages", len(seeds))
	return nil
}

func (s *Store) migrate() error {
	query := `
			CREATE TABLE IF NOT EXISTS rooms (
				id TEXT PRIMARY KEY,
				name TEXT UNIQUE NOT NULL
			);
			CREATE TABLE IF NOT EXISTS messages (
				id TEXT PRIMARY KEY,
				username TEXT NOT NULL,
				content TEXT NOT NULL,
				timestamp INTEGER NOT NULL,
				reply_to_id TEXT DEFAULT '',
				room_id TEXT DEFAULT '',
				deleted INTEGER NOT NULL DEFAULT 0,
				edited INTEGER NOT NULL DEFAULT 0,
				to_user TEXT DEFAULT '',
				thread_id TEXT DEFAULT '',
				parts TEXT DEFAULT ''
			);
			CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp DESC);
			CREATE INDEX IF NOT EXISTS idx_messages_room ON messages(room_id, timestamp DESC);
			CREATE TABLE IF NOT EXISTS reactions (
				message_id TEXT NOT NULL,
				emoji TEXT NOT NULL,
				username TEXT NOT NULL,
				PRIMARY KEY (message_id, emoji, username),
				FOREIGN KEY (message_id) REFERENCES messages(id)
			);
			CREATE INDEX IF NOT EXISTS idx_reactions_message ON reactions(message_id);

				CREATE TABLE IF NOT EXISTS blocked_users (
					username TEXT NOT NULL,
					blocked TEXT NOT NULL,
					PRIMARY KEY (username, blocked)
				);

				CREATE TABLE IF NOT EXISTS pinned_messages (
					room_id TEXT NOT NULL DEFAULT "",
					message_id TEXT NOT NULL,
					pinned_by TEXT NOT NULL,
					pinned_at INTEGER NOT NULL,
					PRIMARY KEY (room_id, message_id)
				);

				CREATE TABLE IF NOT EXISTS notification_prefs (
					username TEXT NOT NULL,
					key TEXT NOT NULL,
					muted_until INTEGER DEFAULT 0,
					show_preview INTEGER DEFAULT 1,
					PRIMARY KEY (username, key)
				);

				CREATE TABLE IF NOT EXISTS user_profiles (
					username TEXT PRIMARY KEY,
					display_name TEXT DEFAULT '',
					avatar_url TEXT DEFAULT '',
					bio TEXT DEFAULT '',
					status TEXT DEFAULT '',
					last_seen INTEGER DEFAULT 0
				);

				CREATE TABLE IF NOT EXISTS polls (
					id TEXT PRIMARY KEY,
					room_id TEXT NOT NULL DEFAULT 'public',
					creator TEXT NOT NULL,
					question TEXT NOT NULL,
					options TEXT NOT NULL,
					multiple_choice INTEGER DEFAULT 0,
					is_anonymous INTEGER DEFAULT 0,
					is_closed INTEGER DEFAULT 0,
					votes TEXT DEFAULT '{}',
					voters TEXT DEFAULT '{}',
					created_at INTEGER NOT NULL
				);

				CREATE TABLE IF NOT EXISTS custom_emojis (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL UNIQUE,
					url TEXT NOT NULL,
					uploader TEXT NOT NULL,
					room_id TEXT DEFAULT '',
					created_at INTEGER NOT NULL
				);
				CREATE INDEX IF NOT EXISTS idx_custom_emojis_name ON custom_emojis(name);

				CREATE TABLE IF NOT EXISTS users (
					username TEXT PRIMARY KEY,
					password_hash TEXT NOT NULL,
					invited_by TEXT DEFAULT '',
					created_at INTEGER NOT NULL
				);

				CREATE TABLE IF NOT EXISTS invite_codes (
					code TEXT PRIMARY KEY,
					creator TEXT NOT NULL,
					max_uses INTEGER DEFAULT 5,
					use_count INTEGER DEFAULT 0,
					created_at INTEGER NOT NULL
				);
	`
	_, err := s.db.Exec(query)
	if err != nil {
		return err
	}

	// Add indexes for DM/thread queries.
	if _, err := s.db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_to_user ON messages(to_user, timestamp DESC)"); err != nil {
		log.Printf("store: idx_messages_to_user: %v", err)
	}
	if _, err := s.db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_thread ON messages(thread_id, timestamp)"); err != nil {
		log.Printf("store: idx_messages_thread: %v", err)
	}

	// Migration: assistant-ui parts for messages created before the column
	// existed. SQLite has no ADD COLUMN IF NOT EXISTS, so probe first.
	if !s.hasColumn("messages", "parts") {
		if _, err := s.db.Exec("ALTER TABLE messages ADD COLUMN parts TEXT DEFAULT ''"); err != nil {
			return fmt.Errorf("store: add messages.parts: %w", err)
		}
	}

	// Seed default room if not present.
	s.ensureDefaultRoom()

	// FTS5 full-text search
	if err := s.createFTS5(); err != nil {
		return err
	}

	// Populate FTS5 index for pre-existing messages not yet indexed.
	s.populateFTS5()

	// Migration: oidc_users table for OIDC-authenticated users.
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS oidc_users (
		sub TEXT PRIMARY KEY,
		chat_username TEXT UNIQUE NOT NULL,
		email TEXT DEFAULT '',
		preferred_username TEXT DEFAULT '',
		name TEXT DEFAULT '',
		picture TEXT DEFAULT '',
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`); err != nil {
		log.Printf("store: migrate create oidc_users: %v", err)
	}
	for column, definition := range map[string]string{
		"name":    "TEXT DEFAULT ''",
		"picture": "TEXT DEFAULT ''",
	} {
		if !s.hasColumn("oidc_users", column) {
			if _, err := s.db.Exec("ALTER TABLE oidc_users ADD COLUMN " + column + " " + definition); err != nil {
				log.Printf("store: add oidc_users.%s: %v", column, err)
			}
		}
	}
	if _, err := s.db.Exec("CREATE INDEX IF NOT EXISTS idx_oidc_users_chat_username ON oidc_users(chat_username)"); err != nil {
		log.Printf("store: idx_oidc_users_chat_username: %v", err)
	}

	// OIDC back-channel logout is subject-scoped. The generation is persisted
	// so a stale Chat cookie cannot become valid again after a restart, while
	// the JTI ledger makes Account's retries idempotent.
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS oidc_logout_epochs (
		sub TEXT PRIMARY KEY,
		generation INTEGER NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("store: create oidc_logout_epochs: %w", err)
	}
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS oidc_backchannel_logout_jtis (
		jti TEXT PRIMARY KEY,
		claims_sha256 TEXT NOT NULL,
		received_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("store: create oidc_backchannel_logout_jtis: %w", err)
	}
	if _, err := s.db.Exec("CREATE INDEX IF NOT EXISTS idx_oidc_logout_jtis_expires ON oidc_backchannel_logout_jtis(expires_at)"); err != nil {
		log.Printf("store: idx_oidc_logout_jtis_expires: %v", err)
	}

	return s.migrateBotRuns()
}

func (s *Store) ensureDefaultRoom() {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM rooms WHERE name = ?", "公共聊天").Scan(&count)
	if err != nil || count == 0 {
		s.db.Exec("INSERT OR IGNORE INTO rooms (id, name) VALUES (?, ?)", uuid.New().String(), "公共聊天")
	}
}

// messageColumns is the projection shared by every message read so the scan
// shape stays consistent.
const messageColumns = "id, username, content, timestamp, reply_to_id, room_id, deleted, edited, to_user, thread_id, parts, run_id, actor_id"

// scanMessage reads one message row projected with messageColumns.
func scanMessage(row interface{ Scan(...any) error }) (StoredMessage, error) {
	var m StoredMessage
	var parts sql.NullString
	if err := row.Scan(&m.ID, &m.Username, &m.Content, &m.Timestamp, &m.ReplyToID, &m.RoomID, &m.Deleted, &m.Edited, &m.ToUser, &m.ThreadID, &parts, &m.RunID, &m.ActorID); err != nil {
		return StoredMessage{}, err
	}
	if parts.Valid && parts.String != "" {
		m.Parts = json.RawMessage(parts.String)
		if m.RunID == "" {
			m.Parts = llm.WithoutSourcesJSON(m.Parts)
		}
		if m.ToUser == "" {
			m.Parts = llm.PublicPartsJSON(m.Parts)
		}
	}
	return m, nil
}

func (s *Store) hasColumn(table, column string) bool {
	rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			return false
		}
		if name == column {
			return true
		}
	}
	return false
}

func (s *Store) InsertMessage(username, content, replyToID, roomID, toUser string, _ string, threadID string) (StoredMessage, error) {
	return s.insertMessageParts(username, content, replyToID, roomID, toUser, threadID, nil)
}

// InsertMessageWithParts stores public text parts or, for a private reply,
// the complete assistant-ui parts (text, reasoning, tool calls).
func (s *Store) InsertMessageWithParts(username, content, roomID, toUser, threadID string, parts json.RawMessage) (StoredMessage, error) {
	return s.insertMessageParts(username, content, "", roomID, toUser, threadID, parts)
}

// InsertActorMessage records a server-bound actor for all new human messages.
func (s *Store) InsertActorMessage(actorID, username, content, replyToID, roomID, toUser, threadID string) (StoredMessage, error) {
	return s.insertMessageParts(username, content, replyToID, roomID, toUser, threadID, nil, actorID)
}

func (s *Store) insertMessageParts(username, content, replyToID, roomID, toUser, threadID string, parts json.RawMessage, actorIDs ...string) (StoredMessage, error) {
	parts = llm.WithoutSourcesJSON(parts)
	actorID := ""
	if len(actorIDs) > 0 {
		actorID = actorIDs[0]
	}
	if toUser == "" {
		parts = llm.PublicPartsJSON(parts)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	id := uuid.New().String()
	ts := time.Now().UnixMilli()
	partsValue := ""
	if len(parts) > 0 {
		partsValue = string(parts)
	}

	_, err := s.db.Exec(
		"INSERT INTO messages (id, username, content, timestamp, reply_to_id, room_id, to_user, thread_id, parts, actor_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		id, username, content, ts, replyToID, roomID, toUser, threadID, partsValue, actorID,
	)
	if err != nil {
		return StoredMessage{}, err
	}

	s.totalMessages.Add(1)

	return StoredMessage{
		ID:        id,
		ActorID:   actorID,
		ToUser:    toUser,
		Username:  username,
		Content:   content,
		Timestamp: ts,
		ReplyToID: replyToID,
		RoomID:    roomID,
		ThreadID:  threadID,
		Parts:     parts,
	}, nil
}

func (s *Store) GetMessages(limit int, before int64) []StoredMessage {
	return s.GetRoomMessages("", limit, before)
}

// GetMessagesBetween returns the 1:1 private thread between two users (both
// directions, oldest-first). Used for the private TokenBot conversation. Each
// row is a DM: room_id is empty and to_user points at the recipient.
func (s *Store) GetMessagesBetween(userA, userB string, limit int) []StoredMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.Query(
		"SELECT "+messageColumns+" FROM messages WHERE ((username = ? AND to_user = ?) OR (username = ? AND to_user = ?)) AND deleted = 0 ORDER BY timestamp DESC, rowid DESC LIMIT ?",
		userA, userB, userB, userA, limit,
	)
	if err != nil {
		log.Printf("store: query messages-between error: %v", err)
		return nil
	}
	defer rows.Close()

	messages := make([]StoredMessage, 0, limit)
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			log.Printf("store: scan messages-between error: %v", err)
			continue
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		log.Printf("store: messages-between rows error: %v", err)
	}

	// Query DESC (newest first), reverse to oldest-first.
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	if len(messages) > 0 {
		messageIDs := make([]string, len(messages))
		for i, m := range messages {
			messageIDs[i] = m.ID
		}
		reactions := s.GetReactionsForMessages(messageIDs)
		for i := range messages {
			messages[i].Reactions = reactions[messages[i].ID]
		}
	}

	return messages
}

func (s *Store) GetRoomMessages(roomID string, limit int, before int64) []StoredMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}

	var rows *sql.Rows
	var err error

	if before > 0 {
		if roomID != "" {
			rows, err = s.db.Query(
				"SELECT "+messageColumns+" FROM messages WHERE room_id = ? AND timestamp < ? ORDER BY timestamp DESC, rowid DESC LIMIT ?",
				roomID, before, limit,
			)
		} else {
			rows, err = s.db.Query(
				"SELECT "+messageColumns+" FROM messages WHERE to_user = '' AND timestamp < ? ORDER BY timestamp DESC, rowid DESC LIMIT ?",
				before, limit,
			)
		}
	} else {
		if roomID != "" {
			rows, err = s.db.Query(
				"SELECT "+messageColumns+" FROM messages WHERE room_id = ? ORDER BY timestamp DESC, rowid DESC LIMIT ?",
				roomID, limit,
			)
		} else {
			rows, err = s.db.Query(
				"SELECT "+messageColumns+" FROM messages WHERE to_user = '' ORDER BY timestamp DESC, rowid DESC LIMIT ?",
				limit,
			)
		}
	}

	if err != nil {
		log.Printf("store: query error: %v", err)
		return nil
	}
	defer rows.Close()

	messages := make([]StoredMessage, 0, limit)
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			log.Printf("store: scan error: %v", err)
			continue
		}
		if m.Deleted {
			m.Content = ""
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		log.Printf("store: rows iteration error: %v", err)
	}

	// Sort: since we query DESC (newest first) and then reverse.
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	// Enrich with reactions.
	if len(messages) > 0 {
		messageIDs := make([]string, len(messages))
		for i, m := range messages {
			messageIDs[i] = m.ID
		}
		reactions := s.GetReactionsForMessages(messageIDs)
		for i := range messages {
			messages[i].Reactions = reactions[messages[i].ID]
		}
	}

	return messages
}

func (s *Store) TotalUsers() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var count int64
	s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	return count
}

func (s *Store) TotalMessages() int64 {
	return s.totalMessages.Load()
}

func (s *Store) MarkDeleted(messageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("UPDATE messages SET deleted = 1 WHERE id = ?", messageID)
	return err
}

// --- Room management ---

func (s *Store) GetRoomID(name string) (string, error) {
	var id string
	err := s.db.QueryRow("SELECT id FROM rooms WHERE name = ?", name).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) CreateRoom(name string) (string, error) {
	id := uuid.New().String()
	_, err := s.db.Exec("INSERT INTO rooms (id, name) VALUES (?, ?)", id, name)
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) ListRooms() []StoredRoom {
	rows, err := s.db.Query("SELECT id, name FROM rooms ORDER BY name")
	if err != nil {
		log.Printf("store: list rooms error: %v", err)
		return nil
	}
	defer rows.Close()

	var rooms []StoredRoom
	for rows.Next() {
		var r StoredRoom
		if err := rows.Scan(&r.ID, &r.Name); err != nil {
			continue
		}
		rooms = append(rooms, r)
	}
	if err := rows.Err(); err != nil {
		log.Printf("store: rows iteration error: %v", err)
	}
	return rooms
}

func (s *Store) DeleteRoom(roomID string) error {
	_, err := s.db.Exec("DELETE FROM rooms WHERE id = ?", roomID)
	return err
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Ping verifies the database connection is alive.
func (s *Store) Ping() error {
	return s.db.Ping()
}

// ToggleReaction adds or removes a reaction. Returns the updated reactions map.
func (s *Store) ToggleReaction(messageID, emoji, username string) (map[string][]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var count int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM reactions WHERE message_id = ? AND emoji = ? AND username = ?",
		messageID, emoji, username,
	).Scan(&count)
	if err != nil {
		return nil, err
	}

	if count > 0 {
		_, err = s.db.Exec(
			"DELETE FROM reactions WHERE message_id = ? AND emoji = ? AND username = ?",
			messageID, emoji, username,
		)
	} else {
		_, err = s.db.Exec(
			"INSERT INTO reactions (message_id, emoji, username) VALUES (?, ?, ?)",
			messageID, emoji, username,
		)
	}
	if err != nil {
		return nil, err
	}

	return s.getReactionsForMessageLocked(messageID), nil
}

func (s *Store) getReactionsForMessageLocked(messageID string) map[string][]string {
	rows, err := s.db.Query(
		"SELECT emoji, username FROM reactions WHERE message_id = ? ORDER BY rowid",
		messageID,
	)
	if err != nil {
		log.Printf("store: reaction query error: %v", err)
		return nil
	}
	defer rows.Close()

	result := make(map[string][]string)
	for rows.Next() {
		var emoji, username string
		if err := rows.Scan(&emoji, &username); err != nil {
			log.Printf("store: reaction scan error: %v", err)
			continue
		}
		result[emoji] = append(result[emoji], username)
	}
	if err := rows.Err(); err != nil {
		log.Printf("store: rows iteration error: %v", err)
	}
	return result
}

func (s *Store) GetReactionsForMessages(messageIDs []string) map[string]map[string][]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]map[string][]string)
	if len(messageIDs) == 0 {
		return result
	}

	// Build IN clause with one placeholder per ID.
	placeholders := make([]string, len(messageIDs))
	args := make([]interface{}, len(messageIDs))
	for i, mid := range messageIDs {
		placeholders[i] = "?"
		args[i] = mid
		result[mid] = make(map[string][]string)
	}

	query := "SELECT message_id, emoji, username FROM reactions WHERE message_id IN (" + strings.Join(placeholders, ",") + ") ORDER BY rowid"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		log.Printf("store: batch reaction query error: %v", err)
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var messageID, emoji, username string
		if err := rows.Scan(&messageID, &emoji, &username); err != nil {
			continue
		}
		result[messageID][emoji] = append(result[messageID][emoji], username)
	}
	if err := rows.Err(); err != nil {
		log.Printf("store: rows iteration error: %v", err)
	}
	return result
}

func (s *Store) UpdateMessage(messageID, content string) (StoredMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		"UPDATE messages SET content = ?, edited = 1 WHERE id = ?",
		content, messageID,
	)
	if err != nil {
		return StoredMessage{}, err
	}

	return s.getMessageByIDLocked(messageID)
}

func (s *Store) GetMessageByID(messageID string) (StoredMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getMessageByIDLocked(messageID)
}

func (s *Store) getMessageByIDLocked(messageID string) (StoredMessage, error) {
	m, err := scanMessage(s.db.QueryRow(
		"SELECT "+messageColumns+" FROM messages WHERE id = ?",
		messageID,
	))
	if err != nil {
		return StoredMessage{}, err
	}
	if m.Deleted {
		m.Content = ""
	}
	return m, nil
}

// --- FTS5 full-text search ---

func (s *Store) createFTS5() error {
	query := `
			CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
				content, username, room_id UNINDEXED,
				content='messages', content_rowid='rowid',
				tokenize='unicode61'
			);

			CREATE TRIGGER IF NOT EXISTS messages_fts_ai AFTER INSERT ON messages BEGIN
				INSERT INTO messages_fts(rowid, content, username, room_id)
				VALUES (new.rowid, new.content, new.username, new.room_id);
			END;

			CREATE TRIGGER IF NOT EXISTS messages_fts_ad AFTER DELETE ON messages BEGIN
				INSERT INTO messages_fts(messages_fts, rowid, content, username, room_id)
				VALUES ('delete', old.rowid, old.content, old.username, old.room_id);
			END;

			CREATE TRIGGER IF NOT EXISTS messages_fts_au AFTER UPDATE ON messages BEGIN
				INSERT INTO messages_fts(messages_fts, rowid, content, username, room_id)
				VALUES ('delete', old.rowid, old.content, old.username, old.room_id);
				INSERT INTO messages_fts(rowid, content, username, room_id)
				VALUES (new.rowid, new.content, new.username, new.room_id);
			END;
			`
	_, err := s.db.Exec(query)
	return err
}

// populateFTS5 backfills the FTS5 index for any messages not yet indexed.
func (s *Store) populateFTS5() {
	result, err := s.db.Exec(
		"INSERT OR IGNORE INTO messages_fts(rowid, content, username, room_id) SELECT rowid, content, username, room_id FROM messages",
	)
	if err != nil {
		log.Printf("store: FTS5 population warning: %v", err)
		return
	}
	if n, _ := result.RowsAffected(); n > 0 {
		log.Printf("store: FTS5 backfilled %d pre-existing messages", n)
	}
}

// SearchResult holds a single full-text search result with snippet and rank.
type SearchResult struct {
	ID        string  `json:"id"`
	Username  string  `json:"username"`
	Content   string  `json:"content"`
	Timestamp int64   `json:"timestamp"`
	Snippet   string  `json:"snippet"`
	Rank      float64 `json:"rank"`
}

// sanitizeFTS5Query strips FTS5 expression operators from user input
// to prevent syntax errors and expression injection.
func sanitizeFTS5Query(q string) string {
	var b strings.Builder
	for _, r := range q {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == ' ' || r >= 0x4e00 {
			b.WriteRune(r)
		}
	}
	cleaned := strings.TrimSpace(b.String())
	if cleaned == "" {
		return ""
	}
	fields := strings.Fields(cleaned)
	b.Reset()
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(f)
		b.WriteByte('*')
	}
	return b.String()
}

// SearchMessages performs a full-text search over public messages using FTS5.
// Private conversations are never returned: every row must have an empty
// to_user, so neither the HTTP search nor the TokenBot tool can read DMs.
func (s *Store) SearchMessages(query string, roomID string, limit int) ([]SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 20
	}

	query = sanitizeFTS5Query(query)
	if query == "" {
		return []SearchResult{}, nil
	}

	var rows *sql.Rows
	var err error

	if roomID != "" {
		rows, err = s.db.Query(`
				SELECT m.id, m.username, m.content, m.timestamp,
					snippet(messages_fts, 1, '<mark>', '</mark>', '...', 40) AS snippet,
					bm25(messages_fts) AS rank
				FROM messages_fts
				JOIN messages m ON m.rowid = messages_fts.rowid
				WHERE m.deleted = 0
					AND messages_fts MATCH ?
					AND messages_fts.room_id = ?
					AND m.to_user = ''
				ORDER BY rank LIMIT ?`, query, roomID, limit)
	} else {
		rows, err = s.db.Query(`
				SELECT m.id, m.username, m.content, m.timestamp,
					snippet(messages_fts, 1, '<mark>', '</mark>', '...', 40) AS snippet,
					bm25(messages_fts) AS rank
				FROM messages_fts
				JOIN messages m ON m.rowid = messages_fts.rowid
				WHERE m.deleted = 0
					AND messages_fts MATCH ?
					AND m.to_user = ''
				ORDER BY rank LIMIT ?`, query, limit)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]SearchResult, 0, limit)
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.ID, &r.Username, &r.Content, &r.Timestamp, &r.Snippet, &r.Rank); err != nil {
			log.Printf("store: search scan error: %v", err)
			continue
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		log.Printf("store: rows iteration error: %v", err)
	}
	if results == nil {
		results = []SearchResult{}
	}
	return results, nil
}

// SearchPrivateMessages searches one user's 1:1 thread with the bot. Both the
// user's messages and the bot's replies to that user belong to the thread;
// nothing else can match.
func (s *Store) SearchPrivateMessages(user, bot, query string, limit int) ([]SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 20
	}
	query = sanitizeFTS5Query(query)
	if query == "" {
		return []SearchResult{}, nil
	}

	rows, err := s.db.Query(`
			SELECT m.id, m.username, m.content, m.timestamp,
				snippet(messages_fts, 1, '<mark>', '</mark>', '...', 40) AS snippet,
				bm25(messages_fts) AS rank
			FROM messages_fts
			JOIN messages m ON m.rowid = messages_fts.rowid
			WHERE m.deleted = 0
				AND messages_fts MATCH ?
				AND m.room_id = ''
				AND ((m.username = ? AND m.to_user = ?) OR (m.username = ? AND m.to_user = ?))
			ORDER BY rank LIMIT ?`, query, user, bot, bot, user, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]SearchResult, 0, limit)
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.ID, &r.Username, &r.Content, &r.Timestamp, &r.Snippet, &r.Rank); err != nil {
			log.Printf("store: private search scan error: %v", err)
			continue
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		log.Printf("store: private search iteration error: %v", err)
	}
	return results, nil
}

// --- User blocking ---

func (s *Store) BlockUser(username, blocked string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("INSERT OR IGNORE INTO blocked_users (username, blocked) VALUES (?, ?)", username, blocked)
	return err
}

func (s *Store) UnblockUser(username, blocked string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM blocked_users WHERE username = ? AND blocked = ?", username, blocked)
	return err
}

func (s *Store) IsBlocked(username, blocked string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM blocked_users WHERE username = ? AND blocked = ?", username, blocked).Scan(&count); err != nil {
		log.Printf("store: IsBlocked error: %v", err)
		return false
	}
	return count > 0
}

func (s *Store) GetBlockedUsers(username string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query("SELECT blocked FROM blocked_users WHERE username = ?", username)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var blocked []string
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err == nil {
			blocked = append(blocked, b)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("store: rows iteration error: %v", err)
	}
	if blocked == nil {
		blocked = []string{}
	}
	return blocked
}

// --- Message pinning ---

func (s *Store) PinMessage(roomID, messageID, pinnedBy string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		"INSERT OR IGNORE INTO pinned_messages (room_id, message_id, pinned_by, pinned_at) VALUES (?, ?, ?, ?)",
		roomID, messageID, pinnedBy, time.Now().UnixMilli(),
	)
	return err
}

func (s *Store) UnpinMessage(roomID, messageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM pinned_messages WHERE room_id = ? AND message_id = ?", roomID, messageID)
	return err
}

func (s *Store) GetPinnedMessages(roomID string) []StoredMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(
		"SELECT m.id, m.username, m.content, m.timestamp, m.reply_to_id, m.room_id, m.deleted, m.edited, m.to_user, m.thread_id, m.parts, m.run_id, m.actor_id FROM pinned_messages p JOIN messages m ON p.message_id = m.id WHERE p.room_id = ? ORDER BY p.pinned_at DESC",
		roomID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var msgs []StoredMessage
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			continue
		}
		if m.Deleted {
			m.Content = ""
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		log.Printf("store: rows iteration error: %v", err)
	}
	if msgs == nil {
		msgs = []StoredMessage{}
	}
	return msgs
}

// --- Conversation pinning ---

// --- Conversation muting ---

// --- Conversation archiving ---

// --- Threaded replies ---

// threadFilterLocked derives the child scope from the real parent. Older
// rows may contain arbitrary thread IDs, so matching the ID alone is unsafe.
// The caller holds s.mu for the parent lookup and subsequent child query.
func (s *Store) threadFilterLocked(parentMessageID string) (string, []any) {
	parent, err := s.getMessageByIDLocked(parentMessageID)
	if err != nil || parent.Deleted {
		return "", nil
	}
	if parent.ToUser == "" {
		return "thread_id = ? AND deleted = 0 AND to_user = '' AND room_id = ?", []any{parent.ID, parent.RoomID}
	}
	if parent.RoomID != "" {
		return "", nil
	}
	return "thread_id = ? AND deleted = 0 AND room_id = '' AND ((username = ? AND to_user = ?) OR (username = ? AND to_user = ?))",
		[]any{parent.ID, parent.Username, parent.ToUser, parent.ToUser, parent.Username}
}

// GetThreadMessages returns messages in a thread, ordered by creation time ascending.
func (s *Store) GetThreadMessages(parentMessageID string) []StoredMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	filter, args := s.threadFilterLocked(parentMessageID)
	if filter == "" {
		return []StoredMessage{}
	}
	rows, err := s.db.Query(
		"SELECT "+messageColumns+" FROM messages WHERE "+filter+" ORDER BY timestamp ASC",
		args...,
	)
	if err != nil {
		log.Printf("store: GetThreadMessages query error: %v", err)
		return nil
	}
	defer rows.Close()

	messages := make([]StoredMessage, 0)
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			log.Printf("store: GetThreadMessages scan error: %v", err)
			continue
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		log.Printf("store: GetThreadMessages iteration error: %v", err)
	}

	// Enrich with reactions.
	if len(messages) > 0 {
		messageIDs := make([]string, len(messages))
		for i, m := range messages {
			messageIDs[i] = m.ID
		}
		reactions := s.GetReactionsForMessages(messageIDs)
		for i := range messages {
			messages[i].Reactions = reactions[messages[i].ID]
		}
	}

	return messages
}

// GetThreadReplyCount returns the number of replies in a thread.
func (s *Store) GetThreadReplyCount(parentMessageID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	filter, args := s.threadFilterLocked(parentMessageID)
	if filter == "" {
		return 0
	}
	var count int
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM messages WHERE "+filter,
		args...,
	).Scan(&count); err != nil {
		log.Printf("store: GetThreadReplyCount error: %v", err)
		return 0
	}
	return count
}

// --- Notification preferences ---

// SetNotificationPrefs upserts notification preferences for a (username, key) pair.
func (s *Store) SetNotificationPrefs(username, key string, mutedUntil int64, showPreview bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	showPreviewInt := 0
	if showPreview {
		showPreviewInt = 1
	}
	_, err := s.db.Exec(
		"INSERT INTO notification_prefs (username, key, muted_until, show_preview) VALUES (?, ?, ?, ?) ON CONFLICT(username, key) DO UPDATE SET muted_until = excluded.muted_until, show_preview = excluded.show_preview",
		username, key, mutedUntil, showPreviewInt,
	)
	return err
}

// GetNotificationPrefs returns the notification preferences for a (username, key) pair.
func (s *Store) GetNotificationPrefs(username, key string) (mutedUntil int64, showPreview bool, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var showPreviewInt int
	err = s.db.QueryRow(
		"SELECT muted_until, show_preview FROM notification_prefs WHERE username = ? AND key = ?",
		username, key,
	).Scan(&mutedUntil, &showPreviewInt)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return 0, true, nil
		}
		return 0, true, err
	}
	return mutedUntil, showPreviewInt != 0, nil
}

// ListNotificationPrefs returns all notification preference records for a user.
func (s *Store) ListNotificationPrefs(username string) []NotificationPref {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		"SELECT key, muted_until, show_preview FROM notification_prefs WHERE username = ? ORDER BY rowid",
		username,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var prefs []NotificationPref
	for rows.Next() {
		var p NotificationPref
		var showPreviewInt int
		if err := rows.Scan(&p.Key, &p.MutedUntil, &showPreviewInt); err != nil {
			continue
		}
		p.ShowPreview = showPreviewInt != 0
		prefs = append(prefs, p)
	}
	if err := rows.Err(); err != nil {
		log.Printf("store: ListNotificationPrefs iteration error: %v", err)
	}
	if prefs == nil {
		prefs = []NotificationPref{}
	}
	return prefs
}

// --- User profiles ---

// UpsertUserProfile inserts or updates a user profile.
func (s *Store) UpsertUserProfile(username, displayName, avatarURL, bio, status string, lastSeen int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		"INSERT INTO user_profiles (username, display_name, avatar_url, bio, status, last_seen) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(username) DO UPDATE SET display_name = excluded.display_name, avatar_url = excluded.avatar_url, bio = excluded.bio, status = excluded.status, last_seen = excluded.last_seen",
		username, displayName, avatarURL, bio, status, lastSeen,
	)
	return err
}

// GetUserProfile returns a user profile by username.
func (s *Store) GetUserProfile(username string) (*UserProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var p UserProfile
	err := s.db.QueryRow(
		"SELECT username, display_name, avatar_url, bio, status, last_seen FROM user_profiles WHERE username = ?",
		username,
	).Scan(&p.Username, &p.DisplayName, &p.AvatarURL, &p.Bio, &p.Status, &p.LastSeen)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdateUserStatus updates the custom status for a user.

// UpdateUserLastSeen updates the last seen timestamp for a user.
func (s *Store) UpdateUserLastSeen(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := time.Now().UnixMilli()
	_, err := s.db.Exec(
		"INSERT INTO user_profiles (username, last_seen) VALUES (?, ?) ON CONFLICT(username) DO UPDATE SET last_seen = excluded.last_seen",
		username, ts,
	)
	return err
}

// GetAllUserProfiles returns all user profiles.
func (s *Store) GetAllUserProfiles() ([]UserProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query("SELECT username, display_name, avatar_url, bio, status, last_seen FROM user_profiles ORDER BY username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var profiles []UserProfile
	for rows.Next() {
		var p UserProfile
		if err := rows.Scan(&p.Username, &p.DisplayName, &p.AvatarURL, &p.Bio, &p.Status, &p.LastSeen); err != nil {
			continue
		}
		profiles = append(profiles, p)
	}
	if profiles == nil {
		profiles = []UserProfile{}
	}
	return profiles, rows.Err()
}

// --- Polls ---

// CreatePoll inserts a new poll.
func (s *Store) CreatePoll(poll *Poll) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	optsJSON, _ := json.Marshal(poll.Options)
	votesJSON, _ := json.Marshal(poll.Votes)
	votersJSON, _ := json.Marshal(poll.Voters)
	_, err := s.db.Exec(
		"INSERT INTO polls (id, room_id, creator, question, options, multiple_choice, is_anonymous, is_closed, votes, voters, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		poll.ID, poll.RoomID, poll.Creator, poll.Question, string(optsJSON),
		boolToInt(poll.MultipleChoice), boolToInt(poll.IsAnonymous), 0,
		string(votesJSON), string(votersJSON), poll.CreatedAt,
	)
	return err
}

// GetPoll returns a poll by ID.
func (s *Store) GetPoll(pollID string) (*Poll, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var p Poll
	var optsJSON, votesJSON, votersJSON string
	var mc, anon, closed int
	err := s.db.QueryRow(
		"SELECT id, room_id, creator, question, options, multiple_choice, is_anonymous, is_closed, votes, voters, created_at FROM polls WHERE id = ?",
		pollID,
	).Scan(&p.ID, &p.RoomID, &p.Creator, &p.Question, &optsJSON, &mc, &anon, &closed, &votesJSON, &votersJSON, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	p.MultipleChoice = mc != 0
	p.IsAnonymous = anon != 0
	p.IsClosed = closed != 0
	json.Unmarshal([]byte(optsJSON), &p.Options)
	json.Unmarshal([]byte(votesJSON), &p.Votes)
	json.Unmarshal([]byte(votersJSON), &p.Voters)
	if p.Options == nil {
		p.Options = []string{}
	}
	if p.Votes == nil {
		p.Votes = make(map[int]int)
	}
	if p.Voters == nil {
		p.Voters = make(map[int][]string)
	}
	return &p, nil
}

// VotePoll records a vote for a poll option.
func (s *Store) VotePoll(pollID string, username string, optionIndex int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var optsJSON, votesJSON, votersJSON string
	err := s.db.QueryRow("SELECT options, votes, voters FROM polls WHERE id = ?", pollID).Scan(&optsJSON, &votesJSON, &votersJSON)
	if err != nil {
		return err
	}

	var options []string
	var votes map[int]int
	var voters map[int][]string
	json.Unmarshal([]byte(optsJSON), &options)
	json.Unmarshal([]byte(votesJSON), &votes)
	json.Unmarshal([]byte(votersJSON), &voters)
	if votes == nil {
		votes = make(map[int]int)
	}
	if voters == nil {
		voters = make(map[int][]string)
	}

	for _, v := range voters[optionIndex] {
		if v == username {
			return nil
		}
	}

	votes[optionIndex]++
	voters[optionIndex] = append(voters[optionIndex], username)

	newVotesJSON, _ := json.Marshal(votes)
	newVotersJSON, _ := json.Marshal(voters)
	_, err = s.db.Exec("UPDATE polls SET votes = ?, voters = ? WHERE id = ?", string(newVotesJSON), string(newVotersJSON), pollID)
	return err
}

// ClosePoll marks a poll as closed.
func (s *Store) ClosePoll(pollID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("UPDATE polls SET is_closed = 1 WHERE id = ?", pollID)
	return err
}

// ExportMessages returns messages for a specific conversation, ordered by timestamp ascending.
// For room export: pass roomID; limit caps the result count; 0 or negative means no limit (max 10000).
func (s *Store) ExportMessages(ctx context.Context, roomID string, toUser string, _ string, username string, limit int) ([]StoredMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 10000
	}

	var rows *sql.Rows
	var err error

	switch {
	case toUser != "" && username != "":
		rows, err = s.db.Query(
			"SELECT "+messageColumns+" FROM messages WHERE deleted = 0 AND ((username = ? AND to_user = ?) OR (username = ? AND to_user = ?)) ORDER BY timestamp ASC LIMIT ?",
			username, toUser, toUser, username, limit,
		)
	case roomID != "":
		rows, err = s.db.Query(
			"SELECT "+messageColumns+" FROM messages WHERE room_id = ? AND deleted = 0 ORDER BY timestamp ASC LIMIT ?",
			roomID, limit,
		)
	default:
		// Public chat (no to_user, no specific room_id).
		rows, err = s.db.Query(
			"SELECT "+messageColumns+" FROM messages WHERE to_user = '' AND deleted = 0 ORDER BY timestamp ASC LIMIT ?",
			limit,
		)
	}

	if err != nil {
		return nil, fmt.Errorf("store: ExportMessages query error: %w", err)
	}
	defer rows.Close()

	messages := make([]StoredMessage, 0, limit)
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			log.Printf("store: ExportMessages scan error: %v", err)
			continue
		}
		if m.Deleted {
			m.Content = ""
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: ExportMessages iteration error: %w", err)
	}

	// Enrich with reactions.
	if len(messages) > 0 {
		messageIDs := make([]string, len(messages))
		for i, m := range messages {
			messageIDs[i] = m.ID
		}
		reactions := s.GetReactionsForMessages(messageIDs)
		for i := range messages {
			messages[i].Reactions = reactions[messages[i].ID]
		}
	}

	if messages == nil {
		messages = []StoredMessage{}
	}
	return messages, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- Custom Emoji ---

// AddCustomEmoji adds a new custom emoji.
func (s *Store) AddCustomEmoji(name, url, uploader, roomID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := uuid.New().String()
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(
		"INSERT INTO custom_emojis (id, name, url, uploader, room_id, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, name, url, uploader, roomID, now,
	)
	return err
}

// ListCustomEmojis returns all custom emojis, optionally filtered by room.
func (s *Store) ListCustomEmojis(roomID string) ([]CustomEmoji, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var rows *sql.Rows
	var err error
	if roomID != "" {
		rows, err = s.db.Query(
			"SELECT id, name, url, uploader, room_id, created_at FROM custom_emojis WHERE room_id = ? OR room_id = '' ORDER BY created_at DESC",
			roomID,
		)
	} else {
		rows, err = s.db.Query(
			"SELECT id, name, url, uploader, room_id, created_at FROM custom_emojis ORDER BY created_at DESC",
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []CustomEmoji
	for rows.Next() {
		var e CustomEmoji
		if err := rows.Scan(&e.ID, &e.Name, &e.URL, &e.Uploader, &e.RoomID, &e.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// DeleteCustomEmoji removes a custom emoji by name (only the uploader can delete).
func (s *Store) DeleteCustomEmoji(name, username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		"DELETE FROM custom_emojis WHERE name = ? AND uploader = ?",
		name, username,
	)
	return err
}

// SearchCustomEmojis searches custom emojis by name prefix.
func (s *Store) SearchCustomEmojis(query string) ([]CustomEmoji, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		"SELECT id, name, url, uploader, room_id, created_at FROM custom_emojis WHERE name LIKE ? ORDER BY created_at DESC LIMIT 20",
		query+"%",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []CustomEmoji
	for rows.Next() {
		var e CustomEmoji
		if err := rows.Scan(&e.ID, &e.Name, &e.URL, &e.Uploader, &e.RoomID, &e.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// --- User registration and authentication ---

const inviteCodeChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
const inviteCodeLength = 8

// hashPassword hashes a password using bcrypt with cost 12.
func hashPassword(password string) string {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		// bcrypt only fails on password > 72 bytes or cost too high.
		// Neither happens here; fallback to a safe hash to avoid panic.
		panic("bcrypt: " + err.Error())
	}
	return string(hash)
}

// checkPassword verifies a plaintext password against a hash.
// Supports both bcrypt ($2a$) and legacy SHA-256 (hex:salt:hash) formats.
// Returns true and a new hash if the legacy format was detected but verified,
// so the caller can upgrade the stored hash to bcrypt.
func checkPassword(hash, password string) (bool, string) {
	// Bcrypt hashes start with "$2a$".
	if strings.HasPrefix(hash, "$2a$") {
		err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
		return err == nil, ""
	}

	// Legacy SHA-256 format: "hexSalt:hexHash".
	parts := strings.SplitN(hash, ":", 2)
	if len(parts) != 2 {
		return false, ""
	}
	salt, err := hex.DecodeString(parts[0])
	if err != nil {
		return false, ""
	}
	h := sha256.Sum256(append(salt, []byte(password)...))
	if hex.EncodeToString(h[:]) != parts[1] {
		return false, ""
	}

	// Password matches legacy hash — upgrade to bcrypt.
	newHash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return true, "" // verified but upgrade failed; not fatal
	}
	return true, string(newHash)
}

// RegisterUser validates the invite code, hashes the password, creates the user,
// and increments the invite code use count. All operations are transactional.
func (s *Store) RegisterUser(username, passwordHash, inviteCode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Normalize invite code: uppercase, trim whitespace.
	code := strings.TrimSpace(strings.ToUpper(inviteCode))
	if code == "" {
		return fmt.Errorf("invite code is required")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Check that the invite code exists and has remaining uses.
	var creator string
	var maxUses, useCount int
	err = tx.QueryRow(
		"SELECT creator, max_uses, use_count FROM invite_codes WHERE code = ?",
		code,
	).Scan(&creator, &maxUses, &useCount)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return fmt.Errorf("invalid invite code")
		}
		return err
	}
	if useCount >= maxUses {
		return fmt.Errorf("invite code has no remaining uses")
	}

	// Check that username does not already exist.
	var existing int
	err = tx.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", username).Scan(&existing)
	if err != nil {
		return err
	}
	if existing > 0 {
		return fmt.Errorf("username already registered")
	}

	// Hash password with bcrypt.
	hash := hashPassword(passwordHash)

	now := time.Now().UnixMilli()

	// Insert user.
	_, err = tx.Exec(
		"INSERT INTO users (username, password_hash, invited_by, created_at) VALUES (?, ?, ?, ?)",
		username, hash, creator, now,
	)
	if err != nil {
		return err
	}

	// Increment use count on invite code.
	_, err = tx.Exec("UPDATE invite_codes SET use_count = use_count + 1 WHERE code = ?", code)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// VerifyUser checks the username and password against stored credentials.
func (s *Store) VerifyUser(username, password string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var passwordHash string
	err := s.db.QueryRow(
		"SELECT password_hash FROM users WHERE username = ?",
		username,
	).Scan(&passwordHash)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			// Spend the same bcrypt time as a real check so an unknown
			// username is not distinguishable by response latency.
			_ = bcrypt.CompareHashAndPassword(dummyPasswordHash(), []byte(password))
			return false, nil
		}
		return false, err
	}

	ok, upgradeHash := checkPassword(passwordHash, password)
	if !ok {
		return false, nil
	}
	// Auto-upgrade legacy SHA-256 hash to bcrypt.
	if upgradeHash != "" {
		s.mu.RUnlock()
		s.mu.Lock()
		_, _ = s.db.Exec("UPDATE users SET password_hash = ? WHERE username = ?", upgradeHash, username)
		s.mu.Unlock()
		s.mu.RLock()
	}
	return true, nil
}

var (
	dummyHashOnce sync.Once
	dummyHash     []byte
)

func dummyPasswordHash() []byte {
	dummyHashOnce.Do(func() {
		dummyHash, _ = bcrypt.GenerateFromPassword([]byte("tokenbot-timing-equaliser"), 12)
	})
	return dummyHash
}

// UserExists reports whether a local registered user owns the username.
func (s *Store) UserExists(username string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM users WHERE username = ?",
		username,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GenerateInviteCode creates a new random invite code for the given creator.
func (s *Store) GenerateInviteCode(creator string, maxUses int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if maxUses <= 0 {
		maxUses = 5
	}

	// Generate unique random code.
	var code string
	for {
		code = randomCode(inviteCodeLength)
		var count int
		err := s.db.QueryRow("SELECT COUNT(*) FROM invite_codes WHERE code = ?", code).Scan(&count)
		if err != nil {
			return "", err
		}
		if count == 0 {
			break
		}
	}

	now := time.Now().UnixMilli()
	_, err := s.db.Exec(
		"INSERT INTO invite_codes (code, creator, max_uses, use_count, created_at) VALUES (?, ?, ?, 0, ?)",
		code, creator, maxUses, now,
	)
	if err != nil {
		return "", err
	}

	return code, nil
}

// ListInviteCodes returns all invite codes created by a given user.
func (s *Store) ListInviteCodes(creator string) ([]InviteCodeRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		"SELECT code, creator, max_uses, use_count, created_at FROM invite_codes WHERE creator = ? ORDER BY created_at DESC",
		creator,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var codes []InviteCodeRecord
	for rows.Next() {
		var c InviteCodeRecord
		if err := rows.Scan(&c.Code, &c.Creator, &c.MaxUses, &c.UseCount, &c.CreatedAt); err != nil {
			continue
		}
		codes = append(codes, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if codes == nil {
		codes = []InviteCodeRecord{}
	}
	return codes, nil
}

// ValidateInviteCode checks whether an invite code exists and has remaining uses.
func (s *Store) ValidateInviteCode(code string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	code = strings.TrimSpace(strings.ToUpper(code))
	if code == "" {
		return false, nil
	}

	var maxUses, useCount int
	err := s.db.QueryRow(
		"SELECT max_uses, use_count FROM invite_codes WHERE code = ?",
		code,
	).Scan(&maxUses, &useCount)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return false, nil
		}
		return false, err
	}

	return useCount < maxUses, nil
}

// randomCode generates a random alphanumeric string of the given length.
func randomCode(length int) string {
	result := make([]byte, length)
	for i := range result {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(inviteCodeChars))))
		if err != nil {
			n = big.NewInt(int64(time.Now().UnixNano() % int64(len(inviteCodeChars))))
		}
		result[i] = inviteCodeChars[n.Int64()]
	}
	return string(result)
}

// OIDCUser represents a user authenticated via TokenDance Account OIDC.
type OIDCUser struct {
	Sub               string `json:"sub"`
	ChatUsername      string `json:"chat_username"`
	Email             string `json:"email"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
	Picture           string `json:"picture"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
}

// OIDCUserProfile is the UserInfo snapshot accepted for an OIDC identity.
type OIDCUserProfile struct {
	Sub               string
	DesiredUsername   string
	Email             string
	PreferredUsername string
	Name              string
	Picture           string
}

// ErrOIDCLogoutJTIReuse means an issuer reused a JTI for different claims.
// Retries with identical claims are handled idempotently instead.
var ErrOIDCLogoutJTIReuse = errors.New("OIDC logout JTI reused with different claims")

// OIDCBackchannelLogoutResult describes the durable effect of one Logout
// Token. ChatUsername is empty when the subject has no local Chat identity.
type OIDCBackchannelLogoutResult struct {
	ChatUsername string
	Generation   int64
	Duplicate    bool
}

// OIDCLogoutGeneration returns the current subject logout generation. Missing
// subjects start at generation zero.
func (s *Store) OIDCLogoutGeneration(sub string) (int64, error) {
	sub = strings.TrimSpace(sub)
	if sub == "" {
		return 0, fmt.Errorf("OIDC subject is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	var generation int64
	err := s.db.QueryRow(
		"SELECT generation FROM oidc_logout_epochs WHERE sub = ?",
		sub,
	).Scan(&generation)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return generation, nil
}

// ApplyOIDCBackchannelLogout records a Logout Token JTI and advances the
// subject generation atomically. Identical retries return Duplicate=true
// without advancing again, allowing the caller to re-enqueue live-session
// invalidation if its first response was lost.
func (s *Store) ApplyOIDCBackchannelLogout(
	sub, jti, claimsSHA256 string,
	receivedAt, expiresAt time.Time,
) (OIDCBackchannelLogoutResult, error) {
	sub = strings.TrimSpace(sub)
	jti = strings.TrimSpace(jti)
	claimsSHA256 = strings.TrimSpace(claimsSHA256)
	if sub == "" || jti == "" || claimsSHA256 == "" {
		return OIDCBackchannelLogoutResult{}, fmt.Errorf("OIDC logout sub, jti, and claims hash are required")
	}
	if !expiresAt.After(receivedAt) {
		return OIDCBackchannelLogoutResult{}, fmt.Errorf("OIDC logout expiry must be after receipt")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return OIDCBackchannelLogoutResult{}, err
	}
	defer tx.Rollback()

	result := OIDCBackchannelLogoutResult{}
	nowMS := receivedAt.UnixMilli()
	res, err := tx.Exec(
		`INSERT INTO oidc_backchannel_logout_jtis
		 (jti, claims_sha256, received_at, expires_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(jti) DO NOTHING`,
		jti, claimsSHA256, nowMS, expiresAt.UnixMilli(),
	)
	if err != nil {
		return OIDCBackchannelLogoutResult{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return OIDCBackchannelLogoutResult{}, err
	}

	if affected == 0 {
		var existingHash string
		if err := tx.QueryRow(
			"SELECT claims_sha256 FROM oidc_backchannel_logout_jtis WHERE jti = ?",
			jti,
		).Scan(&existingHash); err != nil {
			return OIDCBackchannelLogoutResult{}, err
		}
		if existingHash != claimsSHA256 {
			return OIDCBackchannelLogoutResult{}, ErrOIDCLogoutJTIReuse
		}
		result.Duplicate = true
	} else {
		if _, err := tx.Exec(
			"DELETE FROM oidc_backchannel_logout_jtis WHERE expires_at < ?",
			nowMS,
		); err != nil {
			return OIDCBackchannelLogoutResult{}, err
		}
		if _, err := tx.Exec(
			`INSERT INTO oidc_logout_epochs (sub, generation, updated_at)
			 VALUES (?, 1, ?)
			 ON CONFLICT(sub) DO UPDATE SET
			   generation = oidc_logout_epochs.generation + 1,
			   updated_at = excluded.updated_at`,
			sub, nowMS,
		); err != nil {
			return OIDCBackchannelLogoutResult{}, err
		}
	}

	if err := tx.QueryRow(
		"SELECT generation FROM oidc_logout_epochs WHERE sub = ?",
		sub,
	).Scan(&result.Generation); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return OIDCBackchannelLogoutResult{}, err
	}
	err = tx.QueryRow(
		"SELECT chat_username FROM oidc_users WHERE sub = ?",
		sub,
	).Scan(&result.ChatUsername)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return OIDCBackchannelLogoutResult{}, err
	}
	if _, err := tx.Exec(`UPDATE bot_runs SET charged_tokens=CASE WHEN status='queued' THEN 0 ELSE charged_tokens END, status='cancelled', reason='account_logout', updated_at=? WHERE actor_id=? AND session_generation<? AND status IN ('queued','running')`, nowMS, "oidc:"+sub, result.Generation); err != nil {
		return OIDCBackchannelLogoutResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return OIDCBackchannelLogoutResult{}, err
	}
	return result, nil
}

// UpsertOIDCUser inserts or updates an OIDC user mapping.
func (s *Store) UpsertOIDCUser(profile OIDCUserProfile) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(profile.Sub) == "" {
		return "", fmt.Errorf("OIDC subject is required")
	}
	now := time.Now().UnixMilli()

	// Check if sub already exists.
	var existingUsername string
	err := s.db.QueryRow("SELECT chat_username FROM oidc_users WHERE sub = ?", profile.Sub).Scan(&existingUsername)
	if err == nil {
		tx, txErr := s.db.Begin()
		if txErr != nil {
			return "", txErr
		}
		_, err = tx.Exec(
			"UPDATE oidc_users SET email = ?, preferred_username = ?, name = ?, picture = ?, updated_at = ? WHERE sub = ?",
			profile.Email, profile.PreferredUsername, profile.Name, profile.Picture, now, profile.Sub,
		)
		if err == nil {
			err = syncOIDCProfile(tx, existingUsername, profile)
		}
		if err != nil {
			_ = tx.Rollback()
			return "", err
		}
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return existingUsername, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	base := strings.TrimSpace(profile.DesiredUsername)
	if base == "" {
		digest := sha256.Sum256([]byte(profile.Sub))
		base = "td_" + hex.EncodeToString(digest[:6])
	}
	digest := sha256.Sum256([]byte(profile.Sub))
	suffix := hex.EncodeToString(digest[:4])
	resolvedUsername := ""
	for attempt := 0; attempt < 32; attempt++ {
		candidateSuffix := ""
		if attempt > 0 {
			candidateSuffix = "_" + suffix
			if attempt > 1 {
				candidateSuffix += fmt.Sprintf("_%d", attempt)
			}
		}
		candidateBase := base
		maxBase := 20 - len(candidateSuffix)
		if maxBase < 2 {
			candidateBase = "td"
			maxBase = 20 - len(candidateSuffix)
		}
		if len(candidateBase) > maxBase {
			candidateBase = candidateBase[:maxBase]
		}
		candidateBase = strings.TrimRight(candidateBase, "_")
		if candidateBase == "" {
			candidateBase = "td"
		}
		candidate := candidateBase + candidateSuffix
		var exists int
		err := s.db.QueryRow(
			`SELECT 1 FROM oidc_users WHERE chat_username = ?
			 UNION ALL SELECT 1 FROM users WHERE username = ? LIMIT 1`,
			candidate, candidate,
		).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			resolvedUsername = candidate
			break
		}
		if err != nil {
			return "", err
		}
	}
	if resolvedUsername == "" {
		return "", fmt.Errorf("could not allocate a stable OIDC username")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(
		"INSERT INTO oidc_users (sub, chat_username, email, preferred_username, name, picture, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		profile.Sub, resolvedUsername, profile.Email, profile.PreferredUsername, profile.Name, profile.Picture, now, now,
	)
	if err == nil {
		err = syncOIDCProfile(tx, resolvedUsername, profile)
	}
	if err != nil {
		_ = tx.Rollback()
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return resolvedUsername, nil
}

func syncOIDCProfile(tx *sql.Tx, username string, profile OIDCUserProfile) error {
	displayName := strings.TrimSpace(profile.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(profile.PreferredUsername)
	}
	if displayName == "" && profile.Email != "" {
		displayName, _, _ = strings.Cut(profile.Email, "@")
	}
	_, err := tx.Exec(
		`INSERT INTO user_profiles (username, display_name, avatar_url, bio, status, last_seen)
		 VALUES (?, ?, ?, '', '', 0)
		 ON CONFLICT(username) DO UPDATE SET
		   display_name = excluded.display_name,
		   avatar_url = excluded.avatar_url`,
		username, displayName, profile.Picture,
	)
	return err
}

// GetOIDCUserBySub retrieves an OIDC user by their OIDC sub claim.
func (s *Store) GetOIDCUserBySub(sub string) (*OIDCUser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var u OIDCUser
	err := s.db.QueryRow(
		"SELECT sub, chat_username, email, preferred_username, name, picture, created_at, updated_at FROM oidc_users WHERE sub = ?",
		sub,
	).Scan(&u.Sub, &u.ChatUsername, &u.Email, &u.PreferredUsername, &u.Name, &u.Picture, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetOIDCUserByUsername retrieves an OIDC user by their chat username.
func (s *Store) GetOIDCUserByUsername(username string) (*OIDCUser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var u OIDCUser
	err := s.db.QueryRow(
		"SELECT sub, chat_username, email, preferred_username, name, picture, created_at, updated_at FROM oidc_users WHERE chat_username = ?",
		username,
	).Scan(&u.Sub, &u.ChatUsername, &u.Email, &u.PreferredUsername, &u.Name, &u.Picture, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}
