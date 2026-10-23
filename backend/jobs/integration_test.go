package jobs

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"tokendancechat/backend/storepg"
)

// TestRiverClientRunsMaintenance proves the embedded client is wired to the
// real PostgreSQL schema: River migrations apply and the RunOnStart sweep
// removes a stale guest binding.
func TestRiverClientRunsMaintenance(t *testing.T) {
	dsn := os.Getenv("CHAT_PG_TEST_DSN")
	if dsn == "" {
		t.Skip("CHAT_PG_TEST_DSN is not set; skipping River integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := storepg.MigrateDSN(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store, err := storepg.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.ResolveGuestIdentity("stale-guest", "stale_handle"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pool().Exec(ctx,
		"UPDATE guest_identities SET last_seen_at = now() - interval '200 days' WHERE guest_id = 'stale-guest'"); err != nil {
		t.Fatal(err)
	}

	client, err := Start(ctx, store.Pool(), store, "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = client.Close(stopCtx)
	}()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := store.GuestHandle("stale-guest"); !ok {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("maintenance job did not purge the stale guest binding")
}

// TestRiverMigrationsApplied asserts MigrateDSN created River's tables.
func TestRiverMigrationsApplied(t *testing.T) {
	dsn := os.Getenv("CHAT_PG_TEST_DSN")
	if dsn == "" {
		t.Skip("CHAT_PG_TEST_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := storepg.MigrateDSN(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.river_job') IS NOT NULL").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("river_job table was not created")
	}
}
