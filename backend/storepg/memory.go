package storepg

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"tokendancechat/backend/store"
)

// CreateAgentMemory inserts one memory, returning the existing row when the
// same active fact already exists in that scope. A supersede retires the old
// fact in the same transaction.
func (s *Store) CreateAgentMemory(m store.AgentMemory, supersedesID string) (store.AgentMemory, bool, error) {
	content := strings.Join(strings.Fields(m.Content), " ")
	if content == "" || len([]rune(content)) > 500 {
		return store.AgentMemory{}, false, errors.New("memory content must be 1-500 characters")
	}
	if m.Scope != "lane" && m.Scope != "actor" {
		return store.AgentMemory{}, false, errors.New("unsupported memory scope")
	}
	if m.Scope == "actor" {
		m.ThreadID = ""
	}
	now := time.Now()
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	m.Content = content
	m.ContentHash = store.MemoryContentHash(content)
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
	m.UpdatedAt = now.UnixMilli()
	if m.CreatedAt == 0 {
		m.CreatedAt = now.UnixMilli()
	}
	if m.ValidFrom == 0 {
		m.ValidFrom = now.UnixMilli()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.AgentMemory{}, false, err
	}
	defer tx.Rollback(ctx)

	if supersedesID != "" {
		var owner, status string
		if err := tx.QueryRow(ctx, "SELECT owner_actor_id, status FROM agent_memories WHERE id = $1", supersedesID).Scan(&owner, &status); err != nil {
			return store.AgentMemory{}, false, err
		}
		if owner != m.OwnerActorID || status != "active" {
			return store.AgentMemory{}, false, errors.New("memory cannot be superseded by this actor")
		}
	}
	createdAt := time.UnixMilli(m.CreatedAt)
	updatedAt := time.UnixMilli(m.UpdatedAt)
	validFrom := time.UnixMilli(m.ValidFrom)
	var expiresAt *time.Time
	if m.ExpiresAt != nil {
		t := time.UnixMilli(*m.ExpiresAt)
		expiresAt = &t
	}
	tag, err := tx.Exec(ctx, `INSERT INTO agent_memories
		(id, owner_actor_id, scope, thread_id, kind, content, content_hash, confidence, importance, sensitivity, source, source_run_id, source_message_id, status, pinned, created_at, updated_at, valid_from, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'active',$14,$15,$16,$17,$18)
		ON CONFLICT DO NOTHING`,
		m.ID, m.OwnerActorID, m.Scope, m.ThreadID, m.Kind, m.Content, m.ContentHash,
		m.Confidence, m.Importance, m.Sensitivity, m.Source, m.SourceRunID, m.SourceMessageID,
		m.Pinned, createdAt, updatedAt, validFrom, expiresAt)
	if err != nil {
		return store.AgentMemory{}, false, err
	}
	if tag.RowsAffected() == 0 {
		var existing store.AgentMemory
		if err := scanPGMemory(tx.QueryRow(ctx, memorySelect+`
			WHERE owner_actor_id = $1 AND scope = $2 AND thread_id = $3 AND content_hash = $4 AND status = 'active'`,
			m.OwnerActorID, m.Scope, m.ThreadID, m.ContentHash), &existing); err != nil {
			return store.AgentMemory{}, false, err
		}
		return existing, false, nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO agent_memory_events (id, memory_id, actor_id, action, created_at) VALUES ($1,$2,$3,'created',now())`,
		uuid.NewString(), m.ID, m.OwnerActorID); err != nil {
		return store.AgentMemory{}, false, err
	}
	if supersedesID != "" {
		if _, err := tx.Exec(ctx, `UPDATE agent_memories SET status='superseded', superseded_by=$1, valid_to=now(), updated_at=now() WHERE id=$2 AND owner_actor_id=$3`,
			m.ID, supersedesID, m.OwnerActorID); err != nil {
			return store.AgentMemory{}, false, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agent_memory_events (id, memory_id, actor_id, action, detail) VALUES ($1,$2,$3,'superseded',$4)`,
			uuid.NewString(), supersedesID, m.OwnerActorID, m.ID); err != nil {
			return store.AgentMemory{}, false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return store.AgentMemory{}, false, err
	}
	return m, true, nil
}

const memorySelect = `SELECT id, owner_actor_id, scope, thread_id, kind, content, content_hash,
	confidence, importance, sensitivity, source, source_run_id, source_message_id, status,
	COALESCE(superseded_by, ''), pinned, created_at, updated_at, valid_from, valid_to, expires_at
	FROM agent_memories`

func scanPGMemory(row pgx.Row, m *store.AgentMemory) error {
	var createdAt, updatedAt, validFrom time.Time
	var validTo, expiresAt *time.Time
	if err := row.Scan(&m.ID, &m.OwnerActorID, &m.Scope, &m.ThreadID, &m.Kind, &m.Content, &m.ContentHash,
		&m.Confidence, &m.Importance, &m.Sensitivity, &m.Source, &m.SourceRunID, &m.SourceMessageID,
		&m.Status, &m.SupersededBy, &m.Pinned, &createdAt, &updatedAt, &validFrom, &validTo, &expiresAt); err != nil {
		return err
	}
	m.CreatedAt = createdAt.UnixMilli()
	m.UpdatedAt = updatedAt.UnixMilli()
	m.ValidFrom = validFrom.UnixMilli()
	if validTo != nil {
		v := validTo.UnixMilli()
		m.ValidTo = &v
	}
	if expiresAt != nil {
		v := expiresAt.UnixMilli()
		m.ExpiresAt = &v
	}
	return nil
}

// ListAgentMemories returns active actor-wide and lane memories.
func (s *Store) ListAgentMemories(actorID, threadID string, limit int) []store.AgentMemory {
	if strings.TrimSpace(actorID) == "" {
		return nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.pool.Query(ctx, memorySelect+`
		WHERE owner_actor_id = $1 AND status = 'active'
		  AND (scope = 'actor' OR (scope = 'lane' AND thread_id = $2))
		  AND (expires_at IS NULL OR expires_at > now())
		ORDER BY pinned DESC, updated_at DESC LIMIT $3`, actorID, threadID, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []store.AgentMemory
	for rows.Next() {
		var m store.AgentMemory
		if err := scanPGMemory(rows, &m); err != nil {
			continue
		}
		out = append(out, m)
	}
	return out
}

// DeleteAgentMemory hard-deletes one memory owned by the actor.
func (s *Store) DeleteAgentMemory(actorID, memoryID string) (bool, error) {
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(memoryID) == "" {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, "DELETE FROM agent_memories WHERE id = $1 AND owner_actor_id = $2", memoryID, actorID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO agent_memory_events (id, memory_id, actor_id, action) VALUES ($1,$2,$3,'deleted')`,
		uuid.NewString(), memoryID, actorID); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// PurgeGuestIdentities deletes bindings untouched since before the cutoff,
// except handles that still own private history.
func (s *Store) PurgeGuestIdentities(before time.Time) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tag, err := s.pool.Exec(ctx, `DELETE FROM guest_identities g
		WHERE g.last_seen_at < $1
		  AND NOT EXISTS (
		    SELECT 1 FROM messages m
		    WHERE m.room_id = '' AND m.to_user <> ''
		      AND (m.username = g.handle OR m.to_user = g.handle)
		  )`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ExpireAgentMemories retires memories whose expires_at has passed.
func (s *Store) ExpireAgentMemories(now time.Time) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tag, err := s.pool.Exec(ctx,
		"UPDATE agent_memories SET status = 'expired', updated_at = now() WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at <= $1",
		now,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// IsUploadReferenced reports whether any message still points at the upload.
func (s *Store) IsUploadReferenced(filename string) (bool, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return true, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var exists int
	err := s.pool.QueryRow(ctx, "SELECT 1 FROM messages WHERE content LIKE '%' || $1 || '%' LIMIT 1", filename).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
