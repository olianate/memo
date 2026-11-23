// Package search queries the local FTS5 index, falling back to LIKE for one-
// and two-character queries (common in Chinese) because trigram needs three.
package search

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/store"
)

// Service searches memos and tags. The index is local and survives eviction.
type Service struct {
	store *store.Store
}

// New returns a search service over the local store.
func New(s *store.Store) *Service { return &Service{store: s} }

// Options is a plain-text query plus the timeline filters.
type Options struct {
	Query string
	Tag   string // normalized or display name
	From  *time.Time
	To    *time.Time
	Limit int
}

// Snippet splits a result around the matched term so the frontend can render
// <mark> without injecting HTML.
type Snippet struct {
	Before string `json:"before"`
	Match  string `json:"match"`
	After  string `json:"after"`
}

// Result pairs a memo with its display snippet.
type Result struct {
	Memo    domain.Memo `json:"memo"`
	Snippet Snippet     `json:"snippet"`
}

// Search returns active memos, newest first.
func (s *Service) Search(ctx context.Context, opts Options) ([]Result, error) {
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return nil, nil
	}
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	where := []string{"m.deleted_at IS NULL"}
	args := []any{}
	var matchArg string
	useFTS := utf8.RuneCountInString(query) >= 3
	if useFTS {
		matchArg = `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
	} else {
		like := "%" + escapeLike(query) + "%"
		where = append(where, `(m.body_text LIKE ? ESCAPE '\' OR EXISTS (
			SELECT 1 FROM memo_tags mt JOIN tags t ON t.id=mt.tag_id
			WHERE mt.memo_id=m.id AND t.name LIKE ? ESCAPE '\'))`)
		args = append(args, like, like)
	}
	if opts.Tag != "" {
		where = append(where, `EXISTS (SELECT 1 FROM memo_tags mt JOIN tags t ON t.id=mt.tag_id
			WHERE mt.memo_id=m.id AND t.normalized_name=?)`)
		args = append(args, domain.NormalizeTag(opts.Tag))
	}
	if opts.From != nil {
		where = append(where, "m.created_at >= ?")
		args = append(args, opts.From.UTC().UnixMilli())
	}
	if opts.To != nil {
		where = append(where, "m.created_at < ?")
		args = append(args, opts.To.UTC().UnixMilli())
	}

	var rows []string
	if useFTS {
		q := `SELECT m.id FROM memos_fts f JOIN memos m ON m.id=f.memo_id
			WHERE memos_fts MATCH ? AND ` + strings.Join(where, " AND ") +
			` ORDER BY m.created_at DESC, m.id DESC LIMIT ?`
		queryArgs := append([]any{matchArg}, args...)
		queryArgs = append(queryArgs, limit)
		r, err := s.store.DB().QueryContext(ctx, q, queryArgs...)
		if err != nil {
			return nil, fmt.Errorf("search: fts: %w", err)
		}
		rows, err = scanIDs(r)
		if err != nil {
			return nil, err
		}
	} else {
		q := `SELECT m.id FROM memos m WHERE ` + strings.Join(where, " AND ") +
			` ORDER BY m.created_at DESC, m.id DESC LIMIT ?`
		queryArgs := append(args, limit)
		r, err := s.store.DB().QueryContext(ctx, q, queryArgs...)
		if err != nil {
			return nil, fmt.Errorf("search: like: %w", err)
		}
		rows, err = scanIDs(r)
		if err != nil {
			return nil, err
		}
	}

	results := make([]Result, 0, len(rows))
	for _, id := range rows {
		memo, err := s.store.GetMemo(ctx, id)
		if err != nil {
			continue
		}
		results = append(results, Result{Memo: *memo, Snippet: makeSnippet(memo.BodyText, query)})
	}
	return results, nil
}

func scanIDs(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}) ([]string, error) {
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func makeSnippet(text, query string) Snippet {
	runes := []rune(text)
	if len(runes) == 0 {
		return Snippet{}
	}
	lowerText := strings.ToLower(text)
	lowerQuery := strings.ToLower(query)
	idx := strings.Index(lowerText, lowerQuery)
	if idx < 0 {
		end := len(runes)
		if end > 160 {
			end = 160
		}
		before := string(runes[:end])
		if end < len(runes) {
			before += "…"
		}
		return Snippet{Before: before}
	}
	// Convert byte offset to rune offset.
	runeStart := utf8.RuneCountInString(text[:idx])
	runeEnd := runeStart + utf8.RuneCountInString(query)
	start := runeStart - 60
	if start < 0 {
		start = 0
	}
	end := runeEnd + 90
	if end > len(runes) {
		end = len(runes)
	}
	sn := Snippet{
		Before: string(runes[start:runeStart]),
		Match:  string(runes[runeStart:runeEnd]),
		After:  string(runes[runeEnd:end]),
	}
	if start > 0 {
		sn.Before = "…" + sn.Before
	}
	if end < len(runes) {
		sn.After += "…"
	}
	return sn
}
