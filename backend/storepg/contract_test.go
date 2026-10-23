package storepg_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tokendancechat/backend/internal/storetest"
	"tokendancechat/backend/storepg"
)

var _ storetest.Store = (*storepg.Store)(nil)

// TestPGContract runs the shared contract suite against PostgreSQL. Every case
// gets its own schema so failures cannot leak state into the next case.
func TestPGContract(t *testing.T) {
	dsn := os.Getenv("CHAT_PG_TEST_DSN")
	if dsn == "" {
		t.Skip("CHAT_PG_TEST_DSN is not set; skipping PostgreSQL contract test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("admin pool: %v", err)
	}
	t.Cleanup(admin.Close)
	if err := admin.Ping(ctx); err != nil {
		t.Fatalf("admin ping: %v", err)
	}
	// Extensions are database-wide, so install pg_trgm once in public and keep
	// public on the per-case search_path. Installing it per test schema would
	// make the second case unable to see the operator class.
	if _, err := admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_trgm"); err != nil {
		t.Fatalf("ensure pg_trgm: %v", err)
	}

	storetest.Run(t, func(t *testing.T) storetest.Store {
		schema := "ct_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		setupCtx, setupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer setupCancel()
		if _, err := admin.Exec(setupCtx, "CREATE SCHEMA "+schema); err != nil {
			t.Fatalf("create schema: %v", err)
		}
		cfg, err := pgxpool.ParseConfig(dsn)
		if err != nil {
			t.Fatalf("parse dsn: %v", err)
		}
		// Startup-packet runtime params are the reliable way to scope the pool to
		// the per-case schema; public stays visible for pg_trgm.
		cfg.ConnConfig.RuntimeParams["options"] = "-c search_path=" + schema + ",public"
		pool, err := pgxpool.NewWithConfig(setupCtx, cfg)
		if err != nil {
			t.Fatalf("pool for %s: %v", schema, err)
		}
		var current string
		if err := pool.QueryRow(setupCtx, "SELECT current_schema()").Scan(&current); err != nil {
			t.Fatalf("current_schema: %v", err)
		}
		if current != schema {
			t.Fatalf("search_path not applied: current_schema=%q want %q", current, schema)
		}
		s := storepg.NewFromPool(pool)
		if err := storepg.MigrateWithTableName(setupCtx, pool, schema+".goose_db_version"); err != nil {
			pool.Close()
			t.Fatalf("migrate %s: %v", schema, err)
		}
		t.Cleanup(func() {
			pool.Close()
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
				t.Errorf("drop schema %s: %v", schema, err)
			}
		})
		return s
	})
}
