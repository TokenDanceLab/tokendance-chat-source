package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// tableSpec describes one allowlisted table: how to read it from SQLite, how to
// convert each row for PostgreSQL, and how to sample-compare the two sides.
type tableSpec struct {
	name    string
	columns []string
	source  string
	convert func(*sql.Rows) ([]any, error)

	// sampleKey is the single-column primary key used for sample comparison.
	// Empty disables sampling for composite-key tables.
	sampleKey    string
	sourceExprs  []string // SQLite scalar projections (text)
	targetExprs  []string // PostgreSQL scalar projections (text)
	sourceCount  string
	sourceExists bool
}

// buildSpecs inspects the source schema so older databases (before run/lane or
// web tables existed) can still be migrated with explicit defaults.
func buildSpecs(db *sql.DB) ([]tableSpec, error) {
	columns := map[string]map[string]bool{}
	tables, err := sqliteTables(db)
	if err != nil {
		return nil, err
	}
	for name := range tables {
		cols, err := sqliteColumns(db, name)
		if err != nil {
			return nil, err
		}
		columns[name] = cols
	}
	has := func(table, column string) bool { return columns[table][column] }
	or := func(table, column, fallback string) string {
		if has(table, column) {
			return column
		}
		return fallback
	}

	specs := []tableSpec{
		{
			name: "rooms", columns: []string{"id", "name"},
			source: `SELECT id, name FROM rooms`,
			convert: func(r *sql.Rows) ([]any, error) {
				var id, name string
				if err := r.Scan(&id, &name); err != nil {
					return nil, err
				}
				return []any{id, name}, nil
			},
			sampleKey: "id", sourceExprs: []string{"id", "name"}, targetExprs: []string{"id", "name"},
		},
		{
			name: "users", columns: []string{"username", "password_hash", "invited_by", "created_at"},
			source: `SELECT username, password_hash, COALESCE(` + or("users", "invited_by", `''`) + `,''), created_at FROM users`,
			convert: func(r *sql.Rows) ([]any, error) {
				var username, hash, invited string
				var created int64
				if err := r.Scan(&username, &hash, &invited, &created); err != nil {
					return nil, err
				}
				return []any{username, hash, invited, msTime(created)}, nil
			},
			sampleKey:   "username",
			sourceExprs: []string{"username", "COALESCE(invited_by,'')", "CAST(created_at AS TEXT)"},
			targetExprs: []string{"username", "invited_by", "(round(extract(epoch from created_at)*1000))::bigint::text"},
		},
		{
			name: "invite_codes", columns: []string{"code", "creator", "max_uses", "use_count", "created_at"},
			source: `SELECT code, creator, max_uses, use_count, created_at FROM invite_codes`,
			convert: func(r *sql.Rows) ([]any, error) {
				var code, creator string
				var maxUses, useCount int
				var created int64
				if err := r.Scan(&code, &creator, &maxUses, &useCount, &created); err != nil {
					return nil, err
				}
				return []any{code, creator, int64(maxUses), int64(useCount), msTime(created)}, nil
			},
			sampleKey:   "code",
			sourceExprs: []string{"code", "creator", "CAST(max_uses AS TEXT)", "CAST(use_count AS TEXT)", "CAST(created_at AS TEXT)"},
			targetExprs: []string{"code", "creator", "max_uses::bigint::text", "use_count::bigint::text", "(round(extract(epoch from created_at)*1000))::bigint::text"},
		},
		{
			name: "oidc_users", columns: []string{"sub", "chat_username", "email", "preferred_username", "name", "picture", "created_at", "updated_at"},
			source: `SELECT sub, chat_username, COALESCE(` + or("oidc_users", "email", `''`) + `,''), COALESCE(` + or("oidc_users", "preferred_username", `''`) + `,''), COALESCE(` + or("oidc_users", "name", `''`) + `,''), COALESCE(` + or("oidc_users", "picture", `''`) + `,''), created_at, updated_at FROM oidc_users`,
			convert: func(r *sql.Rows) ([]any, error) {
				var sub, chat, email, preferred, name, picture string
				var created, updated int64
				if err := r.Scan(&sub, &chat, &email, &preferred, &name, &picture, &created, &updated); err != nil {
					return nil, err
				}
				return []any{sub, chat, email, preferred, name, picture, msTime(created), msTime(updated)}, nil
			},
			sampleKey:   "sub",
			sourceExprs: []string{"sub", "chat_username", "COALESCE(" + or("oidc_users", "email", `''`) + ",'')", "COALESCE(" + or("oidc_users", "name", `''`) + ",'')", "CAST(created_at AS TEXT)"},
			targetExprs: []string{"sub", "chat_username", "email", "name", "(round(extract(epoch from created_at)*1000))::bigint::text"},
		},
		{
			name: "guest_identities", columns: []string{"guest_id", "handle", "created_at", "last_seen_at"},
			source: `SELECT guest_id, handle, created_at, last_seen_at FROM guest_identities`,
			convert: func(r *sql.Rows) ([]any, error) {
				var guestID, handle string
				var created, lastSeen int64
				if err := r.Scan(&guestID, &handle, &created, &lastSeen); err != nil {
					return nil, err
				}
				return []any{guestID, handle, msTime(created), msTime(lastSeen)}, nil
			},
			sampleKey:   "guest_id",
			sourceExprs: []string{"guest_id", "handle", "CAST(last_seen_at AS TEXT)"},
			targetExprs: []string{"guest_id", "handle", "(round(extract(epoch from last_seen_at)*1000))::bigint::text"},
		},
		{
			name: "agent_memories", columns: []string{"id", "owner_actor_id", "scope", "thread_id", "kind", "content", "content_hash", "confidence", "importance", "sensitivity", "source", "source_run_id", "source_message_id", "status", "superseded_by", "pinned", "created_at", "updated_at", "valid_from", "valid_to", "expires_at"},
			source: `SELECT id, owner_actor_id, scope, thread_id, kind, content, content_hash, confidence, importance, sensitivity, source, source_run_id, source_message_id, status, superseded_by, pinned, created_at, updated_at, valid_from, valid_to, expires_at FROM agent_memories`,
			convert: func(r *sql.Rows) ([]any, error) {
				var id, owner, scope, thread, kind, content, hash, source, runID, messageID, status string
				var confidence float64
				var importance, sensitivity, pinned int
				var superseded sql.NullString
				var created, updated, validFrom, validTo, expires sql.NullInt64
				if err := r.Scan(&id, &owner, &scope, &thread, &kind, &content, &hash,
					&confidence, &importance, &sensitivity, &source, &runID, &messageID,
					&status, &superseded, &pinned, &created, &updated, &validFrom, &validTo, &expires); err != nil {
					return nil, err
				}
				if !created.Valid {
					return nil, fmt.Errorf("agent_memories %s missing created_at", id)
				}
				return []any{id, owner, scope, thread, kind, content, hash, confidence,
					importance, sensitivity, source, runID, messageID, status,
					nullStringValue(superseded), pinned != 0, msTime(created.Int64), msTime(updated.Int64),
					msTime(validFrom.Int64), msNull(validTo.Int64), msNull(expires.Int64)}, nil
			},
			sampleKey:   "id",
			sourceExprs: []string{"id", "owner_actor_id", "scope", "thread_id", "kind", "content", "status", "CAST(updated_at AS TEXT)"},
			targetExprs: []string{"id", "owner_actor_id", "scope", "thread_id", "kind", "content", "status", "(round(extract(epoch from updated_at)*1000))::bigint::text"},
		},
		{
			name: "agent_memory_events", columns: []string{"id", "memory_id", "actor_id", "action", "detail", "created_at"},
			source: `SELECT id, memory_id, actor_id, action, detail, created_at FROM agent_memory_events`,
			convert: func(r *sql.Rows) ([]any, error) {
				var id, memoryID, actorID, action, detail string
				var created int64
				if err := r.Scan(&id, &memoryID, &actorID, &action, &detail, &created); err != nil {
					return nil, err
				}
				return []any{id, memoryID, actorID, action, detail, msTime(created)}, nil
			},
			sampleKey:   "id",
			sourceExprs: []string{"id", "memory_id", "actor_id", "action"},
			targetExprs: []string{"id", "memory_id", "actor_id", "action"},
		},
		{
			name: "oidc_logout_epochs", columns: []string{"sub", "generation", "updated_at"},
			source: `SELECT sub, generation, updated_at FROM oidc_logout_epochs`,
			convert: func(r *sql.Rows) ([]any, error) {
				var sub string
				var generation, updated int64
				if err := r.Scan(&sub, &generation, &updated); err != nil {
					return nil, err
				}
				return []any{sub, generation, msTime(updated)}, nil
			},
			sampleKey:   "sub",
			sourceExprs: []string{"sub", "CAST(generation AS TEXT)"},
			targetExprs: []string{"sub", "generation::bigint::text"},
		},
		{
			name: "oidc_backchannel_logout_jtis", columns: []string{"jti", "claims_sha256", "received_at", "expires_at"},
			source: `SELECT jti, claims_sha256, received_at, expires_at FROM oidc_backchannel_logout_jtis`,
			convert: func(r *sql.Rows) ([]any, error) {
				var jti, claims string
				var received, expires int64
				if err := r.Scan(&jti, &claims, &received, &expires); err != nil {
					return nil, err
				}
				return []any{jti, claims, msTime(received), msTime(expires)}, nil
			},
			sampleKey:   "jti",
			sourceExprs: []string{"jti", "claims_sha256"},
			targetExprs: []string{"jti", "claims_sha256"},
		},
		{
			name: "user_profiles", columns: []string{"username", "display_name", "avatar_url", "bio", "status", "last_seen"},
			source: `SELECT username, COALESCE(display_name,''), COALESCE(avatar_url,''), COALESCE(bio,''), COALESCE(status,''), COALESCE(last_seen,0) FROM user_profiles`,
			convert: func(r *sql.Rows) ([]any, error) {
				var username, display, avatar, bio, status string
				var lastSeen int64
				if err := r.Scan(&username, &display, &avatar, &bio, &status, &lastSeen); err != nil {
					return nil, err
				}
				return []any{username, display, avatar, bio, status, msNull(lastSeen)}, nil
			},
			sampleKey:   "username",
			sourceExprs: []string{"username", "COALESCE(display_name,'')", "COALESCE(status,'')", "CASE WHEN COALESCE(last_seen,0)=0 THEN '' ELSE CAST(last_seen AS TEXT) END"},
			targetExprs: []string{"username", "display_name", "status", "COALESCE((round(extract(epoch from last_seen)*1000))::bigint::text,'')"},
		},
		{
			name: "blocked_users", columns: []string{"username", "blocked"},
			source: `SELECT username, blocked FROM blocked_users`,
			convert: func(r *sql.Rows) ([]any, error) {
				var username, blocked string
				if err := r.Scan(&username, &blocked); err != nil {
					return nil, err
				}
				return []any{username, blocked}, nil
			},
		},
		{
			name: "notification_prefs", columns: []string{"username", "key", "muted_until", "show_preview"},
			source: `SELECT username, key, COALESCE(muted_until,0), COALESCE(show_preview,1) FROM notification_prefs`,
			convert: func(r *sql.Rows) ([]any, error) {
				var username, key string
				var muted, preview int64
				if err := r.Scan(&username, &key, &muted, &preview); err != nil {
					return nil, err
				}
				return []any{username, key, msNull(muted), preview != 0}, nil
			},
		},
		{
			name: "messages", columns: []string{"id", "seq", "username", "content", "created_at", "reply_to_id", "room_id", "deleted", "edited", "to_user", "thread_id", "parts", "run_id", "actor_id"},
			source: `SELECT id, username, content, timestamp, COALESCE(reply_to_id,''), COALESCE(room_id,''), COALESCE(deleted,0), COALESCE(edited,0), COALESCE(to_user,''), ` +
				or("messages", "thread_id", `''`) + `, ` + or("messages", "parts", `''`) + `, ` + or("messages", "run_id", `''`) + `, ` + or("messages", "actor_id", `''`) + `, rowid FROM messages ORDER BY rowid`,
			convert: func(r *sql.Rows) ([]any, error) {
				var id, username, content, replyTo, room, toUser, thread, parts, runID, actor string
				var ts, deleted, edited, rowid int64
				if err := r.Scan(&id, &username, &content, &ts, &replyTo, &room, &deleted, &edited, &toUser, &thread, &parts, &runID, &actor, &rowid); err != nil {
					return nil, err
				}
				partsValue, err := jsonOrNil(parts)
				if err != nil {
					return nil, fmt.Errorf("messages %s parts: %w", id, err)
				}
				return []any{id, rowid, username, content, msTime(ts), replyTo, room, deleted != 0, edited != 0, toUser, thread, partsValue, runID, actor}, nil
			},
			sampleKey: "id",
			sourceExprs: []string{
				"id", "username", "content", "CAST(timestamp AS TEXT)", "COALESCE(room_id,'')", "COALESCE(to_user,'')",
				or("messages", "thread_id", `''`), or("messages", "run_id", `''`), or("messages", "actor_id", `''`),
				"CASE WHEN COALESCE(deleted,0)<>0 THEN '1' ELSE '0' END",
			},
			targetExprs: []string{
				"id", "username", "content", "(round(extract(epoch from created_at)*1000))::bigint::text", "room_id", "to_user",
				"thread_id", "run_id", "actor_id",
				"CASE WHEN deleted THEN '1' ELSE '0' END",
			},
		},
		{
			name: "reactions", columns: []string{"seq", "message_id", "emoji", "username"},
			source: `SELECT rowid, message_id, emoji, username FROM reactions ORDER BY rowid`,
			convert: func(r *sql.Rows) ([]any, error) {
				var seq int64
				var messageID, emoji, username string
				if err := r.Scan(&seq, &messageID, &emoji, &username); err != nil {
					return nil, err
				}
				return []any{seq, messageID, emoji, username}, nil
			},
		},
		{
			name: "pinned_messages", columns: []string{"room_id", "message_id", "pinned_by", "pinned_at"},
			source: `SELECT COALESCE(room_id,''), message_id, pinned_by, pinned_at FROM pinned_messages`,
			convert: func(r *sql.Rows) ([]any, error) {
				var room, messageID, pinnedBy string
				var pinnedAt int64
				if err := r.Scan(&room, &messageID, &pinnedBy, &pinnedAt); err != nil {
					return nil, err
				}
				return []any{room, messageID, pinnedBy, msTime(pinnedAt)}, nil
			},
		},
		{
			name: "polls", columns: []string{"id", "room_id", "creator", "question", "options", "multiple_choice", "is_anonymous", "is_closed", "votes", "voters", "created_at"},
			source: `SELECT id, COALESCE(room_id,'public'), creator, question, COALESCE(options,'[]'), COALESCE(multiple_choice,0), COALESCE(is_anonymous,0), COALESCE(is_closed,0), COALESCE(votes,'{}'), COALESCE(voters,'{}'), created_at FROM polls`,
			convert: func(r *sql.Rows) ([]any, error) {
				var id, room, creator, question, options, votes, voters string
				var multiple, anonymous, closed int64
				var created int64
				if err := r.Scan(&id, &room, &creator, &question, &options, &multiple, &anonymous, &closed, &votes, &voters, &created); err != nil {
					return nil, err
				}
				for name, value := range map[string]string{"options": options, "votes": votes, "voters": voters} {
					if !json.Valid([]byte(value)) {
						return nil, fmt.Errorf("polls %s %s: invalid JSON", id, name)
					}
				}
				return []any{id, room, creator, question, options, multiple != 0, anonymous != 0, closed != 0, votes, voters, msTime(created)}, nil
			},
			sampleKey:   "id",
			sourceExprs: []string{"id", "creator", "question", "CASE WHEN COALESCE(is_closed,0)<>0 THEN '1' ELSE '0' END", "CAST(created_at AS TEXT)"},
			targetExprs: []string{"id", "creator", "question", "CASE WHEN is_closed THEN '1' ELSE '0' END", "(round(extract(epoch from created_at)*1000))::bigint::text"},
		},
		{
			name: "custom_emojis", columns: []string{"id", "name", "url", "uploader", "room_id", "created_at"},
			source: `SELECT id, name, url, uploader, COALESCE(room_id,''), created_at FROM custom_emojis`,
			convert: func(r *sql.Rows) ([]any, error) {
				var id, name, url, uploader, room string
				var created int64
				if err := r.Scan(&id, &name, &url, &uploader, &room, &created); err != nil {
					return nil, err
				}
				return []any{id, name, url, uploader, room, msTime(created)}, nil
			},
			sampleKey:   "id",
			sourceExprs: []string{"id", "name", "uploader"},
			targetExprs: []string{"id", "name", "uploader"},
		},
		{
			name: "bot_runs", columns: []string{"id", "message_id", "trigger_message_id", "actor_id", "requested_by", "room_id", "to_user", "bot_name", "thread_id", "session_generation", "session_expires_at", "status", "reason", "input_json", "budget_day", "ip_key", "reserved_tokens", "charged_tokens", "created_at", "updated_at"},
			source: `SELECT id, message_id, trigger_message_id, actor_id, requested_by, room_id, to_user, bot_name, ` + or("bot_runs", "thread_id", `''`) +
				`, session_generation, COALESCE(session_expires_at,0), status, COALESCE(reason,''), input_json, budget_day, COALESCE(ip_key,''), reserved_tokens, charged_tokens, created_at, updated_at FROM bot_runs`,
			convert: func(r *sql.Rows) ([]any, error) {
				var id, messageID, triggerID, actorID, requestedBy, room, toUser, botName, thread, status, reason, inputJSON, budgetDay, ipKey string
				var generation, expires, reserved, charged, created, updated int64
				if err := r.Scan(&id, &messageID, &triggerID, &actorID, &requestedBy, &room, &toUser, &botName, &thread,
					&generation, &expires, &status, &reason, &inputJSON, &budgetDay, &ipKey, &reserved, &charged, &created, &updated); err != nil {
					return nil, err
				}
				if !json.Valid([]byte(inputJSON)) {
					inputJSON = "{}"
				}
				day, err := time.Parse("2006-01-02", budgetDay)
				if err != nil {
					return nil, fmt.Errorf("bot_runs %s budget_day %q: %w", id, budgetDay, err)
				}
				return []any{id, messageID, triggerID, actorID, requestedBy, room, toUser, botName, thread,
					generation, secNull(expires), status, reason, inputJSON, day, ipKey, reserved, charged,
					msTime(created), msTime(updated)}, nil
			},
			sampleKey: "id",
			sourceExprs: []string{
				"id", "status", "actor_id", "room_id", "to_user", or("bot_runs", "thread_id", `''`),
				"CAST(session_generation AS TEXT)", "budget_day", "CAST(reserved_tokens AS TEXT)", "CAST(charged_tokens AS TEXT)",
			},
			targetExprs: []string{
				"id", "status", "actor_id", "room_id", "to_user", "thread_id",
				"session_generation::bigint::text", "budget_day::text", "reserved_tokens::bigint::text", "charged_tokens::bigint::text",
			},
		},
		{
			name: "bot_web_calls", columns: []string{"id", "run_id", "tool_call_id", "tool", "provider", "attempt", "price_version", "budget_day", "reserved_micro_cny", "charged_micro_cny", "status", "created_at", "updated_at"},
			source: `SELECT id, run_id, tool_call_id, tool, provider, attempt, price_version, budget_day, reserved_micro_cny, charged_micro_cny, status, created_at, updated_at FROM bot_web_calls`,
			convert: func(r *sql.Rows) ([]any, error) {
				var id, runID, toolCallID, tool, provider, priceVersion, budgetDay, status string
				var attempt, reserved, charged, created, updated int64
				if err := r.Scan(&id, &runID, &toolCallID, &tool, &provider, &attempt, &priceVersion, &budgetDay,
					&reserved, &charged, &status, &created, &updated); err != nil {
					return nil, err
				}
				day, err := time.Parse("2006-01-02", budgetDay)
				if err != nil {
					return nil, fmt.Errorf("bot_web_calls %s budget_day %q: %w", id, budgetDay, err)
				}
				return []any{id, runID, toolCallID, tool, provider, attempt, priceVersion, day, reserved, charged, status, msTime(created), msTime(updated)}, nil
			},
		},
		{
			name: "bot_web_sources", columns: []string{"id", "seq", "run_id", "url", "title", "status", "content", "content_hash", "retrieved_at", "truncated", "error_code", "extractor"},
			source: `SELECT id, run_id, url, title, status, content, content_hash, retrieved_at, COALESCE(truncated,0), COALESCE(error_code,''), ` + or("bot_web_sources", "extractor", `'deepseek-fetch-v1'`) + `, rowid FROM bot_web_sources ORDER BY rowid`,
			convert: func(r *sql.Rows) ([]any, error) {
				var id, runID, url, title, status, content, hash, errorCode, extractor string
				var retrieved, truncated, rowid int64
				if err := r.Scan(&id, &runID, &url, &title, &status, &content, &hash, &retrieved, &truncated, &errorCode, &extractor, &rowid); err != nil {
					return nil, err
				}
				return []any{id, rowid, runID, url, title, status, content, hash, msTime(retrieved), truncated != 0, errorCode, extractor}, nil
			},
			sampleKey:   "id",
			sourceExprs: []string{"id", "run_id", "url", "status", "content_hash", "CASE WHEN COALESCE(truncated,0)<>0 THEN '1' ELSE '0' END"},
			targetExprs: []string{"id", "run_id", "url", "status", "content_hash", "CASE WHEN truncated THEN '1' ELSE '0' END"},
		},
	}
	for i := range specs {
		specs[i].sourceExists = tables[specs[i].name]
	}
	return specs, nil
}

func sqliteTables(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

func sqliteColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info("` + strings.ReplaceAll(table, `"`, `""`) + `")`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var (
			cid        int
			name       string
			ctype      string
			notNull    int
			defaultV   sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &defaultV, &primaryKey); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

func msTime(ms int64) time.Time { return time.UnixMilli(ms) }

func msNull(ms int64) any {
	if ms <= 0 {
		return nil
	}
	return time.UnixMilli(ms)
}

// secNull converts a Unix-seconds value. bot_runs.session_expires_at stores
// seconds, unlike the millisecond columns around it.
func secNull(sec int64) any {
	if sec <= 0 {
		return nil
	}
	return time.Unix(sec, 0)
}

func nullStringValue(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func jsonOrNil(value string) (any, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	if !json.Valid([]byte(trimmed)) {
		return nil, fmt.Errorf("invalid JSON")
	}
	return trimmed, nil
}

// copyTable streams rows from SQLite into PostgreSQL with pgx CopyFrom,
// batching so a large message table does not sit in memory at once.
func copyTable(ctx context.Context, tx pgx.Tx, db *sql.DB, spec tableSpec, batchSize int) (int64, error) {
	rows, err := db.Query(spec.source)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", spec.name, err)
	}
	defer rows.Close()

	var (
		pending [][]any
		copied  int64
	)
	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		_, err := tx.CopyFrom(ctx, pgx.Identifier{spec.name}, spec.columns, pgx.CopyFromRows(pending))
		if err != nil {
			return fmt.Errorf("%s: copy: %w", spec.name, err)
		}
		copied += int64(len(pending))
		pending = pending[:0]
		return nil
	}
	for rows.Next() {
		values, err := spec.convert(rows)
		if err != nil {
			return copied, fmt.Errorf("%s: %w", spec.name, err)
		}
		pending = append(pending, values)
		if len(pending) >= batchSize {
			if err := flush(); err != nil {
				return copied, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return copied, fmt.Errorf("%s: read: %w", spec.name, err)
	}
	if err := flush(); err != nil {
		return copied, err
	}
	return copied, nil
}

// sampleCompare checks up to limit source rows against the target by primary
// key and returns the number of compared rows plus a bounded mismatch list.
func sampleCompare(ctx context.Context, tx pgx.Tx, db *sql.DB, spec tableSpec, limit int) (int, []string, error) {
	if spec.sampleKey == "" || !spec.sourceExists || len(spec.sourceExprs) == 0 {
		return 0, nil, nil
	}
	sourceQuery := fmt.Sprintf("SELECT %s FROM %s ORDER BY %s LIMIT %d",
		strings.Join(spec.sourceExprs, ", "), spec.name, spec.sampleKey, limit)
	rows, err := db.Query(sourceQuery)
	if err != nil {
		return 0, nil, fmt.Errorf("%s sample: %w", spec.name, err)
	}
	defer rows.Close()

	expected := map[string][]string{}
	var keys []string
	for rows.Next() {
		values := make([]sql.NullString, len(spec.sourceExprs))
		dest := make([]any, len(values))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return 0, nil, fmt.Errorf("%s sample scan: %w", spec.name, err)
		}
		key := values[0].String
		canonical := make([]string, len(values)-1)
		for i := 1; i < len(values); i++ {
			canonical[i-1] = values[i].String
		}
		expected[key] = canonical
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	if len(keys) == 0 {
		return 0, nil, nil
	}

	targetColumns := make([]string, len(spec.targetExprs))
	for i, expr := range spec.targetExprs {
		targetColumns[i] = "COALESCE((" + expr + ")::text, '')"
	}
	targetQuery := fmt.Sprintf("SELECT %s FROM %s WHERE %s = ANY($1)",
		strings.Join(targetColumns, ", "), spec.name, spec.sampleKey)
	targetRows, err := tx.Query(ctx, targetQuery, keys)
	if err != nil {
		return 0, nil, fmt.Errorf("%s sample target: %w", spec.name, err)
	}
	defer targetRows.Close()
	actual := map[string][]string{}
	for targetRows.Next() {
		values := make([]string, len(spec.targetExprs))
		dest := make([]any, len(values))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := targetRows.Scan(dest...); err != nil {
			return 0, nil, fmt.Errorf("%s sample target scan: %w", spec.name, err)
		}
		actual[values[0]] = values[1:]
	}
	if err := targetRows.Err(); err != nil {
		return 0, nil, err
	}

	var mismatches []string
	checked := 0
	for _, key := range keys {
		want, ok := expected[key]
		if !ok {
			continue
		}
		checked++
		got, ok := actual[key]
		if !ok {
			mismatches = append(mismatches, fmt.Sprintf("%s key %q missing in target", spec.name, key))
			continue
		}
		if !equalStrings(want, got) {
			mismatches = append(mismatches, fmt.Sprintf("%s key %q: source=%v target=%v", spec.name, key, want, got))
		}
		if len(mismatches) >= 10 {
			break
		}
	}
	sort.Strings(mismatches)
	return checked, mismatches, nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
