package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AgentMemory is one durable fact a user explicitly asked the assistant to
// remember. It is small, structured data layered on top of the transcript, not
// a second copy of the conversation.
type AgentMemory struct {
	ID              string  `json:"id"`
	OwnerActorID    string  `json:"-"`
	Scope           string  `json:"scope"`
	ThreadID        string  `json:"thread_id,omitempty"`
	Kind            string  `json:"kind"`
	Content         string  `json:"content"`
	ContentHash     string  `json:"-"`
	Confidence      float64 `json:"confidence"`
	Importance      int     `json:"importance"`
	Sensitivity     int     `json:"sensitivity"`
	Source          string  `json:"source"`
	SourceRunID     string  `json:"source_run_id,omitempty"`
	SourceMessageID string  `json:"source_message_id,omitempty"`
	Status          string  `json:"status"`
	SupersededBy    string  `json:"superseded_by,omitempty"`
	Pinned          bool    `json:"pinned"`
	CreatedAt       int64   `json:"created_at"`
	UpdatedAt       int64   `json:"updated_at"`
	ValidFrom       int64   `json:"valid_from"`
	ValidTo         *int64  `json:"valid_to,omitempty"`
	ExpiresAt       *int64  `json:"expires_at,omitempty"`
}

// MemoryContentHash normalises whitespace and hashes the content. Dedupe is
// exact on this value; near-duplicate handling belongs to a later phase.
func MemoryContentHash(content string) string {
	normalised := strings.Join(strings.Fields(content), " ")
	sum := sha256.Sum256([]byte(normalised))
	return hex.EncodeToString(sum[:])
}

func (s *Store) migrateMemory() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS agent_memories (
		id TEXT PRIMARY KEY,
		owner_actor_id TEXT NOT NULL,
		scope TEXT NOT NULL,
		thread_id TEXT NOT NULL DEFAULT '',
		kind TEXT NOT NULL,
		content TEXT NOT NULL,
		content_hash TEXT NOT NULL,
		confidence REAL NOT NULL DEFAULT 0.7,
		importance INTEGER NOT NULL DEFAULT 3,
		sensitivity INTEGER NOT NULL DEFAULT 0,
		source TEXT NOT NULL,
		source_run_id TEXT NOT NULL DEFAULT '',
		source_message_id TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'active',
		superseded_by TEXT,
		pinned INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		valid_from INTEGER NOT NULL,
		valid_to INTEGER,
		expires_at INTEGER
	)`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS agent_memories_dedup
		ON agent_memories(owner_actor_id, scope, thread_id, content_hash)
		WHERE status = 'active'`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS agent_memories_owner_active
		ON agent_memories(owner_actor_id, scope, updated_at DESC)
		WHERE status = 'active'`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS agent_memory_events (
		id TEXT PRIMARY KEY,
		memory_id TEXT NOT NULL,
		actor_id TEXT NOT NULL,
		action TEXT NOT NULL,
		detail TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL
	)`); err != nil {
		return err
	}
	return nil
}

func memoryExec(tx *sql.Tx, query string, args ...any) error {
	_, err := tx.Exec(query, args...)
	return err
}

// CreateAgentMemory inserts one memory. An exact duplicate in the same scope
// returns the existing row with created=false. When supersedesID is set, the
// old row is retired in the same transaction so a correction cannot leave two
// active facts.
func (s *Store) CreateAgentMemory(m AgentMemory, supersedesID string) (AgentMemory, bool, error) {
	content := strings.Join(strings.Fields(m.Content), " ")
	if content == "" || len([]rune(content)) > 500 {
		return AgentMemory{}, false, errors.New("memory content must be 1-500 characters")
	}
	if m.Scope != "lane" && m.Scope != "actor" {
		return AgentMemory{}, false, errors.New("unsupported memory scope")
	}
	if m.Scope == "actor" {
		m.ThreadID = ""
	}
	now := time.Now().UnixMilli()
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	m.Content = content
	m.ContentHash = MemoryContentHash(content)
	if m.Status == "" {
		m.Status = "active"
	}
	if m.Source == "" {
		m.Source = "bot_tool"
	}
	if m.Confidence == 0 {
		m.Confidence = 0.7
	}
	if m.Importance == 0 {
		m.Importance = 3
	}
	if m.ValidFrom == 0 {
		m.ValidFrom = now
	}
	if m.CreatedAt == 0 {
		m.CreatedAt = now
	}
	m.UpdatedAt = now

	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return AgentMemory{}, false, err
	}
	defer tx.Rollback()

	if supersedesID != "" {
		var owner, status string
		if err := tx.QueryRow("SELECT owner_actor_id, status FROM agent_memories WHERE id = ?", supersedesID).Scan(&owner, &status); err != nil {
			return AgentMemory{}, false, err
		}
		if owner != m.OwnerActorID || status != "active" {
			return AgentMemory{}, false, errors.New("memory cannot be superseded by this actor")
		}
	}
	res, err := tx.Exec(`INSERT OR IGNORE INTO agent_memories
		(id, owner_actor_id, scope, thread_id, kind, content, content_hash, confidence, importance, sensitivity, source, source_run_id, source_message_id, status, pinned, created_at, updated_at, valid_from, valid_to, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?, ?, NULL, ?)`,
		m.ID, m.OwnerActorID, m.Scope, m.ThreadID, m.Kind, m.Content, m.ContentHash,
		m.Confidence, m.Importance, m.Sensitivity, m.Source, m.SourceRunID, m.SourceMessageID,
		boolInt(m.Pinned), m.CreatedAt, m.UpdatedAt, m.ValidFrom, m.ExpiresAt)
	if err != nil {
		return AgentMemory{}, false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return AgentMemory{}, false, err
	}
	if rows == 0 {
		var existing AgentMemory
		if err := scanMemory(tx.QueryRow(`SELECT id, owner_actor_id, scope, thread_id, kind, content, content_hash, confidence, importance, sensitivity, source, source_run_id, source_message_id, status, COALESCE(superseded_by, ''), pinned, created_at, updated_at, valid_from, valid_to, expires_at
			FROM agent_memories WHERE owner_actor_id = ? AND scope = ? AND thread_id = ? AND content_hash = ? AND status = 'active'`,
			m.OwnerActorID, m.Scope, m.ThreadID, m.ContentHash), &existing); err != nil {
			return AgentMemory{}, false, err
		}
		return existing, false, nil
	}
	if err := memoryExec(tx, `INSERT INTO agent_memory_events (id, memory_id, actor_id, action, detail, created_at) VALUES (?, ?, ?, 'created', '', ?)`,
		uuid.NewString(), m.ID, m.OwnerActorID, now); err != nil {
		return AgentMemory{}, false, err
	}
	if supersedesID != "" {
		if err := memoryExec(tx, `UPDATE agent_memories SET status = 'superseded', superseded_by = ?, valid_to = ?, updated_at = ? WHERE id = ? AND owner_actor_id = ?`,
			m.ID, now, now, supersedesID, m.OwnerActorID); err != nil {
			return AgentMemory{}, false, err
		}
		if err := memoryExec(tx, `INSERT INTO agent_memory_events (id, memory_id, actor_id, action, detail, created_at) VALUES (?, ?, ?, 'superseded', ?, ?)`,
			uuid.NewString(), supersedesID, m.OwnerActorID, m.ID, now); err != nil {
			return AgentMemory{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return AgentMemory{}, false, err
	}
	return m, true, nil
}

// ListAgentMemories returns the active facts for one actor: its actor-wide
// memories plus the ones bound to this lane. Expired rows are skipped.
func (s *Store) ListAgentMemories(actorID, threadID string, limit int) []AgentMemory {
	if strings.TrimSpace(actorID) == "" {
		return nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	now := time.Now().UnixMilli()
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(`SELECT id, owner_actor_id, scope, thread_id, kind, content, content_hash, confidence, importance, sensitivity, source, source_run_id, source_message_id, status, COALESCE(superseded_by, ''), pinned, created_at, updated_at, valid_from, valid_to, expires_at
		FROM agent_memories
		WHERE owner_actor_id = ? AND status = 'active'
		  AND (scope = 'actor' OR (scope = 'lane' AND thread_id = ?))
		  AND (expires_at IS NULL OR expires_at > ?)
		ORDER BY pinned DESC, updated_at DESC LIMIT ?`, actorID, threadID, now, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []AgentMemory
	for rows.Next() {
		var m AgentMemory
		if err := scanMemory(rows, &m); err != nil {
			continue
		}
		out = append(out, m)
	}
	return out
}

// DeleteAgentMemory hard-deletes one memory owned by the actor. The audit
// event keeps no content.
func (s *Store) DeleteAgentMemory(actorID, memoryID string) (bool, error) {
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(memoryID) == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.Exec("DELETE FROM agent_memories WHERE id = ? AND owner_actor_id = ?", memoryID, actorID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	if err := memoryExec(tx, `INSERT INTO agent_memory_events (id, memory_id, actor_id, action, detail, created_at) VALUES (?, ?, ?, 'deleted', '', ?)`,
		uuid.NewString(), memoryID, actorID, time.Now().UnixMilli()); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

type memoryScanner interface {
	Scan(dest ...any) error
}

func scanMemory(row memoryScanner, m *AgentMemory) error {
	var pinned int
	var validTo, expiresAt sql.NullInt64
	if err := row.Scan(&m.ID, &m.OwnerActorID, &m.Scope, &m.ThreadID, &m.Kind, &m.Content, &m.ContentHash,
		&m.Confidence, &m.Importance, &m.Sensitivity, &m.Source, &m.SourceRunID, &m.SourceMessageID,
		&m.Status, &m.SupersededBy, &pinned, &m.CreatedAt, &m.UpdatedAt, &m.ValidFrom, &validTo, &expiresAt); err != nil {
		return err
	}
	m.Pinned = pinned != 0
	if validTo.Valid {
		m.ValidTo = &validTo.Int64
	}
	if expiresAt.Valid {
		m.ExpiresAt = &expiresAt.Int64
	}
	return nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// PurgeGuestIdentities deletes bindings untouched since before the cutoff,
// except handles that still own private history. Releasing such a handle would
// let a new browser read it once the binding is gone.
func (s *Store) PurgeGuestIdentities(before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`DELETE FROM guest_identities
		WHERE last_seen_at < ?
		  AND NOT EXISTS (
		    SELECT 1 FROM messages m
		    WHERE m.room_id = '' AND m.to_user <> ''
		      AND (m.username = guest_identities.handle OR m.to_user = guest_identities.handle)
		  )`, before.UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ExpireAgentMemories retires memories whose expires_at has passed.
func (s *Store) ExpireAgentMemories(now time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(
		"UPDATE agent_memories SET status = 'expired', updated_at = ? WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at <= ?",
		now.UnixMilli(), now.UnixMilli(),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// IsUploadReferenced reports whether any message still points at the given
// upload filename. The maintenance sweep deletes unreferenced files older than
// the upload TTL so failed/abandoned attachments do not accumulate.
func (s *Store) IsUploadReferenced(filename string) (bool, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return true, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var exists int
	err := s.db.QueryRow(
		"SELECT 1 FROM messages WHERE content LIKE ? LIMIT 1",
		"%"+filename+"%",
	).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
