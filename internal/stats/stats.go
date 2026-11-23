// Package stats computes the metadata-driven dashboard from local SQLite. It
// never calls Notion and works after bodies are evicted because body_text and
// metadata are retained.
package stats

import (
	"context"
	"fmt"
	"time"
	"unicode"

	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/store"
)

// Service builds dashboard snapshots.
type Service struct{ store *store.Store }

// New returns a statistics service.
func New(s *store.Store) *Service { return &Service{store: s} }

// Totals are the five headline numbers.
type Totals struct {
	Notes       int `json:"notes"`
	Words       int `json:"words"`
	Tags        int `json:"tags"`
	Attachments int `json:"attachments"`
	ActiveDays  int `json:"active_days"`
}

// Day is one point in the daily chart or yearly heatmap.
type Day struct {
	Date  string `json:"date"` // YYYY-MM-DD
	Count int    `json:"count"`
}

// Composition counts blocks by type.
type Composition struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// Bucket is one word-count band.
type Bucket struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// CoOccurrence is one symmetric tag pair count.
type CoOccurrence struct {
	A     string `json:"a"`
	B     string `json:"b"`
	Count int    `json:"count"`
}

// Hour is one hour-of-day bucket (local time).
type Hour struct {
	Hour  int `json:"hour"`
	Count int `json:"count"`
}

// Todos is monthly todo completion.
type Todos struct {
	Total int `json:"total"`
	Done  int `json:"done"`
}

// AttachmentStats separates images and files.
type AttachmentStats struct {
	Images int   `json:"images"`
	Files  int   `json:"files"`
	Bytes  int64 `json:"bytes"`
}

// Health is local/sync/storage state over all active memos.
type Health struct {
	LocalOnly int `json:"local_only"`
	Synced    int `json:"synced"`
	Evicted   int `json:"evicted"`
	Pending   int `json:"pending"`
}

// Dashboard is the full statistics screen payload.
type Dashboard struct {
	Month        string           `json:"month"`
	Totals       Totals           `json:"totals"`
	Daily        []Day            `json:"daily"`
	Year         []Day            `json:"year"`
	Composition  []Composition    `json:"composition"`
	WordBuckets  []Bucket         `json:"word_buckets"`
	TopTags      []store.TagCount `json:"top_tags"`
	CoOccurrence []CoOccurrence   `json:"co_occurrence"`
	Hours        []Hour           `json:"hours"`
	Todos        Todos            `json:"todos"`
	Attachments  AttachmentStats  `json:"attachments"`
	Health       Health           `json:"health"`
}

// MonthRange returns the local-month bounds for a YYYY-MM selector.
func MonthRange(month string, now time.Time) (start, end time.Time, normalized string, err error) {
	if month == "" {
		now = now.Local()
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	} else {
		parsed, parseErr := time.ParseInLocation("2006-01", month, time.Local)
		if parseErr != nil {
			return time.Time{}, time.Time{}, "", fmt.Errorf("month must be YYYY-MM")
		}
		start = parsed
	}
	end = start.AddDate(0, 1, 0)
	return start, end, start.Format("2006-01"), nil
}

// Dashboard computes the selected month plus the trailing year.
func (s *Service) Dashboard(ctx context.Context, month string, now time.Time) (*Dashboard, error) {
	start, end, normalized, err := MonthRange(month, now)
	if err != nil {
		return nil, err
	}
	d := &Dashboard{Month: normalized, Daily: []Day{}, Year: []Day{}, TopTags: []store.TagCount{},
		CoOccurrence: []CoOccurrence{}, Composition: []Composition{}, WordBuckets: []Bucket{}}

	// Month memos.
	type memoRow struct {
		id         string
		createdAt  time.Time
		blockStats string
		bodyText   string
	}
	var memos []memoRow
	rows, err := s.store.DB().QueryContext(ctx, `SELECT id, created_at, block_stats, body_text FROM memos
		WHERE deleted_at IS NULL AND created_at>=? AND created_at<? ORDER BY created_at`, start.UnixMilli(), end.UnixMilli())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m memoRow
		var created int64
		if err := rows.Scan(&m.id, &created, &m.blockStats, &m.bodyText); err != nil {
			rows.Close()
			return nil, err
		}
		m.createdAt = time.UnixMilli(created).Local()
		memos = append(memos, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	dayCounts := map[string]int{}
	activeDays := map[string]bool{}
	wordTotal := 0
	composition := map[string]int{}
	buckets := []int{0, 0, 0, 0, 0, 0}
	todos := Todos{}
	hours := make([]int, 24)
	tagSet := map[string]bool{}
	att := AttachmentStats{}

	for _, m := range memos {
		date := m.createdAt.Format("2006-01-02")
		dayCounts[date]++
		activeDays[date] = true
		hours[m.createdAt.Hour()]++

		words := CountWords(m.bodyText)
		wordTotal += words
		switch {
		case words <= 50:
			buckets[0]++
		case words <= 100:
			buckets[1]++
		case words <= 200:
			buckets[2]++
		case words <= 400:
			buckets[3]++
		case words <= 800:
			buckets[4]++
		default:
			buckets[5]++
		}

		bs := domain.UnmarshalBlockStats(m.blockStats)
		composition["paragraph"] += bs.Paragraph
		composition["heading"] += bs.Heading
		composition["list"] += bs.List
		composition["todo"] += bs.Todo
		composition["quote"] += bs.Quote
		composition["code"] += bs.Code
		composition["attachment"] += bs.Attachment
		todos.Total += bs.Todo
		todos.Done += bs.TodoDone
	}

	// Daily points for the month.
	for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
		date := day.Format("2006-01-02")
		d.Daily = append(d.Daily, Day{Date: date, Count: dayCounts[date]})
	}

	// Year heatmap: trailing 365 local days ending today.
	today := time.Date(now.Local().Year(), now.Local().Month(), now.Local().Day(), 0, 0, 0, 0, time.Local)
	yearStart := today.AddDate(0, 0, -364)
	yearEnd := today.AddDate(0, 0, 1)
	yearCounts, err := s.dailyCounts(ctx, yearStart, yearEnd)
	if err != nil {
		return nil, err
	}
	for day := yearStart; day.Before(yearEnd); day = day.AddDate(0, 0, 1) {
		date := day.Format("2006-01-02")
		d.Year = append(d.Year, Day{Date: date, Count: yearCounts[date]})
	}

	d.Totals = Totals{Notes: len(memos), Words: wordTotal, ActiveDays: len(activeDays)}
	for _, key := range []string{"paragraph", "heading", "list", "todo", "quote", "code", "attachment"} {
		d.Composition = append(d.Composition, Composition{Type: key, Count: composition[key]})
	}
	labels := []string{"0–50", "51–100", "101–200", "201–400", "401–800", "800+"}
	for i, label := range labels {
		d.WordBuckets = append(d.WordBuckets, Bucket{Label: label, Count: buckets[i]})
	}
	d.Todos = todos
	for h := 0; h < 24; h++ {
		d.Hours = append(d.Hours, Hour{Hour: h, Count: hours[h]})
	}

	// Month tags and attachment counts.
	rows, err = s.store.DB().QueryContext(ctx, `SELECT t.id, t.name, t.normalized_name, COUNT(mt.memo_id)
		FROM tags t JOIN memo_tags mt ON mt.tag_id=t.id JOIN memos m ON m.id=mt.memo_id
		WHERE m.deleted_at IS NULL AND m.created_at>=? AND m.created_at<?
		GROUP BY t.id ORDER BY COUNT(mt.memo_id) DESC, t.name COLLATE NOCASE`, start.UnixMilli(), end.UnixMilli())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var tag store.TagCount
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.NormalizedName, &tag.Count); err != nil {
			rows.Close()
			return nil, err
		}
		d.TopTags = append(d.TopTags, tag)
		tagSet[tag.NormalizedName] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	d.Totals.Tags = len(tagSet)

	rows, err = s.store.DB().QueryContext(ctx, `SELECT a.kind, COUNT(*), COALESCE(SUM(a.bytes),0)
		FROM attachments a JOIN memos m ON m.id=a.memo_id
		WHERE m.deleted_at IS NULL AND m.created_at>=? AND m.created_at<?
		GROUP BY a.kind`, start.UnixMilli(), end.UnixMilli())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var kind string
		var n int
		var bytes int64
		if err := rows.Scan(&kind, &n, &bytes); err != nil {
			rows.Close()
			return nil, err
		}
		if kind == string(domain.AttachmentImage) {
			att.Images = n
		} else {
			att.Files = n
		}
		att.Bytes += bytes
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	d.Attachments = att
	d.Totals.Attachments = att.Images + att.Files

	// Co-occurrence over the month's memos.
	d.CoOccurrence, err = s.coOccurrence(ctx, start, end)
	if err != nil {
		return nil, err
	}

	// Health over all active memos.
	row := s.store.DB().QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN notion_page_id='' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN notion_page_id<>'' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN local_state='evicted' THEN 1 ELSE 0 END),0)
		FROM memos WHERE deleted_at IS NULL`)
	_ = row.Scan(&d.Health.LocalOnly, &d.Health.Synced, &d.Health.Evicted)
	_ = s.store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox`).Scan(&d.Health.Pending)

	return d, nil
}

func (s *Service) dailyCounts(ctx context.Context, from, to time.Time) (map[string]int, error) {
	rows, err := s.store.DB().QueryContext(ctx, `SELECT created_at FROM memos
		WHERE deleted_at IS NULL AND created_at>=? AND created_at<?`, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var ms int64
		if err := rows.Scan(&ms); err != nil {
			return nil, err
		}
		out[time.UnixMilli(ms).Local().Format("2006-01-02")]++
	}
	return out, rows.Err()
}

func (s *Service) coOccurrence(ctx context.Context, start, end time.Time) ([]CoOccurrence, error) {
	rows, err := s.store.DB().QueryContext(ctx, `SELECT a.normalized_name, b.normalized_name, COUNT(*)
		FROM memo_tags ma
		JOIN tags a ON a.id=ma.tag_id
		JOIN memo_tags mb ON mb.memo_id=ma.memo_id AND mb.tag_id<>ma.tag_id
		JOIN tags b ON b.id=mb.tag_id
		JOIN memos m ON m.id=ma.memo_id
		WHERE m.deleted_at IS NULL AND m.created_at>=? AND m.created_at<? AND a.normalized_name<b.normalized_name
		GROUP BY a.normalized_name, b.normalized_name
		ORDER BY COUNT(*) DESC, a.normalized_name, b.normalized_name`, start.UnixMilli(), end.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CoOccurrence{}
	for rows.Next() {
		var c CoOccurrence
		if err := rows.Scan(&c.A, &c.B, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CountWords counts CJK characters individually and Latin-script runs as words.
func CountWords(text string) int {
	count := 0
	inWord := false
	for _, r := range text {
		switch {
		case isCJK(r):
			count++
			inWord = false
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if !inWord {
				count++
				inWord = true
			}
		default:
			inWord = false
		}
	}
	return count
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}
