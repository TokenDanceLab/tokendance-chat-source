// Package storepg is the PostgreSQL implementation of the chat persistence
// contract (hub.Store plus the run and web extensions and the OIDC helpers the
// handler uses).
//
// The SQLite store remains the live implementation until the integration lane
// wires this package in. Behaviour is aligned method by method with
// backend/store; docs/pg-migration.md records every deliberate difference.
package storepg

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"tokendancechat/backend/hub"
	"tokendancechat/backend/llm"
	"tokendancechat/backend/store"
)

// Compile-time proof that the adapter satisfies the read/write contract the Hub
// consumes. The run and web extension interfaces in hub are unexported; their
// method sets are mirrored by the contract interface in internal/storetest.
var _ hub.Store = (*Store)(nil)

//go:embed migrations/*.sql
var migrations embed.FS

// Store owns a pgx connection pool. Unlike the SQLite store it has no
// process-wide write mutex; transactions carry their own locking.
type Store struct {
	pool *pgxpool.Pool
}

// MigrateDSN opens a short-lived pool, applies the embedded migrations and
// closes it. Production runs it with the DDL-capable migration role while the
// application pool connects as the DDL-less runtime role.
func MigrateDSN(ctx context.Context, dsn string) error {
	pg, err := Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer pg.Close()
	if err := pg.Migrate(ctx); err != nil {
		return err
	}
	// River owns its own schema; apply it with the same DDL-capable connection
	// so the runtime role never needs DDL rights.
	migrator, err := rivermigrate.New(riverpgxv5.New(pg.pool), nil)
	if err != nil {
		return fmt.Errorf("storepg: river migrator: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("storepg: river migrate: %w", err)
	}
	return nil
}

// Pool exposes the underlying pool to the embedded background job client.
// Runtime callers must not close it.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Open parses dsn, creates the pool, and verifies connectivity.
func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("storepg: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("storepg: ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

// NewFromPool wraps an existing pool. Used by tests that isolate each case in
// its own schema, and by future wiring that owns pool configuration.
func NewFromPool(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Close releases every pooled connection.
func (s *Store) Close() error {
	s.pool.Close()
	return nil
}

// Migrate applies the embedded goose migrations. It is idempotent.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("storepg: set goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("storepg: migrate: %w", err)
	}
	return nil
}

// Migrate applies pending migrations using the store's pool.
func (s *Store) Migrate(ctx context.Context) error {
	return Migrate(ctx, s.pool)
}

// MigrateWithTableName applies the embedded migrations while recording history
// in a schema-qualified table. It exists so tests can isolate each case in its
// own schema without seeing public.goose_db_version; production uses the
// default history table.
func MigrateWithTableName(ctx context.Context, pool *pgxpool.Pool, table string) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("storepg: set goose dialect: %w", err)
	}
	if table != "" {
		goose.SetTableName(table)
		defer goose.SetTableName("goose_db_version")
	}
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("storepg: migrate: %w", err)
	}
	return nil
}

// Ping verifies that the database is reachable (hub.Store).
func (s *Store) Ping() error {
	if err := s.pool.Ping(context.Background()); err != nil {
		return fmt.Errorf("storepg: ping: %w", err)
	}
	return nil
}

// --- Messages ---

// InsertMessage appends a message.
func (s *Store) InsertMessage(username, content, replyToID, roomID, toUser, _ string, threadID string) (store.StoredMessage, error) {
	return s.insertMessageParts(username, content, replyToID, roomID, toUser, threadID, nil, "")
}

// InsertMessageWithParts appends a message carrying assistant-ui parts.
func (s *Store) InsertMessageWithParts(username, content, roomID, toUser, threadID string, parts json.RawMessage) (store.StoredMessage, error) {
	return s.insertMessageParts(username, content, "", roomID, toUser, threadID, parts, "")
}

// InsertActorMessage records the authenticated principal for a human message.
func (s *Store) InsertActorMessage(actorID, username, content, replyToID, roomID, toUser, threadID string) (store.StoredMessage, error) {
	return s.insertMessageParts(username, content, replyToID, roomID, toUser, threadID, nil, actorID)
}

func (s *Store) insertMessageParts(username, content, replyToID, roomID, toUser, threadID string, parts json.RawMessage, actorID string) (store.StoredMessage, error) {
	parts = llm.WithoutSourcesJSON(parts)
	if toUser == "" {
		parts = llm.PublicPartsJSON(parts)
	}
	ctx, cancel := s.opCtx()
	defer cancel()

	id := uuid.New().String()
	ts := time.Now().UnixMilli()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO messages (id, username, content, created_at, reply_to_id, room_id, to_user, thread_id, parts, actor_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		id, username, content, time.UnixMilli(ts), replyToID, roomID, toUser, threadID, jsonValue(parts), actorID,
	)
	if err != nil {
		return store.StoredMessage{}, err
	}
	return store.StoredMessage{
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

// GetMessages returns recent public messages (room_id unset).
func (s *Store) GetMessages(limit int, before int64) []store.StoredMessage {
	return s.GetRoomMessages("", limit, before)
}

// GetMessagesBetween returns the main private lane between two actors.
func (s *Store) GetMessagesBetween(userA, userB string, limit int) []store.StoredMessage {
	return s.GetMessagesBetweenThread(userA, userB, "", limit)
}

// GetMessagesBetweenThread scopes a 1:1 conversation to one lane.
func (s *Store) GetMessagesBetweenThread(userA, userB, threadID string, limit int) []store.StoredMessage {
	ctx, cancel := s.opCtx()
	defer cancel()
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx,
		"SELECT "+messageColumns+` FROM messages
		 WHERE ((username=$1 AND to_user=$2) OR (username=$3 AND to_user=$4))
		   AND thread_id=$5 AND deleted=false
		 ORDER BY created_at DESC, seq DESC LIMIT $6`,
		userA, userB, userB, userA, threadID, limit,
	)
	if err != nil {
		log.Printf("storepg: query messages-between error: %v", err)
		return nil
	}
	defer rows.Close()

	messages := make([]store.StoredMessage, 0, limit)
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			log.Printf("storepg: scan messages-between error: %v", err)
			continue
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		log.Printf("storepg: messages-between rows error: %v", err)
	}
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	s.enrichReactions(ctx, messages)
	return messages
}

// ListPrivateThreads groups the caller's assistant lanes for the sidebar.
func (s *Store) ListPrivateThreads(user, bot string) []store.PrivateThread {
	ctx, cancel := s.opCtx()
	defer cancel()
	rows, err := s.pool.Query(ctx, `
		SELECT t.tid, t.cnt, t.last_at, (
			SELECT substr(m2.content, 1, 40) FROM messages m2
			WHERE m2.thread_id = t.tid AND m2.username = $1 AND m2.to_user = $2 AND NOT m2.deleted
			ORDER BY m2.created_at, m2.seq LIMIT 1
		)
		FROM (
			SELECT thread_id AS tid, COUNT(*) AS cnt, MAX(created_at) AS last_at
			FROM messages
			WHERE ((username = $1 AND to_user = $2) OR (username = $2 AND to_user = $1)) AND NOT deleted
			GROUP BY thread_id
		) t
		ORDER BY CASE WHEN t.tid = '' THEN 0 ELSE 1 END, t.last_at DESC`,
		user, bot,
	)
	if err != nil {
		log.Printf("storepg: list private threads error: %v", err)
		return nil
	}
	defer rows.Close()
	threads := make([]store.PrivateThread, 0, 4)
	for rows.Next() {
		var (
			t       store.PrivateThread
			cnt     int64
			lastAt  time.Time
			preview pgtype.Text
		)
		if err := rows.Scan(&t.ThreadID, &cnt, &lastAt, &preview); err != nil {
			log.Printf("storepg: scan private thread error: %v", err)
			continue
		}
		t.Count = int(cnt)
		t.LastAt = lastAt.UnixMilli()
		t.Preview = preview.String
		threads = append(threads, t)
	}
	return threads
}

// GetRoomMessages returns recent messages for one room.
func (s *Store) GetRoomMessages(roomID string, limit int, before int64) []store.StoredMessage {
	ctx, cancel := s.opCtx()
	defer cancel()
	if limit <= 0 {
		limit = 100
	}

	var (
		rows pgx.Rows
		err  error
	)
	switch {
	case before > 0 && roomID != "":
		rows, err = s.pool.Query(ctx, "SELECT "+messageColumns+
			" FROM messages WHERE room_id=$1 AND created_at < $2 ORDER BY created_at DESC, seq DESC LIMIT $3",
			roomID, time.UnixMilli(before), limit)
	case before > 0:
		rows, err = s.pool.Query(ctx, "SELECT "+messageColumns+
			" FROM messages WHERE to_user='' AND created_at < $1 ORDER BY created_at DESC, seq DESC LIMIT $2",
			time.UnixMilli(before), limit)
	case roomID != "":
		rows, err = s.pool.Query(ctx, "SELECT "+messageColumns+
			" FROM messages WHERE room_id=$1 ORDER BY created_at DESC, seq DESC LIMIT $2",
			roomID, limit)
	default:
		rows, err = s.pool.Query(ctx, "SELECT "+messageColumns+
			" FROM messages WHERE to_user='' ORDER BY created_at DESC, seq DESC LIMIT $1",
			limit)
	}
	if err != nil {
		log.Printf("storepg: query error: %v", err)
		return nil
	}
	defer rows.Close()

	messages := make([]store.StoredMessage, 0, limit)
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			log.Printf("storepg: scan error: %v", err)
			continue
		}
		if m.Deleted {
			m.Content = ""
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		log.Printf("storepg: rows iteration error: %v", err)
	}
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	s.enrichReactions(ctx, messages)
	return messages
}

// TotalUsers reports the number of local password accounts.
func (s *Store) TotalUsers() int64 {
	ctx, cancel := s.opCtx()
	defer cancel()
	var count int64
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return 0
	}
	return count
}

// TotalMessages reports the durable message count. The SQLite store tracks a
// per-process counter; PostgreSQL observes the table directly.
func (s *Store) TotalMessages() int64 {
	ctx, cancel := s.opCtx()
	defer cancel()
	var count int64
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM messages").Scan(&count); err != nil {
		return 0
	}
	return count
}

// MarkDeleted soft-deletes a message.
func (s *Store) MarkDeleted(messageID string) error {
	ctx, cancel := s.opCtx()
	defer cancel()
	_, err := s.pool.Exec(ctx, "UPDATE messages SET deleted=true WHERE id=$1", messageID)
	return err
}

// --- Rooms ---

// GetRoomID resolves a room name to its ID.
func (s *Store) GetRoomID(name string) (string, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	var id string
	if err := s.pool.QueryRow(ctx, "SELECT id FROM rooms WHERE name=$1", name).Scan(&id); err != nil {
		return "", err
	}
	return id, nil
}

// CreateRoom creates a room and returns its ID.
func (s *Store) CreateRoom(name string) (string, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	id := uuid.New().String()
	if _, err := s.pool.Exec(ctx, "INSERT INTO rooms (id, name) VALUES ($1,$2)", id, name); err != nil {
		return "", err
	}
	return id, nil
}

// ListRooms lists rooms ordered by name.
func (s *Store) ListRooms() []store.StoredRoom {
	ctx, cancel := s.opCtx()
	defer cancel()
	rows, err := s.pool.Query(ctx, "SELECT id, name FROM rooms ORDER BY name")
	if err != nil {
		log.Printf("storepg: list rooms error: %v", err)
		return nil
	}
	defer rows.Close()
	var rooms []store.StoredRoom
	for rows.Next() {
		var r store.StoredRoom
		if err := rows.Scan(&r.ID, &r.Name); err != nil {
			continue
		}
		rooms = append(rooms, r)
	}
	if err := rows.Err(); err != nil {
		log.Printf("storepg: rows iteration error: %v", err)
	}
	return rooms
}

// DeleteRoom removes a room.
func (s *Store) DeleteRoom(roomID string) error {
	ctx, cancel := s.opCtx()
	defer cancel()
	_, err := s.pool.Exec(ctx, "DELETE FROM rooms WHERE id=$1", roomID)
	return err
}

// --- Reactions ---

// ToggleReaction adds or removes a reaction and returns the updated map.
func (s *Store) ToggleReaction(messageID, emoji, username string) (map[string][]string, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Serialize toggles per message, replacing the SQLite process-wide mutex.
	var one int
	if err := tx.QueryRow(ctx, "SELECT 1 FROM messages WHERE id=$1 FOR UPDATE", messageID).Scan(&one); err != nil {
		return nil, err
	}
	var count int
	if err := tx.QueryRow(ctx,
		"SELECT COUNT(*) FROM reactions WHERE message_id=$1 AND emoji=$2 AND username=$3",
		messageID, emoji, username).Scan(&count); err != nil {
		return nil, err
	}
	if count > 0 {
		if _, err := tx.Exec(ctx,
			"DELETE FROM reactions WHERE message_id=$1 AND emoji=$2 AND username=$3",
			messageID, emoji, username); err != nil {
			return nil, err
		}
	} else {
		if _, err := tx.Exec(ctx,
			"INSERT INTO reactions (message_id, emoji, username) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING",
			messageID, emoji, username); err != nil {
			return nil, err
		}
	}
	result, err := reactionsForMessage(ctx, tx, messageID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

type querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func reactionsForMessage(ctx context.Context, q querier, messageID string) (map[string][]string, error) {
	rows, err := q.Query(ctx,
		"SELECT emoji, username FROM reactions WHERE message_id=$1 ORDER BY seq", messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string][]string)
	for rows.Next() {
		var emoji, username string
		if err := rows.Scan(&emoji, &username); err != nil {
			continue
		}
		result[emoji] = append(result[emoji], username)
	}
	return result, rows.Err()
}

// GetReactionsForMessages bulk-loads reactions.
func (s *Store) GetReactionsForMessages(messageIDs []string) map[string]map[string][]string {
	ctx, cancel := s.opCtx()
	defer cancel()
	result := make(map[string]map[string][]string)
	if len(messageIDs) == 0 {
		return result
	}
	for _, id := range messageIDs {
		result[id] = make(map[string][]string)
	}
	rows, err := s.pool.Query(ctx,
		"SELECT message_id, emoji, username FROM reactions WHERE message_id = ANY($1) ORDER BY seq",
		messageIDs)
	if err != nil {
		log.Printf("storepg: batch reaction query error: %v", err)
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
		log.Printf("storepg: rows iteration error: %v", err)
	}
	return result
}

func (s *Store) enrichReactions(ctx context.Context, messages []store.StoredMessage) {
	if len(messages) == 0 {
		return
	}
	ids := make([]string, len(messages))
	for i, m := range messages {
		ids[i] = m.ID
	}
	reactions := s.GetReactionsForMessages(ids)
	for i := range messages {
		messages[i].Reactions = reactions[messages[i].ID]
	}
}

// --- Editing ---

// UpdateMessage edits a message body.
func (s *Store) UpdateMessage(messageID, content string) (store.StoredMessage, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	if _, err := s.pool.Exec(ctx,
		"UPDATE messages SET content=$1, edited=true WHERE id=$2", content, messageID); err != nil {
		return store.StoredMessage{}, err
	}
	return s.getMessageByID(ctx, messageID)
}

// GetMessageByID fetches one message.
func (s *Store) GetMessageByID(messageID string) (store.StoredMessage, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	return s.getMessageByID(ctx, messageID)
}

func (s *Store) getMessageByID(ctx context.Context, messageID string) (store.StoredMessage, error) {
	m, err := scanMessage(s.pool.QueryRow(ctx,
		"SELECT "+messageColumns+" FROM messages WHERE id=$1", messageID))
	if err != nil {
		return store.StoredMessage{}, err
	}
	if m.Deleted {
		m.Content = ""
	}
	return m, nil
}

// --- Search ---

// SearchMessages searches public room history using pg_trgm substring matching.
func (s *Store) SearchMessages(query, roomID string, limit int) ([]store.SearchResult, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	if limit <= 0 {
		limit = 20
	}
	query = sanitizeSearchQuery(query)
	if query == "" {
		return []store.SearchResult{}, nil
	}
	sql := `SELECT id, username, content, created_at, similarity(content, $1)
		FROM messages
		WHERE deleted=false AND to_user='' AND content ILIKE $2 ESCAPE '\'`
	args := []any{query, likeContains(query)}
	if roomID != "" {
		args = append(args, roomID)
		sql += fmt.Sprintf(" AND room_id=$%d", len(args))
	}
	args = append(args, limit)
	sql += fmt.Sprintf(" ORDER BY similarity(content, $1) DESC, created_at DESC, seq DESC LIMIT $%d", len(args))
	return s.runSearch(ctx, sql, args, query)
}

// SearchPrivateMessages searches one actor's assistant thread inside one lane.
func (s *Store) SearchPrivateMessages(user, bot, threadID, query string, limit int) ([]store.SearchResult, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	if limit <= 0 {
		limit = 20
	}
	query = sanitizeSearchQuery(query)
	if query == "" {
		return []store.SearchResult{}, nil
	}
	sql := `SELECT id, username, content, created_at, similarity(content, $1)
		FROM messages
		WHERE deleted=false AND room_id='' AND thread_id=$2
		  AND ((username=$3 AND to_user=$4) OR (username=$5 AND to_user=$6))
		  AND content ILIKE $7 ESCAPE '\'
		ORDER BY similarity(content, $1) DESC, created_at DESC, seq DESC
		LIMIT $8`
	args := []any{query, threadID, user, bot, bot, user, likeContains(query), limit}
	return s.runSearch(ctx, sql, args, query)
}

func (s *Store) runSearch(ctx context.Context, sql string, args []any, query string) ([]store.SearchResult, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]store.SearchResult, 0, 20)
	for rows.Next() {
		var (
			r   store.SearchResult
			ts  time.Time
			sim float64
		)
		if err := rows.Scan(&r.ID, &r.Username, &r.Content, &ts, &sim); err != nil {
			log.Printf("storepg: search scan error: %v", err)
			continue
		}
		r.Timestamp = ts.UnixMilli()
		// Keep the incumbent "smaller rank first" contract: bm25 was negative
		// for better matches, so rank is the negated similarity.
		r.Rank = -sim
		r.Snippet = searchSnippet(r.Content, query)
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		log.Printf("storepg: search iteration error: %v", err)
	}
	return results, nil
}

// --- Pinning ---

// PinMessage pins a message in a room (idempotent).
func (s *Store) PinMessage(roomID, messageID, pinnedBy string) error {
	ctx, cancel := s.opCtx()
	defer cancel()
	_, err := s.pool.Exec(ctx,
		"INSERT INTO pinned_messages (room_id, message_id, pinned_by, pinned_at) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING",
		roomID, messageID, pinnedBy, time.Now())
	return err
}

// UnpinMessage removes a pin.
func (s *Store) UnpinMessage(roomID, messageID string) error {
	ctx, cancel := s.opCtx()
	defer cancel()
	_, err := s.pool.Exec(ctx,
		"DELETE FROM pinned_messages WHERE room_id=$1 AND message_id=$2", roomID, messageID)
	return err
}

// GetPinnedMessages lists a room's pins, newest first.
func (s *Store) GetPinnedMessages(roomID string) []store.StoredMessage {
	ctx, cancel := s.opCtx()
	defer cancel()
	rows, err := s.pool.Query(ctx,
		"SELECT "+messageColumnsM+` FROM messages m
		 JOIN pinned_messages p ON p.message_id = m.id
		 WHERE p.room_id=$1 ORDER BY p.pinned_at DESC`, roomID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	msgs := make([]store.StoredMessage, 0)
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
		log.Printf("storepg: rows iteration error: %v", err)
	}
	return msgs
}

// --- Blocking ---

// BlockUser blocks another handle (idempotent).
func (s *Store) BlockUser(username, blocked string) error {
	ctx, cancel := s.opCtx()
	defer cancel()
	_, err := s.pool.Exec(ctx,
		"INSERT INTO blocked_users (username, blocked) VALUES ($1,$2) ON CONFLICT DO NOTHING",
		username, blocked)
	return err
}

// UnblockUser removes a block.
func (s *Store) UnblockUser(username, blocked string) error {
	ctx, cancel := s.opCtx()
	defer cancel()
	_, err := s.pool.Exec(ctx,
		"DELETE FROM blocked_users WHERE username=$1 AND blocked=$2", username, blocked)
	return err
}

// IsBlocked reports whether blocked is blocked by username.
func (s *Store) IsBlocked(username, blocked string) bool {
	ctx, cancel := s.opCtx()
	defer cancel()
	var count int
	if err := s.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM blocked_users WHERE username=$1 AND blocked=$2",
		username, blocked).Scan(&count); err != nil {
		log.Printf("storepg: IsBlocked error: %v", err)
		return false
	}
	return count > 0
}

// GetBlockedUsers lists a user's blocks.
func (s *Store) GetBlockedUsers(username string) []string {
	ctx, cancel := s.opCtx()
	defer cancel()
	rows, err := s.pool.Query(ctx,
		"SELECT blocked FROM blocked_users WHERE username=$1", username)
	if err != nil {
		return nil
	}
	defer rows.Close()
	blocked := []string{}
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err == nil {
			blocked = append(blocked, b)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("storepg: rows iteration error: %v", err)
	}
	return blocked
}

// --- Threads ---

// threadFilter derives the child scope from the real parent message.
func (s *Store) threadFilter(ctx context.Context, parentMessageID string) (string, []any, bool) {
	parent, err := s.getMessageByID(ctx, parentMessageID)
	if err != nil || parent.Deleted {
		return "", nil, false
	}
	if parent.ToUser == "" {
		return "thread_id=$1 AND deleted=false AND to_user='' AND room_id=$2",
			[]any{parent.ID, parent.RoomID}, true
	}
	if parent.RoomID != "" {
		return "", nil, false
	}
	return "thread_id=$1 AND deleted=false AND room_id='' AND ((username=$2 AND to_user=$3) OR (username=$4 AND to_user=$5))",
		[]any{parent.ID, parent.Username, parent.ToUser, parent.ToUser, parent.Username}, true
}

// GetThreadMessages returns replies to a parent, oldest first.
func (s *Store) GetThreadMessages(parentMessageID string) []store.StoredMessage {
	ctx, cancel := s.opCtx()
	defer cancel()
	filter, args, ok := s.threadFilter(ctx, parentMessageID)
	if !ok {
		return []store.StoredMessage{}
	}
	rows, err := s.pool.Query(ctx,
		"SELECT "+messageColumns+" FROM messages WHERE "+filter+" ORDER BY created_at ASC, seq ASC", args...)
	if err != nil {
		log.Printf("storepg: GetThreadMessages query error: %v", err)
		return nil
	}
	defer rows.Close()
	messages := make([]store.StoredMessage, 0)
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			log.Printf("storepg: GetThreadMessages scan error: %v", err)
			continue
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		log.Printf("storepg: GetThreadMessages iteration error: %v", err)
	}
	s.enrichReactions(ctx, messages)
	return messages
}

// GetThreadReplyCount counts replies to a parent.
func (s *Store) GetThreadReplyCount(parentMessageID string) int {
	ctx, cancel := s.opCtx()
	defer cancel()
	filter, args, ok := s.threadFilter(ctx, parentMessageID)
	if !ok {
		return 0
	}
	var count int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM messages WHERE "+filter, args...).Scan(&count); err != nil {
		log.Printf("storepg: GetThreadReplyCount error: %v", err)
		return 0
	}
	return count
}
