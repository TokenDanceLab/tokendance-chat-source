package storepg

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"tokendancechat/backend/llm"
	"tokendancechat/backend/store"
)

// opTimeout bounds every hub-triggered call. The Hub's store methods have no
// context parameter, and a stuck connection must not stall a websocket client
// indefinitely.
const opTimeout = 10 * time.Second

func (s *Store) opCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), opTimeout)
}

// budgetLedgerLockKey serializes every transaction that admits or settles
// budget/usage. SQLite achieved the same with one process-wide mutex; the
// advisory lock is the PostgreSQL equivalent and keeps the accounting exact
// under concurrent admissions, completions and revocations.
const budgetLedgerLockKey = "budget-ledger"

func lockBudgetLedger(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", budgetLedgerLockKey)
	return err
}

// messageColumns is the projection shared by every message read. PostgreSQL
// replaces SQLite's rowid ordering with an explicit seq column.
const messageColumns = "id, username, content, created_at, reply_to_id, room_id, deleted, edited, to_user, thread_id, parts, run_id, actor_id"

// messageColumnsM is the qualified projection for joins that carry a room_id.
const messageColumnsM = "m.id, m.username, m.content, m.created_at, m.reply_to_id, m.room_id, m.deleted, m.edited, m.to_user, m.thread_id, m.parts, m.run_id, m.actor_id"

// scanMessage mirrors store.scanMessage: it converts timestamptz back to Unix
// milliseconds and applies the same public/private parts projection.
func scanMessage(row interface{ Scan(...any) error }) (store.StoredMessage, error) {
	var m store.StoredMessage
	var ts time.Time
	var parts []byte
	if err := row.Scan(&m.ID, &m.Username, &m.Content, &ts, &m.ReplyToID, &m.RoomID, &m.Deleted, &m.Edited, &m.ToUser, &m.ThreadID, &parts, &m.RunID, &m.ActorID); err != nil {
		return store.StoredMessage{}, err
	}
	m.Timestamp = ts.UnixMilli()
	if len(parts) > 0 {
		m.Parts = json.RawMessage(parts)
		if m.RunID == "" {
			m.Parts = llm.WithoutSourcesJSON(m.Parts)
		}
		if m.ToUser == "" {
			m.Parts = llm.PublicPartsJSON(m.Parts)
		}
	}
	// A deleted row must not keep renderable parts: clients prefer parts over
	// the cleared content.
	if m.Deleted {
		m.Parts = nil
	}
	return m, nil
}

// nullableTime converts the SQLite 0 sentinel to NULL and back.
func nullableTime(ms int64) any {
	if ms <= 0 {
		return nil
	}
	return time.UnixMilli(ms)
}

// secondsTime converts a Unix-seconds value (session expiries use seconds,
// unlike the millisecond timestamps elsewhere in the schema).
func secondsTime(sec int64) any {
	if sec <= 0 {
		return nil
	}
	return time.Unix(sec, 0)
}

func unixMilliOrZero(t pgtype.Timestamptz) int64 {
	if !t.Valid {
		return 0
	}
	return t.Time.UnixMilli()
}

func unixSecondsOrZero(t pgtype.Timestamptz) int64 {
	if !t.Valid {
		return 0
	}
	return t.Time.Unix()
}

// jsonValue returns NULL for empty payloads; jsonb rejects the empty string
// that SQLite stored for "no parts".
func jsonValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}

// jsonObjectValue is the NOT NULL variant used by bot_runs.input_json and the
// poll documents.
func jsonObjectValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return "{}"
	}
	return string(raw)
}

const runColumns = "id, message_id, trigger_message_id, actor_id, requested_by, room_id, to_user, bot_name, session_generation, session_expires_at, status, reason, input_json, budget_day, ip_key, reserved_tokens, charged_tokens, created_at, updated_at, thread_id"

func scanRun(row interface{ Scan(...any) error }) (store.BotRun, error) {
	var r store.BotRun
	var expires pgtype.Timestamptz
	var input []byte
	var budgetDay time.Time
	var created, updated time.Time
	err := row.Scan(
		&r.ID, &r.MessageID, &r.TriggerMessageID, &r.ActorID, &r.RequestedBy,
		&r.RoomID, &r.ToUser, &r.BotName, &r.SessionGeneration, &expires,
		&r.Status, &r.Reason, &input, &budgetDay, &r.IPKey, &r.ReservedTokens,
		&r.ChargedTokens, &created, &updated, &r.ThreadID,
	)
	if err != nil {
		return store.BotRun{}, err
	}
	r.SessionExpiresAt = unixSecondsOrZero(expires)
	r.Input = json.RawMessage(input)
	r.BudgetDay = budgetDay.Format("2006-01-02")
	r.CreatedAt = created.UnixMilli()
	r.UpdatedAt = updated.UnixMilli()
	return r, nil
}

// botActorID derives the bot's stable accounting identity from its configured
// name, matching store.botActorID.
func botActorID(botName string) string {
	return "bot:" + strings.ToLower(botName)
}

// runSessionCurrent is the PostgreSQL counterpart of store.runSessionCurrent.
func runSessionCurrent(ctx context.Context, tx pgx.Tx, r store.BotRun) bool {
	if r.SessionExpiresAt > 0 && time.Now().Unix() >= r.SessionExpiresAt {
		return false
	}
	if !strings.HasPrefix(r.ActorID, "oidc:") {
		return true
	}
	var generation int64
	err := tx.QueryRow(ctx, "SELECT generation FROM oidc_logout_epochs WHERE sub=$1", strings.TrimPrefix(r.ActorID, "oidc:")).Scan(&generation)
	return (err == nil || errors.Is(err, pgx.ErrNoRows)) && generation == r.SessionGeneration
}

// sanitizeSearchQuery keeps the incumbent character allowlist. FTS5 syntax
// stripping is unnecessary with bound parameters, but the allowlist also bounds
// input and keeps SQLite/PG result sets comparable.
func sanitizeSearchQuery(q string) string {
	var b strings.Builder
	for _, r := range q {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == ' ' || r >= 0x4e00 {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// likeContains builds a literal substring pattern. Underscore survives the
// allowlist but is a LIKE wildcard, so it is escaped; callers use ESCAPE '\'.
func likeContains(q string) string {
	return "%" + strings.ReplaceAll(q, "_", `\_`) + "%"
}

// searchSnippet reproduces the '<mark>' snippet contract without depending on
// a full-text engine: a bounded window around the first case-insensitive hit.
func searchSnippet(content, query string) string {
	if query == "" {
		return ""
	}
	runes := []rune(content)
	lowerRunes := []rune(strings.ToLower(content))
	lowerQuery := []rune(strings.ToLower(query))
	if len(lowerQuery) == 0 {
		return ""
	}
	idx := -1
	for i := 0; i+len(lowerQuery) <= len(lowerRunes); i++ {
		match := true
		for j := range lowerQuery {
			if lowerRunes[i+j] != lowerQuery[j] {
				match = false
				break
			}
		}
		if match {
			idx = i
			break
		}
	}
	if idx < 0 {
		end := len(runes)
		if end > 80 {
			end = 80
		}
		suffix := ""
		if end < len(runes) {
			suffix = "..."
		}
		return string(runes[:end]) + suffix
	}
	start := idx - 20
	if start < 0 {
		start = 0
	}
	end := idx + len(lowerQuery) + 20
	if end > len(runes) {
		end = len(runes)
	}
	prefix, suffix := "", ""
	if start > 0 {
		prefix = "..."
	}
	if end < len(runes) {
		suffix = "..."
	}
	return prefix + string(runes[start:idx]) + "<mark>" + string(runes[idx:idx+len(lowerQuery)]) + "</mark>" + string(runes[idx+len(lowerQuery):end]) + suffix
}
