package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"tokendancechat/backend/llm"
)

var ErrRunUnavailable = errors.New("run unavailable")

// BotRun is private persistence state. Never serialize it as a public DTO.
// ActorID is derived from the authenticated Account sub (local/guest otherwise).
type BotRun struct {
	ID, MessageID, TriggerMessageID, ActorID, RequestedBy, RoomID, ToUser, BotName string
	SessionGeneration, SessionExpiresAt                                            int64
	Status, Reason, BudgetDay, IPKey                                               string
	Input                                                                          json.RawMessage
	ReservedTokens, ChargedTokens, CreatedAt, UpdatedAt                            int64
}

type RunBudgetLimits struct {
	PerUser, PerIP, Replies int
	Tokens                  int64
}

func (s *Store) migrateBotRuns() error {
	for _, column := range []string{"run_id", "actor_id"} {
		if !s.hasColumn("messages", column) {
			if _, err := s.db.Exec("ALTER TABLE messages ADD COLUMN " + column + " TEXT NOT NULL DEFAULT ''"); err != nil {
				return err
			}
		}
	}
	_, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS messages_run_final ON messages(run_id) WHERE run_id <> '';
 CREATE TABLE IF NOT EXISTS bot_runs (
 id TEXT PRIMARY KEY, message_id TEXT NOT NULL UNIQUE, trigger_message_id TEXT NOT NULL UNIQUE,
 actor_id TEXT NOT NULL, requested_by TEXT NOT NULL, room_id TEXT NOT NULL, to_user TEXT NOT NULL,
 bot_name TEXT NOT NULL, session_generation INTEGER NOT NULL, session_expires_at INTEGER NOT NULL,
 status TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '', input_json TEXT NOT NULL,
 budget_day TEXT NOT NULL, ip_key TEXT NOT NULL, reserved_tokens INTEGER NOT NULL,
 charged_tokens INTEGER NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
 );
 CREATE INDEX IF NOT EXISTS bot_runs_budget_day ON bot_runs(budget_day);
 CREATE INDEX IF NOT EXISTS bot_runs_actor ON bot_runs(actor_id,status);`)
	if err != nil {
		return err
	}
	return s.migrateWeb()
}

// SnapshotBotHistory applies an immutable rowid upper bound, including when two
// messages have the same millisecond timestamp. The returned values are copies.
func (s *Store) SnapshotBotHistory(triggerID, roomID, user, bot string, private bool, limit int) ([]StoredMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var upper int64
	if err := s.db.QueryRow("SELECT rowid FROM messages WHERE id=?", triggerID).Scan(&upper); err != nil {
		return nil, err
	}
	filter := "room_id=? AND to_user=''"
	args := []any{roomID}
	if private {
		filter = "room_id='' AND ((username=? AND to_user=?) OR (username=? AND to_user=?))"
		args = []any{user, bot, bot, user}
	}
	args = append(args, upper, limit)
	rows, err := s.db.Query("SELECT "+messageColumns+" FROM messages WHERE "+filter+" AND rowid<=? AND deleted=0 ORDER BY rowid DESC LIMIT ?", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredMessage
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// CreateBotRun reserves the complete bounded token cost and the request count
// once, in the same transaction as the durable queued state.
func (s *Store) CreateBotRun(r BotRun, limits RunBudgetLimits) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var existing string
	err = tx.QueryRow("SELECT id FROM bot_runs WHERE trigger_message_id=?", r.TriggerMessageID).Scan(&existing)
	if err == nil {
		return "duplicate", nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if !runSessionCurrent(tx, r) {
		return "auth", ErrRunUnavailable
	}
	var total, user, ip int
	var tokens int64
	err = tx.QueryRow(`SELECT COUNT(*),COALESCE(SUM(actor_id=?),0),COALESCE(SUM(ip_key=?),0),COALESCE(SUM(charged_tokens),0) FROM bot_runs WHERE budget_day=?`, r.ActorID, r.IPKey, r.BudgetDay).Scan(&total, &user, &ip, &tokens)
	if err != nil {
		return "", err
	}
	if limits.Replies > 0 && total >= limits.Replies || limits.Tokens > 0 && (r.ReservedTokens > limits.Tokens-tokens) {
		return "global", nil
	}
	if limits.PerUser > 0 && user >= limits.PerUser {
		return "user", nil
	}
	if limits.PerIP > 0 && r.IPKey != "" && ip >= limits.PerIP {
		return "ip", nil
	}
	_, err = tx.Exec(`INSERT INTO bot_runs(id,message_id,trigger_message_id,actor_id,requested_by,room_id,to_user,bot_name,session_generation,session_expires_at,status,input_json,budget_day,ip_key,reserved_tokens,charged_tokens,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,'queued',?,?,?,?,?,?,?)`, r.ID, r.MessageID, r.TriggerMessageID, r.ActorID, r.RequestedBy, r.RoomID, r.ToUser, r.BotName, r.SessionGeneration, r.SessionExpiresAt, string(r.Input), r.BudgetDay, r.IPKey, r.ReservedTokens, r.ReservedTokens, r.CreatedAt, r.CreatedAt)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec("UPDATE messages SET actor_id=? WHERE id=?", r.ActorID, r.TriggerMessageID)
	if err != nil {
		return "", err
	}
	return "", tx.Commit()
}

const runColumns = `id,message_id,trigger_message_id,actor_id,requested_by,room_id,to_user,bot_name,session_generation,session_expires_at,status,reason,input_json,budget_day,ip_key,reserved_tokens,charged_tokens,created_at,updated_at`

func scanRun(row interface{ Scan(...any) error }) (BotRun, error) {
	var r BotRun
	var input string
	err := row.Scan(&r.ID, &r.MessageID, &r.TriggerMessageID, &r.ActorID, &r.RequestedBy, &r.RoomID, &r.ToUser, &r.BotName, &r.SessionGeneration, &r.SessionExpiresAt, &r.Status, &r.Reason, &input, &r.BudgetDay, &r.IPKey, &r.ReservedTokens, &r.ChargedTokens, &r.CreatedAt, &r.UpdatedAt)
	r.Input = json.RawMessage(input)
	return r, err
}
func (s *Store) GetBotRun(id string) (BotRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return scanRun(s.db.QueryRow("SELECT "+runColumns+" FROM bot_runs WHERE id=?", id))
}

func runSessionCurrent(tx *sql.Tx, r BotRun) bool {
	if r.SessionExpiresAt > 0 && time.Now().Unix() >= r.SessionExpiresAt {
		return false
	}
	if !strings.HasPrefix(r.ActorID, "oidc:") {
		return true
	}
	var generation int64
	err := tx.QueryRow("SELECT generation FROM oidc_logout_epochs WHERE sub=?", strings.TrimPrefix(r.ActorID, "oidc:")).Scan(&generation)
	return (err == nil || errors.Is(err, sql.ErrNoRows)) && generation == r.SessionGeneration
}

func (s *Store) StartBotRun(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := scanRun(tx.QueryRow("SELECT "+runColumns+" FROM bot_runs WHERE id=?", id))
	if err != nil {
		return err
	}
	if r.Status != "queued" || !runSessionCurrent(tx, r) {
		return ErrRunUnavailable
	}
	_, err = tx.Exec("UPDATE bot_runs SET status='running',updated_at=? WHERE id=?", time.Now().UnixMilli(), id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// FinishBotRun is a terminal compare-and-swap. Unknown/partial usage retains
// the full reservation. Queued cancellation has never contacted the model.
func (s *Store) FinishBotRun(id, status, reason string, tokens int64, known bool) error {
	if status != "cancelled" && status != "failed" {
		return fmt.Errorf("invalid terminal status")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	charged := "charged_tokens"
	if known {
		charged = "?"
	}
	args := []any{status, reason, time.Now().UnixMilli()}
	if known {
		args = append(args, tokens)
	}
	args = append(args, id)
	_, err := s.db.Exec("UPDATE bot_runs SET status=?,reason=?,updated_at=?,charged_tokens="+charged+" WHERE id=? AND status IN ('queued','running')", args...)
	return err
}

// CompleteBotRun owns the only final delivery. State, authorization, quota
// settlement and insertion share one transaction; a duplicate returns the same
// message and created=false, so retries cannot broadcast a second delivery.
func (s *Store) CompleteBotRun(id, content string, parts json.RawMessage, tokens int64, known bool) (StoredMessage, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return StoredMessage{}, false, err
	}
	defer tx.Rollback()
	r, err := scanRun(tx.QueryRow("SELECT "+runColumns+" FROM bot_runs WHERE id=?", id))
	if err != nil {
		return StoredMessage{}, false, err
	}
	if r.Status == "completed" {
		m, err := scanMessage(tx.QueryRow("SELECT "+messageColumns+" FROM messages WHERE run_id=?", id))
		return m, false, err
	}
	if r.Status != "running" || !runSessionCurrent(tx, r) {
		return StoredMessage{}, false, ErrRunUnavailable
	}
	content, parts, err = finalWebParts(tx, r.ID, content, parts)
	if err != nil {
		return StoredMessage{}, false, err
	}
	if r.ToUser == "" {
		if r.RoomID == "" {
			return StoredMessage{}, false, ErrRunUnavailable
		}
		parts = llm.PublicPartsJSON(parts)
	}
	now := time.Now().UnixMilli()
	charged := r.ReservedTokens
	if known && tokens >= 0 {
		charged = tokens
	}
	_, err = tx.Exec(`INSERT INTO messages(id,username,content,timestamp,reply_to_id,room_id,to_user,parts,run_id,actor_id) VALUES(?,?,?,?,?,?,?,?,?,?)`, r.MessageID, r.BotName, content, now, r.TriggerMessageID, r.RoomID, r.ToUser, string(parts), r.ID, "bot:tokenbot")
	if err != nil {
		return StoredMessage{}, false, err
	}
	_, err = tx.Exec("UPDATE bot_runs SET status='completed',updated_at=?,charged_tokens=? WHERE id=?", now, charged, id)
	if err != nil {
		return StoredMessage{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return StoredMessage{}, false, err
	}
	s.totalMessages.Add(1)
	return StoredMessage{ID: r.MessageID, RunID: r.ID, ActorID: "bot:tokenbot", Username: r.BotName, Content: content, Timestamp: now, ReplyToID: r.TriggerMessageID, RoomID: r.RoomID, ToUser: r.ToUser, Parts: parts}, true, nil
}

// Interrupted requests are never automatically retried after a restart. Their
// full reservation survives, because provider usage may have been lost.
func (s *Store) InterruptBotRuns() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE bot_runs SET charged_tokens=CASE WHEN status='queued' THEN 0 ELSE charged_tokens END,status='failed',reason='interrupted',updated_at=? WHERE status IN ('queued','running')`, time.Now().UnixMilli())
	return err
}

func (s *Store) RecentBotRuns(roomID, actorID string, limit int) ([]BotRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query("SELECT "+runColumns+" FROM bot_runs WHERE (room_id=? AND room_id<>'' AND to_user='') OR (actor_id=? AND to_user<>'') ORDER BY CASE WHEN status IN ('queued','running') THEN 0 ELSE 1 END, created_at DESC LIMIT ?", roomID, actorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BotRun
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// WithBotRunPublication serializes the last authorization check and outbound
// enqueue against Account's durable generation transaction. Nothing can pass
// this gate after a backchannel revocation has committed.
func (s *Store) WithBotRunPublication(id string, publish func()) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Read only the authorization columns. The previous full-row scan pulled
	// the frozen input payload (and its JSON) on every streamed token.
	var status, actorID string
	var expiresAt, generation int64
	err := s.db.QueryRow("SELECT status,actor_id,session_expires_at,session_generation FROM bot_runs WHERE id=?", id).
		Scan(&status, &actorID, &expiresAt, &generation)
	if err != nil || (status != "queued" && status != "running" && status != "completed") {
		return ErrRunUnavailable
	}
	if expiresAt > 0 && time.Now().Unix() >= expiresAt {
		return ErrRunUnavailable
	}
	if strings.HasPrefix(actorID, "oidc:") {
		var current int64
		err = s.db.QueryRow("SELECT generation FROM oidc_logout_epochs WHERE sub=?", strings.TrimPrefix(actorID, "oidc:")).Scan(&current)
		if (err != nil && !errors.Is(err, sql.ErrNoRows)) || current != generation {
			return ErrRunUnavailable
		}
	}
	publish()
	return nil
}
