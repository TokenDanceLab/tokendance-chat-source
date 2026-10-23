package storepg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"tokendancechat/backend/llm"
	"tokendancechat/backend/store"
	"tokendancechat/backend/websearch"
)

// ReserveWebCall persists the monetary reservation before provider dispatch.
func (s *Store) ReserveWebCall(runID, id, toolCallID, tool string, dailyCap int64) error {
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
	r, err := scanRun(tx.QueryRow(ctx,
		"SELECT "+runColumns+" FROM bot_runs WHERE id=$1 FOR UPDATE", runID))
	if err != nil || r.Status != "running" || !runSessionCurrent(ctx, tx, r) {
		return store.ErrRunUnavailable
	}
	var count int
	if err := tx.QueryRow(ctx,
		"SELECT COUNT(*) FROM bot_web_calls WHERE run_id=$1", runID).Scan(&count); err != nil {
		return err
	}
	var total int64
	if err := tx.QueryRow(ctx,
		"SELECT COALESCE(SUM(charged_micro_cny),0) FROM bot_web_calls WHERE budget_day=$1", r.BudgetDay).Scan(&total); err != nil {
		return err
	}
	if count >= websearch.MaxCalls || dailyCap <= 0 || total > dailyCap-websearch.PriceMicroCNY {
		return errors.New("web research budget exhausted")
	}
	budgetDay, err := parseBudgetDay(r.BudgetDay)
	if err != nil {
		return err
	}
	now := time.Now()
	_, err = tx.Exec(ctx,
		`INSERT INTO bot_web_calls (id, run_id, tool_call_id, tool, provider, attempt, price_version,
		                            budget_day, reserved_micro_cny, charged_micro_cny, status, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,'deepseek',1,$5,$6,$7,$7,'reserved',$8,$8)`,
		id, runID, toolCallID, tool, websearch.PriceVersion, budgetDay,
		websearch.PriceMicroCNY, now)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SettleWebCall settles a reservation exactly once.
func (s *Store) SettleWebCall(id, status string, unbilled bool) error {
	charged := websearch.PriceMicroCNY
	if unbilled {
		charged = 0
	}
	ctx, cancel := s.opCtx()
	defer cancel()
	_, err := s.pool.Exec(ctx,
		"UPDATE bot_web_calls SET status=$1, charged_micro_cny=$2, updated_at=$3 WHERE id=$4 AND status='reserved'",
		status, charged, time.Now(), id)
	return err
}

// SaveWebSource stores the bounded provider snapshot for a run.
func (s *Store) SaveWebSource(source store.WebSource) (store.WebSource, error) {
	if !websearch.SafeURL(source.URL) {
		return store.WebSource{}, errors.New("invalid source URL")
	}
	ctx, cancel := s.opCtx()
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.WebSource{}, err
	}
	defer tx.Rollback(ctx)

	r, err := scanRun(tx.QueryRow(ctx,
		"SELECT "+runColumns+" FROM bot_runs WHERE id=$1 FOR UPDATE", source.RunID))
	if err != nil || r.Status != "running" || !runSessionCurrent(ctx, tx, r) {
		return store.WebSource{}, store.ErrRunUnavailable
	}
	old, err := scanSource(tx.QueryRow(ctx,
		"SELECT "+sourceColumns+" FROM bot_web_sources WHERE run_id=$1 AND url=$2",
		source.RunID, source.URL))
	if err == nil {
		source.ID = old.ID
		// A later discovery or failed refresh does not erase already-read evidence.
		if source.Status == "discovered" || old.Status == "fetched" {
			return old, nil
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return store.WebSource{}, err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO bot_web_sources (id, run_id, url, title, status, content, content_hash, retrieved_at, truncated, error_code)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 ON CONFLICT (run_id, url) DO UPDATE SET
		   title = excluded.title,
		   status = excluded.status,
		   content = excluded.content,
		   content_hash = excluded.content_hash,
		   retrieved_at = excluded.retrieved_at,
		   truncated = excluded.truncated,
		   error_code = excluded.error_code`,
		source.ID, source.RunID, source.URL, source.Title, source.Status, source.Content,
		source.ContentHash, time.UnixMilli(source.RetrievedAt), source.Truncated, source.ErrorCode)
	if err != nil {
		return store.WebSource{}, err
	}
	return source, tx.Commit(ctx)
}

const sourceColumns = "id,run_id,url,title,status,content,content_hash,retrieved_at,truncated,error_code"

func scanSource(row interface{ Scan(...any) error }) (store.WebSource, error) {
	var (
		s         store.WebSource
		retrieved time.Time
	)
	err := row.Scan(&s.ID, &s.RunID, &s.URL, &s.Title, &s.Status, &s.Content,
		&s.ContentHash, &retrieved, &s.Truncated, &s.ErrorCode)
	if err != nil {
		return store.WebSource{}, err
	}
	s.RetrievedAt = retrieved.UnixMilli()
	return s, nil
}

// GetWebSource reads one run-scoped source.
func (s *Store) GetWebSource(runID, id string) (store.WebSource, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	return scanSource(s.pool.QueryRow(ctx,
		"SELECT "+sourceColumns+" FROM bot_web_sources WHERE run_id=$1 AND id=$2", runID, id))
}

// Consume malformed/unclosed reserved markers as well as valid references.
var citationPattern = regexp.MustCompile(`(?s)\[\[source:(.*?)(?:\]\]|$)`)

// finalWebParts is the only source publication path.
func finalWebParts(ctx context.Context, tx pgx.Tx, runID, content string, raw json.RawMessage) (string, json.RawMessage, error) {
	contents, partsList, err := finalWebPartsMulti(ctx, tx, runID, []string{content}, raw)
	if err != nil {
		return "", nil, err
	}
	return contents[0], partsList[0], nil
}

// finalWebPartsMulti is the multi-segment variant: citation numbering runs
// continuously across segments, and source DTOs ride on the last segment.
func finalWebPartsMulti(ctx context.Context, tx pgx.Tx, runID string, contents []string, raw json.RawMessage) ([]string, []json.RawMessage, error) {
	rows, err := tx.Query(ctx,
		"SELECT "+sourceColumns+" FROM bot_web_sources WHERE run_id=$1 ORDER BY seq LIMIT 12", runID)
	if err != nil {
		return nil, nil, err
	}
	var sources []store.WebSource
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
		filtered = append(filtered, llm.Part{
			Type: "source", SourceID: source.ID, Title: websearch.Clip(title, 200),
			URL: source.URL, Status: source.Status, Cited: used[source.ID] > 0,
		})
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
		seg, err := json.Marshal([]llm.Part{{Type: "text", Text: contents[i]}})
		if err != nil {
			return nil, nil, err
		}
		partsList[i] = seg
	}
	return contents, partsList, nil
}
