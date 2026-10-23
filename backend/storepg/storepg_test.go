package storepg_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"tokendancechat/backend/store"
	"tokendancechat/backend/storepg"
)

// The run and web interfaces live in unexported hub types, so the test mirrors
// their method sets to catch signature drift before the adapter gets wired.
type runStoreContract interface {
	WithBotRunPublication(string, func()) error
	SnapshotBotHistory(string, string, string, string, string, bool, int) ([]store.StoredMessage, error)
	CreateBotRun(store.BotRun, store.RunBudgetLimits) (string, error)
	GetBotRun(string) (store.BotRun, error)
	StartBotRun(string) error
	FinishBotRun(string, string, string, int64, bool) error
	CompleteBotRun(string, string, json.RawMessage, int64, bool) (store.StoredMessage, bool, error)
	CompleteBotRunSplit(string, []string, json.RawMessage, int64, bool) ([]store.StoredMessage, bool, error)
	InterruptBotRuns() error
	RecentBotRuns(string, string, int) ([]store.BotRun, error)
}

type webStoreContract interface {
	ReserveWebCall(string, string, string, string, int64) error
	SettleWebCall(string, string, bool) error
	SaveWebSource(store.WebSource) (store.WebSource, error)
	GetWebSource(string, string) (store.WebSource, error)
}

var (
	_ runStoreContract = (*storepg.Store)(nil)
	_ webStoreContract = (*storepg.Store)(nil)
)

// TestMigrateSchema applies the embedded migrations to the database named by
// CHAT_PG_TEST_DSN and verifies the P0 schema. Point it at a scratch database:
// the test creates tables but never drops or truncates data.
//
//	CHAT_PG_TEST_DSN='postgres://chat:chat@127.0.0.1:55432/tokendance_chat?sslmode=disable' go test ./storepg/ # leak-guard-allow:LG-F4C6A155 dev-only example credentials
func TestMigrateSchema(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL migration test in -short mode")
	}
	dsn := os.Getenv("CHAT_PG_TEST_DSN")
	if dsn == "" {
		t.Skip("CHAT_PG_TEST_DSN is not set; skipping PostgreSQL migration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s, err := storepg.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("storepg.Open: %v", err)
	}
	if err := s.Ping(); err != nil {
		s.Close()
		t.Fatalf("Ping: %v", err)
	}
	if err := s.Migrate(ctx); err != nil {
		s.Close()
		t.Fatalf("Migrate: %v", err)
	}
	// goose must report a clean, idempotent second run.
	if err := s.Migrate(ctx); err != nil {
		s.Close()
		t.Fatalf("second Migrate must be a no-op: %v", err)
	}
	s.Close()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	// P0 schema: the active SQLite tables, translated.
	tables := []string{
		"rooms",
		"messages",
		"reactions",
		"blocked_users",
		"pinned_messages",
		"notification_prefs",
		"user_profiles",
		"polls",
		"custom_emojis",
		"users",
		"invite_codes",
		"oidc_users",
		"oidc_logout_epochs",
		"oidc_backchannel_logout_jtis",
		"bot_runs",
		"bot_web_calls",
		"bot_web_sources",
	}
	for _, table := range tables {
		if !tableExists(ctx, t, pool, table) {
			t.Errorf("missing table %q", table)
		}
	}

	// The agent_tasks domain is P1: it must not be created by 0001_init.sql.
	if tableExists(ctx, t, pool, "agent_tasks") {
		t.Error("agent_tasks exists: the P1 domain must stay out of the P0 migration")
	}

	// Columns whose absence would silently break run isolation or history.
	requiredColumns := map[string][]string{
		"messages": {
			"id", "seq", "username", "content", "created_at", "reply_to_id",
			"room_id", "deleted", "edited", "to_user", "thread_id", "parts",
			"run_id", "actor_id",
		},
		"bot_runs": {
			"id", "message_id", "trigger_message_id", "actor_id", "requested_by",
			"room_id", "to_user", "bot_name", "thread_id", "session_generation",
			"session_expires_at", "status", "reason", "input_json", "budget_day",
			"ip_key", "reserved_tokens", "charged_tokens", "created_at", "updated_at",
		},
		"bot_web_sources": {
			"id", "run_id", "url", "title", "status", "content", "content_hash",
			"retrieved_at", "truncated", "error_code", "extractor",
		},
	}
	for table, columns := range requiredColumns {
		for _, column := range columns {
			var exists bool
			err := pool.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2
				)`, table, column).Scan(&exists)
			if err != nil {
				t.Fatalf("inspect %s.%s: %v", table, column, err)
			}
			if !exists {
				t.Errorf("missing column %s.%s", table, column)
			}
		}
	}

	// Type decisions that the Go adapter depends on.
	typeChecks := []struct {
		table, column, want string
	}{
		{"messages", "created_at", "timestamp with time zone"},
		{"messages", "deleted", "boolean"},
		{"messages", "parts", "jsonb"},
		{"bot_runs", "session_expires_at", "timestamp with time zone"},
		{"bot_runs", "input_json", "jsonb"},
		{"bot_runs", "budget_day", "date"},
		{"notification_prefs", "muted_until", "timestamp with time zone"},
	}
	for _, check := range typeChecks {
		var got string
		err := pool.QueryRow(ctx, `
			SELECT data_type FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`,
			check.table, check.column).Scan(&got)
		if err != nil {
			t.Fatalf("inspect type %s.%s: %v", check.table, check.column, err)
		}
		if got != check.want {
			t.Errorf("%s.%s type = %q, want %q", check.table, check.column, got, check.want)
		}
	}

	// Indexes that encode invariants, not just performance.
	for _, index := range []string{"messages_run_final", "idx_messages_trgm", "bot_runs_budget_day", "idx_messages_room"} {
		var exists bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes
				WHERE schemaname = current_schema() AND indexname = $1
			)`, index).Scan(&exists)
		if err != nil {
			t.Fatalf("inspect index %s: %v", index, err)
		}
		if !exists {
			t.Errorf("missing index %q", index)
		}
	}

	// Short session timeouts make this failure mode obvious in CI.
	if deadline, ok := ctx.Deadline(); ok && time.Now().After(deadline) {
		t.Fatal("schema verification timed out")
	}
}

func tableExists(ctx context.Context, t *testing.T, pool *pgxpool.Pool, table string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = current_schema() AND table_name = $1
		)`, table).Scan(&exists)
	if err != nil {
		t.Fatalf("inspect table %s: %v", table, err)
	}
	return exists
}
