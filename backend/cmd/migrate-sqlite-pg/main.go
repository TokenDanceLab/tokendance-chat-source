// Command migrate-sqlite-pg copies the chat SQLite database into PostgreSQL.
//
// It is an offline, single-shot tool: stop the application, run it against a
// scratch PostgreSQL database, verify the report, then switch the backend.
// Source tables are read through an explicit allowlist so retired IM tables
// (calls, folders, webhooks, ...) never leak into the new schema.
//
// Usage:
//
//	go run ./cmd/migrate-sqlite-pg -sqlite data/chat.db -pg "$CHAT_PG_DSN"
//	go run ./cmd/migrate-sqlite-pg -sqlite data/chat.db -dry-run
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "modernc.org/sqlite"

	"tokendancechat/backend/storepg"
)

type tableReport struct {
	Table  string `json:"table"`
	Source int64  `json:"source"`
	Target int64  `json:"target"`
	Match  bool   `json:"match"`
	Note   string `json:"note,omitempty"`
}

type integrityReport struct {
	OrphanRunLinks       int64 `json:"orphan_run_links"`
	OrphanThreadParents  int64 `json:"orphan_thread_parents"`
	PrivateRoomLeaks     int64 `json:"private_room_leaks"`
	PublicRunLanes       int64 `json:"public_run_lanes"`
	CompletedRunsNoFinal int64 `json:"completed_runs_without_final"`
	OrphanWebCalls       int64 `json:"orphan_web_calls"`
	OrphanWebSources     int64 `json:"orphan_web_sources"`
}

type report struct {
	Source         string          `json:"source"`
	Tables         []tableReport   `json:"tables"`
	SampleChecked  int             `json:"sample_checked"`
	SampleMismatch []string        `json:"sample_mismatches,omitempty"`
	Integrity      integrityReport `json:"integrity"`
	OK             bool            `json:"ok"`
}

func main() {
	sqlitePath := flag.String("sqlite", "", "path to the source SQLite database (required)")
	pgDSN := flag.String("pg", "", "PostgreSQL DSN (or CHAT_PG_DSN)")
	batch := flag.Int("batch", 500, "rows per COPY batch")
	sample := flag.Int("sample", 100, "rows per table to sample-compare")
	force := flag.Bool("force", false, "truncate a non-empty target schema")
	dryRun := flag.Bool("dry-run", false, "read and validate the source without writing")
	reportPath := flag.String("report", "", "write the JSON report to this path")
	flag.Parse()

	if *sqlitePath == "" {
		fmt.Fprintln(os.Stderr, "error: -sqlite is required")
		os.Exit(2)
	}
	if *pgDSN == "" {
		*pgDSN = os.Getenv("CHAT_PG_DSN")
	}

	db, err := openSQLite(*sqlitePath)
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	specs, err := buildSpecs(db)
	if err != nil {
		fatal(err)
	}

	if *dryRun {
		if err := runDryRun(db, specs); err != nil {
			fatal(err)
		}
		return
	}
	if *pgDSN == "" {
		fmt.Fprintln(os.Stderr, "error: -pg or CHAT_PG_DSN is required unless -dry-run is set")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, *pgDSN)
	if err != nil {
		fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		fatal(fmt.Errorf("postgres ping: %w", err))
	}
	if err := storepg.Migrate(ctx, pool); err != nil {
		fatal(err)
	}

	rep, err := migrate(ctx, db, pool, specs, *batch, *sample, *force)
	if err != nil {
		fatal(err)
	}
	rep.Source = *sqlitePath
	printReport(rep)
	if *reportPath != "" {
		if err := writeReport(*reportPath, rep); err != nil {
			fatal(err)
		}
	}
	if !rep.OK {
		os.Exit(1)
	}
}

func openSQLite(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("sqlite source: %w", err)
	}
	dsn := "file:" + filepath.ToSlash(path) + "?mode=ro&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return db, nil
}

func runDryRun(db *sql.DB, specs []tableSpec) error {
	fmt.Printf("%-30s %12s %s\n", "table", "rows", "note")
	for _, spec := range specs {
		if !spec.sourceExists {
			fmt.Printf("%-30s %12d %s\n", spec.name, 0, "absent in source")
			continue
		}
		var count int64
		if err := db.QueryRow("SELECT COUNT(*) FROM " + spec.name).Scan(&count); err != nil {
			return fmt.Errorf("%s count: %w", spec.name, err)
		}
		rows, err := db.Query(spec.source)
		if err != nil {
			return fmt.Errorf("%s: %w", spec.name, err)
		}
		var converted int64
		for rows.Next() {
			if _, err := spec.convert(rows); err != nil {
				rows.Close()
				return fmt.Errorf("%s: %w", spec.name, err)
			}
			converted++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", spec.name, err)
		}
		if converted != count {
			return fmt.Errorf("%s: converted %d rows, counted %d", spec.name, converted, count)
		}
		fmt.Printf("%-30s %12d %s\n", spec.name, count, "conversion ok")
	}
	fmt.Println("dry-run OK: no writes performed")
	return nil
}

func migrate(ctx context.Context, db *sql.DB, pool *pgxpool.Pool, specs []tableSpec, batchSize, sampleSize int, force bool) (report, error) {
	rep := report{Tables: make([]tableReport, 0, len(specs)), OK: true}

	// Refuse to merge into a populated target unless the operator opted in.
	for _, spec := range specs {
		var count int64
		if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+spec.name).Scan(&count); err != nil {
			return rep, fmt.Errorf("target count %s: %w", spec.name, err)
		}
		if count > 0 && !force {
			return rep, fmt.Errorf("target table %s already has %d rows; pass -force to truncate", spec.name, count)
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return rep, err
	}
	defer tx.Rollback(ctx)

	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.name)
	}
	if _, err := tx.Exec(ctx, "TRUNCATE "+strings.Join(names, ", ")+" RESTART IDENTITY CASCADE"); err != nil {
		return rep, fmt.Errorf("truncate: %w", err)
	}

	for _, spec := range specs {
		tr := tableReport{Table: spec.name}
		if !spec.sourceExists {
			tr.Note = "absent in source"
			tr.Match = true
			rep.Tables = append(rep.Tables, tr)
			continue
		}
		if err := db.QueryRow("SELECT COUNT(*) FROM " + spec.name).Scan(&tr.Source); err != nil {
			return rep, fmt.Errorf("source count %s: %w", spec.name, err)
		}
		copied, err := copyTable(ctx, tx, db, spec, batchSize)
		if err != nil {
			return rep, err
		}
		if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM "+spec.name).Scan(&tr.Target); err != nil {
			return rep, fmt.Errorf("target count %s: %w", spec.name, err)
		}
		tr.Match = tr.Source == tr.Target && copied == tr.Source
		if !tr.Match {
			rep.OK = false
		}
		rep.Tables = append(rep.Tables, tr)
	}

	for _, table := range []string{"messages", "reactions", "bot_web_sources"} {
		query := fmt.Sprintf(
			"SELECT setval(pg_get_serial_sequence('%s','seq'), COALESCE((SELECT MAX(seq) FROM %s),0)+1, false)",
			table, table)
		if _, err := tx.Exec(ctx, query); err != nil {
			return rep, fmt.Errorf("setval %s: %w", table, err)
		}
	}

	for _, spec := range specs {
		checked, mismatches, err := sampleCompare(ctx, tx, db, spec, sampleSize)
		if err != nil {
			return rep, err
		}
		rep.SampleChecked += checked
		rep.SampleMismatch = append(rep.SampleMismatch, mismatches...)
	}
	if len(rep.SampleMismatch) > 0 {
		rep.OK = false
	}

	if err := verifyIntegrity(ctx, tx, &rep.Integrity); err != nil {
		return rep, err
	}
	if rep.Integrity != (integrityReport{}) {
		rep.OK = false
	}
	if err := tx.Commit(ctx); err != nil {
		return rep, err
	}
	return rep, nil
}

func verifyIntegrity(ctx context.Context, tx pgx.Tx, out *integrityReport) error {
	checks := []struct {
		name  string
		query string
		dst   *int64
	}{
		{"orphan run links", `SELECT COUNT(*) FROM messages m
			WHERE m.run_id <> '' AND NOT EXISTS (
				SELECT 1 FROM bot_runs r
				WHERE r.id = CASE WHEN position('#' in m.run_id) > 0 THEN split_part(m.run_id, '#', 1) ELSE m.run_id END
			)`, &out.OrphanRunLinks},
		{"orphan thread parents", `SELECT COUNT(*) FROM messages m
			WHERE m.to_user = '' AND m.thread_id <> ''
			  AND NOT EXISTS (SELECT 1 FROM messages p WHERE p.id = m.thread_id)`, &out.OrphanThreadParents},
		{"private messages in rooms", `SELECT COUNT(*) FROM messages WHERE to_user <> '' AND room_id <> ''`, &out.PrivateRoomLeaks},
		{"public runs with lanes", `SELECT COUNT(*) FROM bot_runs WHERE room_id <> '' AND thread_id <> ''`, &out.PublicRunLanes},
		{"completed runs without final", `SELECT COUNT(*) FROM bot_runs r
			WHERE r.status = 'completed' AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.run_id = r.id)`, &out.CompletedRunsNoFinal},
		{"orphan web calls", `SELECT COUNT(*) FROM bot_web_calls c
			WHERE NOT EXISTS (SELECT 1 FROM bot_runs r WHERE r.id = c.run_id)`, &out.OrphanWebCalls},
		{"orphan web sources", `SELECT COUNT(*) FROM bot_web_sources s
			WHERE NOT EXISTS (SELECT 1 FROM bot_runs r WHERE r.id = s.run_id)`, &out.OrphanWebSources},
	}
	for _, check := range checks {
		if err := tx.QueryRow(ctx, check.query).Scan(check.dst); err != nil {
			return fmt.Errorf("integrity %s: %w", check.name, err)
		}
	}
	return nil
}

func printReport(rep report) {
	fmt.Printf("%-30s %12s %12s %s\n", "table", "source", "target", "result")
	for _, tr := range rep.Tables {
		result := "ok"
		if !tr.Match {
			result = "MISMATCH"
		}
		if tr.Note != "" {
			fmt.Printf("%-30s %12d %12d %s (%s)\n", tr.Table, tr.Source, tr.Target, result, tr.Note)
			continue
		}
		fmt.Printf("%-30s %12d %12d %s\n", tr.Table, tr.Source, tr.Target, result)
	}
	fmt.Printf("sample: checked=%d mismatches=%d\n", rep.SampleChecked, len(rep.SampleMismatch))
	for _, mismatch := range rep.SampleMismatch {
		fmt.Printf("  sample mismatch: %s\n", mismatch)
	}
	fmt.Printf("integrity: run_links=%d thread_parents=%d private_room_leaks=%d public_run_lanes=%d completed_without_final=%d web_calls=%d web_sources=%d\n",
		rep.Integrity.OrphanRunLinks, rep.Integrity.OrphanThreadParents, rep.Integrity.PrivateRoomLeaks,
		rep.Integrity.PublicRunLanes, rep.Integrity.CompletedRunsNoFinal,
		rep.Integrity.OrphanWebCalls, rep.Integrity.OrphanWebSources)
	if rep.OK {
		fmt.Println("RESULT: OK")
	} else {
		fmt.Println("RESULT: FAILED")
	}
}

func writeReport(path string, rep report) error {
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "migrate-sqlite-pg: %v\n", err)
	os.Exit(1)
}
