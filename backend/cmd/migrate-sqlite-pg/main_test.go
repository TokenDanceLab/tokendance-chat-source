package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tokendancechat/backend/store"
	"tokendancechat/backend/storepg"
)

// TestMigrateSQLiteToPG builds a current-schema SQLite fixture, copies it into
// an isolated PostgreSQL schema, and checks the report plus representative
// converted rows. It is skipped without CHAT_PG_TEST_DSN.
func TestMigrateSQLiteToPG(t *testing.T) {
	dsn := os.Getenv("CHAT_PG_TEST_DSN")
	if dsn == "" {
		t.Skip("CHAT_PG_TEST_DSN is not set; skipping migration integration test")
	}
	t.Setenv("CHAT_SKIP_SEED", "true")

	srcPath := filepath.Join(t.TempDir(), "source.db")
	sq, err := store.New(srcPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	seedFixture(t, sq)
	if err := sq.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	db, err := openSQLite(srcPath)
	if err != nil {
		t.Fatalf("openSQLite: %v", err)
	}
	defer db.Close()
	specs, err := buildSpecs(db)
	if err != nil {
		t.Fatalf("buildSpecs: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("admin pool: %v", err)
	}
	defer admin.Close()
	schema := "mig_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	defer admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["options"] = "-c search_path=" + schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	if err := storepg.MigrateWithTableName(ctx, pool, schema+".goose_db_version"); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	rep, err := migrate(ctx, db, pool, specs, 50, 50, true)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !rep.OK {
		t.Fatalf("migration report failed: %+v", rep)
	}
	if rep.SampleChecked == 0 {
		t.Fatal("no sample rows compared")
	}
	assertTable := func(name string) {
		t.Helper()
		for _, tr := range rep.Tables {
			if tr.Table == name {
				if tr.Source == 0 || !tr.Match {
					t.Fatalf("%s report = %+v", name, tr)
				}
				return
			}
		}
		t.Fatalf("missing table report for %s", name)
	}
	for _, name := range []string{"rooms", "messages", "reactions", "pinned_messages", "polls", "oidc_users", "bot_runs", "bot_web_calls", "bot_web_sources"} {
		assertTable(name)
	}

	var (
		msgCount, runCount, sourceCount, burstCount int
	)
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM messages").Scan(&msgCount); err != nil || msgCount == 0 {
		t.Fatalf("target messages = %d, err=%v", msgCount, err)
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM bot_runs").Scan(&runCount); err != nil || runCount < 2 {
		t.Fatalf("target bot_runs = %d, err=%v", runCount, err)
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM bot_web_sources").Scan(&sourceCount); err != nil || sourceCount == 0 {
		t.Fatalf("target bot_web_sources = %d, err=%v", sourceCount, err)
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM messages WHERE run_id LIKE $1", "%#%").Scan(&burstCount); err != nil || burstCount == 0 {
		t.Fatalf("burst segment count = %d, err=%v", burstCount, err)
	}

	// Timestamp/type fidelity for one known message.
	firstID := fixtureFirstMessageID
	var (
		content   string
		ts        int64
		deleted   bool
		partsText string
	)
	err = pool.QueryRow(ctx,
		`SELECT content, (round(extract(epoch from created_at)*1000))::bigint, deleted, COALESCE(parts::text,'')
		 FROM messages WHERE id=$1`, firstID).Scan(&content, &ts, &deleted, &partsText)
	if err != nil {
		t.Fatalf("sample message: %v", err)
	}
	if content != "hello migration" || deleted || ts == 0 {
		t.Fatalf("sample message = content=%q ts=%d deleted=%v", content, ts, deleted)
	}
	if partsText != "" && !json.Valid([]byte(partsText)) {
		t.Fatalf("parts not valid JSON: %s", partsText)
	}
}

var fixtureFirstMessageID string

func seedFixture(t *testing.T, s *store.Store) {
	t.Helper()
	roomID, err := s.CreateRoom("migration-room")
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	trigger, err := s.InsertActorMessage("local:alice", "alice", "hello migration", "", roomID, "", "")
	if err != nil {
		t.Fatalf("InsertActorMessage: %v", err)
	}
	fixtureFirstMessageID = trigger.ID
	if _, err := s.InsertActorMessage("local:bob", "bob", "thread reply", "", roomID, "", trigger.ID); err != nil {
		t.Fatalf("thread reply: %v", err)
	}
	if _, err := s.InsertMessageWithParts("TokenBot", "private reply", "", "alice", "", json.RawMessage(`[{"type":"text","text":"private reply"}]`)); err != nil {
		t.Fatalf("private message: %v", err)
	}
	if _, err := s.ToggleReaction(trigger.ID, "👍", "alice"); err != nil {
		t.Fatalf("reaction: %v", err)
	}
	if err := s.PinMessage(roomID, trigger.ID, "alice"); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if err := s.BlockUser("alice", "bob"); err != nil {
		t.Fatalf("block: %v", err)
	}
	if err := s.SetNotificationPrefs("alice", "dm:TokenBot", 5000, false); err != nil {
		t.Fatalf("prefs: %v", err)
	}
	if err := s.UpsertUserProfile("alice", "Alice", "", "", "", 0); err != nil {
		t.Fatalf("profile: %v", err)
	}
	if err := s.CreatePoll(&store.Poll{
		ID: "poll-1", RoomID: roomID, Creator: "alice", Question: "q?",
		Options: []string{"a", "b"}, Votes: map[int]int{}, Voters: map[int][]string{},
		CreatedAt: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := s.AddCustomEmoji("party", "https://example.com/p.gif", "alice", roomID); err != nil {
		t.Fatalf("emoji: %v", err)
	}
	if _, err := s.UpsertOIDCUser(store.OIDCUserProfile{Sub: "sub-fixture", DesiredUsername: "oidc-alice", Name: "OIDC Alice"}); err != nil {
		t.Fatalf("oidc: %v", err)
	}
	code, err := s.GenerateInviteCode("admin", 5)
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	if err := s.RegisterUser("carol", "fixture-password", code); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Run 1: web research + two-segment final.
	runTrigger, err := s.InsertActorMessage("local:alice", "alice", "research this", "", roomID, "", "")
	if err != nil {
		t.Fatalf("run trigger: %v", err)
	}
	runID := uuid.NewString()
	r := store.BotRun{
		ID: runID, MessageID: uuid.NewString(), TriggerMessageID: runTrigger.ID,
		ActorID: "local:alice", RequestedBy: "alice", RoomID: roomID, BotName: "TokenBot",
		Input: json.RawMessage(`{"q":"research"}`), BudgetDay: time.Now().Format("2006-01-02"),
		IPKey: "ip:10.0.0.1", ReservedTokens: 100, CreatedAt: time.Now().UnixMilli(),
	}
	if reason, err := s.CreateBotRun(r, store.RunBudgetLimits{}); err != nil || reason != "" {
		t.Fatalf("CreateBotRun = %q, %v", reason, err)
	}
	if err := s.StartBotRun(runID); err != nil {
		t.Fatalf("StartBotRun: %v", err)
	}
	if err := s.ReserveWebCall(runID, "call-1", "tc-1", "web_search", 1_000_000); err != nil {
		t.Fatalf("ReserveWebCall: %v", err)
	}
	if _, err := s.SaveWebSource(store.WebSource{
		ID: "src-1", RunID: runID, URL: "https://example.com/a", Title: "A",
		Status: "fetched", Content: "body", ContentHash: "hash",
		RetrievedAt: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("SaveWebSource: %v", err)
	}
	if _, _, err := s.CompleteBotRunSplit(runID, []string{"first [[source:src-1]]", "second"}, nil, 10, true); err != nil {
		t.Fatalf("CompleteBotRunSplit: %v", err)
	}
	if err := s.SettleWebCall("call-1", "fetched", false); err != nil {
		t.Fatalf("SettleWebCall: %v", err)
	}

	// Run 2: failed terminal state.
	failTrigger, err := s.InsertActorMessage("local:bob", "bob", "fail this", "", roomID, "", "")
	if err != nil {
		t.Fatalf("fail trigger: %v", err)
	}
	failID := uuid.NewString()
	if reason, err := s.CreateBotRun(store.BotRun{
		ID: failID, MessageID: uuid.NewString(), TriggerMessageID: failTrigger.ID,
		ActorID: "local:bob", RequestedBy: "bob", RoomID: roomID, BotName: "TokenBot",
		Input: json.RawMessage(`{"q":"fail"}`), BudgetDay: time.Now().Format("2006-01-02"),
		IPKey: "ip:10.0.0.2", ReservedTokens: 100, CreatedAt: time.Now().UnixMilli(),
	}, store.RunBudgetLimits{}); err != nil || reason != "" {
		t.Fatalf("CreateBotRun failed run = %q, %v", reason, err)
	}
	if err := s.StartBotRun(failID); err != nil {
		t.Fatalf("StartBotRun failed run: %v", err)
	}
	if err := s.FinishBotRun(failID, "failed", "provider", 0, false); err != nil {
		t.Fatalf("FinishBotRun: %v", err)
	}
}
