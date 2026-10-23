package storepg

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"tokendancechat/backend/llm"
	"tokendancechat/backend/store"
)

// WithBotRunPublication runs publish while the run's authorization is still
// valid. For OIDC actors it holds a shared advisory lock for the subject, which
// is the PostgreSQL equivalent of the SQLite RLock that used to serialize
// publication against ApplyOIDCBackchannelLogout.
func (s *Store) WithBotRunPublication(id string, publish func()) error {
	ctx, cancel := s.opCtx()
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var (
		status     string
		actorID    string
		expires    pgtype.Timestamptz
		generation int64
	)
	err = tx.QueryRow(ctx,
		"SELECT status, actor_id, session_expires_at, session_generation FROM bot_runs WHERE id=$1", id).
		Scan(&status, &actorID, &expires, &generation)
	if err != nil || (status != "queued" && status != "running" && status != "completed") {
		return store.ErrRunUnavailable
	}
	if expires.Valid && time.Now().Unix() >= expires.Time.Unix() {
		return store.ErrRunUnavailable
	}
	if strings.HasPrefix(actorID, "oidc:") {
		sub := strings.TrimPrefix(actorID, "oidc:")
		if _, err := tx.Exec(ctx,
			"SELECT pg_advisory_xact_lock_shared(hashtextextended($1, 0))", "oidc-logout:"+sub); err != nil {
			return err
		}
		var current int64
		err := tx.QueryRow(ctx,
			"SELECT generation FROM oidc_logout_epochs WHERE sub=$1", sub).Scan(&current)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return store.ErrRunUnavailable
		}
		if current != generation {
			return store.ErrRunUnavailable
		}
	}
	publish()
	return tx.Commit(ctx)
}

// SnapshotBotHistory freezes the model input at the trigger message using the
// seq upper bound that replaces SQLite rowid.
func (s *Store) SnapshotBotHistory(triggerID, roomID, user, bot, threadID string, private bool, limit int) ([]store.StoredMessage, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	var upper int64
	if err := s.pool.QueryRow(ctx, "SELECT seq FROM messages WHERE id=$1", triggerID).Scan(&upper); err != nil {
		return nil, err
	}
	var (
		filter string
		args   []any
	)
	if private {
		filter = "room_id='' AND ((username=$1 AND to_user=$2) OR (username=$3 AND to_user=$4)) AND thread_id=$5"
		args = []any{user, bot, bot, user, threadID}
	} else {
		filter = "room_id=$1 AND to_user=''"
		args = []any{roomID}
	}
	upperPlaceholder := fmt.Sprintf("$%d", len(args)+1)
	limitPlaceholder := fmt.Sprintf("$%d", len(args)+2)
	args = append(args, upper, limit)
	rows, err := s.pool.Query(ctx, "SELECT "+messageColumns+
		" FROM messages WHERE "+filter+" AND seq<="+upperPlaceholder+
		" AND deleted=false ORDER BY seq DESC LIMIT "+limitPlaceholder, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.StoredMessage
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// CreateBotRun reserves budget and persists the queued run in one transaction.
// An advisory lock keyed by budget day replaces the SQLite process-wide mutex
// so concurrent admissions cannot double-spend the daily quota.
func (s *Store) CreateBotRun(r store.BotRun, limits store.RunBudgetLimits) (string, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	if err := lockBudgetLedger(ctx, tx); err != nil {
		return "", err
	}

	var existing string
	err = tx.QueryRow(ctx, "SELECT id FROM bot_runs WHERE trigger_message_id=$1", r.TriggerMessageID).Scan(&existing)
	if err == nil {
		return "duplicate", nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if !runSessionCurrent(ctx, tx, r) {
		return "auth", store.ErrRunUnavailable
	}

	var total, user, ip, tokens int64
	err = tx.QueryRow(ctx,
		`SELECT COUNT(*),
		        COALESCE(SUM(CASE WHEN actor_id=$1 THEN 1 ELSE 0 END),0),
		        COALESCE(SUM(CASE WHEN ip_key=$2 THEN 1 ELSE 0 END),0),
		        COALESCE(SUM(charged_tokens),0)
		 FROM bot_runs WHERE budget_day=$3`,
		r.ActorID, r.IPKey, r.BudgetDay).Scan(&total, &user, &ip, &tokens)
	if err != nil {
		return "", err
	}
	if limits.Replies > 0 && total >= int64(limits.Replies) || limits.Tokens > 0 && (r.ReservedTokens > limits.Tokens-tokens) {
		return "global", nil
	}
	if limits.PerUser > 0 && user >= int64(limits.PerUser) {
		return "user", nil
	}
	if limits.PerIP > 0 && r.IPKey != "" && ip >= int64(limits.PerIP) {
		return "ip", nil
	}

	budgetDay, err := parseBudgetDay(r.BudgetDay)
	if err != nil {
		return "", err
	}
	created := time.UnixMilli(r.CreatedAt)
	if r.CreatedAt == 0 {
		created = time.Now()
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO bot_runs (id, message_id, trigger_message_id, actor_id, requested_by, room_id, to_user,
		                      bot_name, session_generation, session_expires_at, status, reason, input_json,
		                      budget_day, ip_key, reserved_tokens, charged_tokens, created_at, updated_at, thread_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'queued','',$11,$12,$13,$14,$14,$15,$15,$16)`,
		r.ID, r.MessageID, r.TriggerMessageID, r.ActorID, r.RequestedBy, r.RoomID, r.ToUser,
		r.BotName, r.SessionGeneration, secondsTime(r.SessionExpiresAt), jsonObjectValue(r.Input),
		budgetDay, r.IPKey, r.ReservedTokens, created, r.ThreadID)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, "UPDATE messages SET actor_id=$1 WHERE id=$2", r.ActorID, r.TriggerMessageID); err != nil {
		return "", err
	}
	return "", tx.Commit(ctx)
}

func parseBudgetDay(day string) (time.Time, error) {
	if day == "" {
		return time.Time{}, fmt.Errorf("budget day is required")
	}
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return time.Time{}, fmt.Errorf("budget day %q: %w", day, err)
	}
	return t, nil
}

// GetBotRun reads one run.
func (s *Store) GetBotRun(id string) (store.BotRun, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	return scanRun(s.pool.QueryRow(ctx, "SELECT "+runColumns+" FROM bot_runs WHERE id=$1", id))
}

// StartBotRun moves a queued run to running, serialized by a row lock.
func (s *Store) StartBotRun(id string) error {
	ctx, cancel := s.opCtx()
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	r, err := scanRun(tx.QueryRow(ctx,
		"SELECT "+runColumns+" FROM bot_runs WHERE id=$1 FOR UPDATE", id))
	if err != nil {
		return err
	}
	if r.Status != "queued" || !runSessionCurrent(ctx, tx, r) {
		return store.ErrRunUnavailable
	}
	if _, err := tx.Exec(ctx,
		"UPDATE bot_runs SET status='running', updated_at=$1 WHERE id=$2", time.Now(), id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// FinishBotRun is the terminal compare-and-swap for cancelled/failed runs.
func (s *Store) FinishBotRun(id, status, reason string, tokens int64, known bool) error {
	if status != "cancelled" && status != "failed" {
		return fmt.Errorf("invalid terminal status")
	}
	ctx, cancel := s.opCtx()
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := lockBudgetLedger(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE bot_runs SET status=$1, reason=$2, updated_at=$3,
		 charged_tokens = CASE WHEN $4::boolean THEN $5::bigint ELSE 0::bigint END
		 WHERE id=$6 AND status IN ('queued','running')`,
		status, reason, time.Now(), known, tokens, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CompleteBotRun commits the single final message and settles usage.
func (s *Store) CompleteBotRun(id, content string, parts json.RawMessage, tokens int64, known bool) (store.StoredMessage, bool, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.StoredMessage{}, false, err
	}
	defer tx.Rollback(ctx)

	if err := lockBudgetLedger(ctx, tx); err != nil {
		return store.StoredMessage{}, false, err
	}
	r, err := scanRun(tx.QueryRow(ctx,
		"SELECT "+runColumns+" FROM bot_runs WHERE id=$1 FOR UPDATE", id))
	if err != nil {
		return store.StoredMessage{}, false, err
	}
	if r.Status == "completed" {
		m, err := scanMessage(tx.QueryRow(ctx,
			"SELECT "+messageColumns+" FROM messages WHERE run_id=$1", id))
		return m, false, err
	}
	if r.Status != "running" || !runSessionCurrent(ctx, tx, r) {
		return store.StoredMessage{}, false, store.ErrRunUnavailable
	}
	content, parts, err = finalWebParts(ctx, tx, r.ID, content, parts)
	if err != nil {
		return store.StoredMessage{}, false, err
	}
	if r.ToUser == "" {
		if r.RoomID == "" {
			return store.StoredMessage{}, false, store.ErrRunUnavailable
		}
		parts = llm.PublicPartsJSON(parts)
	}
	now := time.Now()
	charged := r.ReservedTokens
	if known && tokens >= 0 {
		charged = tokens
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO messages (id, username, content, created_at, reply_to_id, room_id, to_user, parts, run_id, actor_id, thread_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		r.MessageID, r.BotName, content, now, r.TriggerMessageID, r.RoomID, r.ToUser,
		jsonValue(parts), r.ID, botActorID(r.BotName), r.ThreadID); err != nil {
		return store.StoredMessage{}, false, err
	}
	if _, err := tx.Exec(ctx,
		"UPDATE bot_runs SET status='completed', updated_at=$1, charged_tokens=$2 WHERE id=$3",
		now, charged, id); err != nil {
		return store.StoredMessage{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return store.StoredMessage{}, false, err
	}
	return store.StoredMessage{
		ID: r.MessageID, RunID: r.ID, ActorID: botActorID(r.BotName), Username: r.BotName,
		Content: content, Timestamp: now.UnixMilli(), ReplyToID: r.TriggerMessageID,
		RoomID: r.RoomID, ToUser: r.ToUser, ThreadID: r.ThreadID, Parts: parts,
	}, true, nil
}

// CompleteBotRunSplit commits a final burst atomically.
func (s *Store) CompleteBotRunSplit(id string, contents []string, parts json.RawMessage, tokens int64, known bool) ([]store.StoredMessage, bool, error) {
	if len(contents) <= 1 {
		single := ""
		if len(contents) == 1 {
			single = contents[0]
		}
		m, created, err := s.CompleteBotRun(id, single, parts, tokens, known)
		return []store.StoredMessage{m}, created, err
	}
	ctx, cancel := s.opCtx()
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)

	if err := lockBudgetLedger(ctx, tx); err != nil {
		return nil, false, err
	}
	r, err := scanRun(tx.QueryRow(ctx,
		"SELECT "+runColumns+" FROM bot_runs WHERE id=$1 FOR UPDATE", id))
	if err != nil {
		return nil, false, err
	}
	if r.Status == "completed" {
		m, err := scanMessage(tx.QueryRow(ctx,
			"SELECT "+messageColumns+" FROM messages WHERE run_id=$1", id))
		if err != nil {
			return nil, false, err
		}
		return []store.StoredMessage{m}, false, nil
	}
	if r.Status != "running" || !runSessionCurrent(ctx, tx, r) {
		return nil, false, store.ErrRunUnavailable
	}
	contents, partsList, err := finalWebPartsMulti(ctx, tx, r.ID, contents, parts)
	if err != nil {
		return nil, false, err
	}
	if r.ToUser == "" {
		if r.RoomID == "" {
			return nil, false, store.ErrRunUnavailable
		}
		for i := range partsList {
			partsList[i] = llm.PublicPartsJSON(partsList[i])
		}
	}
	now := time.Now()
	charged := r.ReservedTokens
	if known && tokens >= 0 {
		charged = tokens
	}
	messages := make([]store.StoredMessage, 0, len(contents))
	for i, content := range contents {
		messageID := r.MessageID
		replyTo := r.TriggerMessageID
		runLink := r.ID
		if i > 0 {
			messageID = uuid.NewString()
			replyTo = ""
			runLink = fmt.Sprintf("%s#%d", r.ID, i+1)
		}
		ts := now.Add(time.Duration(i) * time.Millisecond)
		if _, err := tx.Exec(ctx,
			`INSERT INTO messages (id, username, content, created_at, reply_to_id, room_id, to_user, parts, run_id, actor_id, thread_id)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			messageID, r.BotName, content, ts, replyTo, r.RoomID, r.ToUser,
			jsonValue(partsList[i]), runLink, botActorID(r.BotName), r.ThreadID); err != nil {
			return nil, false, err
		}
		messages = append(messages, store.StoredMessage{
			ID: messageID, RunID: r.ID, ActorID: botActorID(r.BotName), Username: r.BotName,
			Content: content, Timestamp: ts.UnixMilli(), ReplyToID: replyTo,
			RoomID: r.RoomID, ToUser: r.ToUser, ThreadID: r.ThreadID, Parts: partsList[i],
		})
	}
	if _, err := tx.Exec(ctx,
		"UPDATE bot_runs SET status='completed', updated_at=$1, charged_tokens=$2 WHERE id=$3",
		now, charged, id); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return messages, true, nil
}

// InterruptBotRuns marks in-flight runs failed after a restart.
func (s *Store) InterruptBotRuns() error {
	ctx, cancel := s.opCtx()
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := lockBudgetLedger(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE bot_runs
		 SET charged_tokens = 0,
		     status='failed', reason='interrupted', updated_at=$1
		 WHERE status IN ('queued','running')`, time.Now()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RecentBotRuns lists visible run state for reconnect catch-up.
func (s *Store) RecentBotRuns(roomID, actorID string, limit int) ([]store.BotRun, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	rows, err := s.pool.Query(ctx,
		`SELECT `+runColumns+` FROM bot_runs
		 WHERE (room_id=$1 AND room_id<>'' AND to_user='') OR (actor_id=$2 AND to_user<>'')
		 ORDER BY CASE WHEN status IN ('queued','running') THEN 0 ELSE 1 END, created_at DESC
		 LIMIT $3`, roomID, actorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.BotRun
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
