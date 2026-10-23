package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"tokendancechat/backend/llm"
	"tokendancechat/backend/websearch"
)

// WebSource is private, run-scoped evidence. Content is the bounded provider
// Markdown snapshot; no final/redirect URL is invented when the API omits it.
type WebSource struct {
	ID, RunID, URL, Title, Status, Content, ContentHash, ErrorCode string
	RetrievedAt                                                    int64
	Truncated                                                      bool
}

func (s *Store) migrateWeb() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS bot_web_calls (
 id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES bot_runs(id), tool_call_id TEXT NOT NULL,
 tool TEXT NOT NULL, provider TEXT NOT NULL, attempt INTEGER NOT NULL, price_version TEXT NOT NULL,
 budget_day TEXT NOT NULL, reserved_micro_cny INTEGER NOT NULL, charged_micro_cny INTEGER NOT NULL,
 status TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
 UNIQUE(run_id,tool_call_id));
 CREATE INDEX IF NOT EXISTS bot_web_calls_day ON bot_web_calls(budget_day);
 CREATE TABLE IF NOT EXISTS bot_web_sources (
 id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES bot_runs(id), url TEXT NOT NULL,
 title TEXT NOT NULL, status TEXT NOT NULL, content TEXT NOT NULL, content_hash TEXT NOT NULL,
 retrieved_at INTEGER NOT NULL, truncated INTEGER NOT NULL, error_code TEXT NOT NULL,
 extractor TEXT NOT NULL DEFAULT 'deepseek-fetch-v1', UNIQUE(run_id,url));`)
	return err
}

// ReserveWebCall commits the monetary reservation before network dispatch.
// A cancelled or interrupted request retains any unknown provider charge.
func (s *Store) ReserveWebCall(runID, id, toolCallID, tool string, dailyCap int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := scanRun(tx.QueryRow("SELECT "+runColumns+" FROM bot_runs WHERE id=?", runID))
	if err != nil || r.Status != "running" || !runSessionCurrent(tx, r) {
		return ErrRunUnavailable
	}
	var count int
	if err = tx.QueryRow("SELECT COUNT(*) FROM bot_web_calls WHERE run_id=?", runID).Scan(&count); err != nil {
		return err
	}
	var total int64
	if err = tx.QueryRow("SELECT COALESCE(SUM(charged_micro_cny),0) FROM bot_web_calls WHERE budget_day=?", r.BudgetDay).Scan(&total); err != nil {
		return err
	}
	if count >= websearch.MaxCalls || dailyCap <= 0 || total > dailyCap-websearch.PriceMicroCNY {
		return errors.New("web research budget exhausted")
	}
	now := time.Now().UnixMilli()
	_, err = tx.Exec(`INSERT INTO bot_web_calls(id,run_id,tool_call_id,tool,provider,attempt,price_version,budget_day,reserved_micro_cny,charged_micro_cny,status,created_at,updated_at) VALUES(?,?,?,?,'deepseek',1,?,?,?,?, 'reserved',?,?)`, id, runID, toolCallID, tool, websearch.PriceVersion, r.BudgetDay, websearch.PriceMicroCNY, websearch.PriceMicroCNY, now, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SettleWebCall(id, status string, unbilled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	charged := websearch.PriceMicroCNY
	if unbilled {
		charged = 0
	}
	_, err := s.db.Exec("UPDATE bot_web_calls SET status=?,charged_micro_cny=?,updated_at=? WHERE id=? AND status='reserved'", status, charged, time.Now().UnixMilli(), id)
	return err
}

func (s *Store) SaveWebSource(source WebSource) (WebSource, error) {
	if !websearch.SafeURL(source.URL) {
		return WebSource{}, errors.New("invalid source URL")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return WebSource{}, err
	}
	defer tx.Rollback()
	r, err := scanRun(tx.QueryRow("SELECT "+runColumns+" FROM bot_runs WHERE id=?", source.RunID))
	if err != nil || r.Status != "running" || !runSessionCurrent(tx, r) {
		return WebSource{}, ErrRunUnavailable
	}
	var old WebSource
	old, err = scanSource(tx.QueryRow("SELECT "+sourceColumns+" FROM bot_web_sources WHERE run_id=? AND url=?", source.RunID, source.URL))
	if err == nil {
		source.ID = old.ID
		// A later discovery or failed refresh does not erase already-read evidence.
		if source.Status == "discovered" || old.Status == "fetched" {
			return old, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return WebSource{}, err
	}
	_, err = tx.Exec(`INSERT INTO bot_web_sources(id,run_id,url,title,status,content,content_hash,retrieved_at,truncated,error_code) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(run_id,url) DO UPDATE SET title=excluded.title,status=excluded.status,content=excluded.content,content_hash=excluded.content_hash,retrieved_at=excluded.retrieved_at,truncated=excluded.truncated,error_code=excluded.error_code`, source.ID, source.RunID, source.URL, source.Title, source.Status, source.Content, source.ContentHash, source.RetrievedAt, source.Truncated, source.ErrorCode)
	if err != nil {
		return WebSource{}, err
	}
	return source, tx.Commit()
}

const sourceColumns = "id,run_id,url,title,status,content,content_hash,retrieved_at,truncated,error_code"

func scanSource(row interface{ Scan(...any) error }) (WebSource, error) {
	var s WebSource
	err := row.Scan(&s.ID, &s.RunID, &s.URL, &s.Title, &s.Status, &s.Content, &s.ContentHash, &s.RetrievedAt, &s.Truncated, &s.ErrorCode)
	return s, err
}
func (s *Store) GetWebSource(runID, id string) (WebSource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return scanSource(s.db.QueryRow("SELECT "+sourceColumns+" FROM bot_web_sources WHERE run_id=? AND id=?", runID, id))
}

// Consume malformed/unclosed reserved markers as well as valid references.
var citationPattern = regexp.MustCompile(`(?s)\[\[source:(.*?)(?:\]\]|$)`)

// finalWebParts is the only source publication path: ignore supplied source
// parts, reload this run's evidence, resolve known fetched IDs, drop unknown
// markers. Model-created links remain ordinary text, never evidence metadata.
func finalWebParts(tx *sql.Tx, runID, content string, raw json.RawMessage) (string, json.RawMessage, error) {
	contents, partsList, err := finalWebPartsMulti(tx, runID, []string{content}, raw)
	if err != nil {
		return "", nil, err
	}
	return contents[0], partsList[0], nil
}

// finalWebPartsMulti is the multi-segment variant of finalWebParts: citation
// numbering runs continuously across segments, and the rebuilt source DTOs
// are attached to the last segment only, so the source list appears once, at
// the end of the burst. Every earlier segment carries a single text part.
func finalWebPartsMulti(tx *sql.Tx, runID string, contents []string, raw json.RawMessage) ([]string, []json.RawMessage, error) {
	rows, err := tx.Query("SELECT "+sourceColumns+" FROM bot_web_sources WHERE run_id=? ORDER BY rowid LIMIT 12", runID)
	if err != nil {
		return nil, nil, err
	}
	var sources []WebSource
	for rows.Next() {
		source, e := scanSource(rows)
		if e != nil {
			rows.Close()
			return nil, nil, e
		}
		if websearch.SafeURL(source.URL) {
			sources = append(sources, source)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	used := map[string]int{}
	resolve := func(text string) string {
		return citationPattern.ReplaceAllStringFunc(text, func(marker string) string {
			id := citationPattern.FindStringSubmatch(marker)[1]
			for _, source := range sources {
				if source.ID != id || source.Status != "fetched" {
					continue
				}
				n, ok := used[id]
				if !ok {
					n = len(used) + 1
					used[id] = n
				}
				// Angle destination protects URLs containing parentheses in Markdown.
				return fmt.Sprintf("[%d](<%s>)", n, source.URL)
			}
			return ""
		})
	}
	for i := range contents {
		contents[i] = resolve(contents[i])
	}
	var parts []llm.Part
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &parts)
	}
	last := len(contents) - 1
	filtered := make([]llm.Part, 0, len(parts)+len(sources))
	// Preserve the old parts shape when no web research happened.
	if len(sources) == 0 {
		textPlaced := false
		for _, part := range parts {
			if part.Type != "source" {
				if part.Type == "text" {
					if textPlaced {
						continue
					}
					part.Text = contents[last]
					textPlaced = true
				}
				filtered = append(filtered, part)
			}
		}
		if !textPlaced {
			filtered = append(filtered, llm.Part{Type: "text", Text: contents[last]})
		}
	} else {
		for _, part := range parts {
			if part.Type != "source" && part.Type != "text" {
				filtered = append(filtered, part)
			}
		}
		filtered = append(filtered, llm.Part{Type: "text", Text: contents[last]})
	}
	for _, source := range sources {
		if source.Status != "discovered" && source.Status != "fetched" && source.Status != "failed" {
			continue
		}
		title := strings.TrimSpace(source.Title)
		if title == "" {
			u, _ := url.Parse(source.URL)
			title = u.Hostname()
		}
		filtered = append(filtered, llm.Part{Type: "source", SourceID: source.ID, Title: websearch.Clip(title, 200), URL: source.URL, Status: source.Status, Cited: used[source.ID] > 0})
	}
	lastData, err := json.Marshal(filtered)
	if err != nil {
		return nil, nil, err
	}
	partsList := make([]json.RawMessage, len(contents))
	for i := range contents {
		if i == last {
			partsList[i] = lastData
			continue
		}
		// Earlier segments keep it to their own text; non-text parts from the
		// model stream (reasoning, tool calls) belong to the run, not to any
		// single bubble.
		seg, err := json.Marshal([]llm.Part{{Type: "text", Text: contents[i]}})
		if err != nil {
			return nil, nil, err
		}
		partsList[i] = seg
	}
	return contents, partsList, nil
}
