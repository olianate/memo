package stats

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/store"
)

func testStats(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(st), st
}

func TestCountWordsMixed(t *testing.T) {
	tests := []struct {
		text string
		want int
	}{
		{"hello world", 2},
		{"你好世界", 4},
		{"hello 世界 again", 4},
		{"", 0},
		{"2026-11-21", 3},
	}
	for _, tt := range tests {
		if got := CountWords(tt.text); got != tt.want {
			t.Errorf("CountWords(%q) = %d, want %d", tt.text, got, tt.want)
		}
	}
}

func TestDashboardAggregates(t *testing.T) {
	ctx := context.Background()
	svc, st := testStats(t)
	loc := time.Local
	nov := func(day, hour int) time.Time { return time.Date(2026, 11, day, hour, 0, 0, 0, loc) }

	first, err := st.CreateMemo(ctx, store.MemoInput{
		CreatedAt: nov(3, 9),
		Blocks: []domain.Block{
			{ID: domain.NewID(), Type: domain.BlockHeading, Level: 1, Spans: []domain.Span{{Text: "标题"}}},
			{ID: domain.NewID(), Type: domain.BlockParagraph, Spans: []domain.Span{{Text: "hello 世界"}}},
			{ID: domain.NewID(), Type: domain.BlockTodo, Checked: true, Spans: []domain.Span{{Text: "done task"}}},
			{ID: domain.NewID(), Type: domain.BlockTodo, Spans: []domain.Span{{Text: "open task"}}},
		},
		Tags: []string{"work"},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := st.CreateMemo(ctx, store.MemoInput{
		CreatedAt: nov(4, 22),
		Blocks:    []domain.Block{{ID: domain.NewID(), Type: domain.BlockQuote, Spans: []domain.Span{{Text: "quoted"}}}},
		Tags:      []string{"work", "life"},
	})
	_, _ = st.CreateMemo(ctx, store.MemoInput{
		CreatedAt: nov(3, 10),
		Blocks:    []domain.Block{{ID: domain.NewID(), Type: domain.BlockParagraph, Spans: []domain.Span{{Text: "third"}}}},
		Tags:      []string{"life"},
	})
	_, err = st.CreateAttachment(ctx, domain.Attachment{
		ID: domain.NewID(), MemoID: first.ID, Kind: domain.AttachmentImage,
		OriginalName: "a.png", MIME: "image/png", Bytes: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Evicted body must still contribute words and composition.
	if err := st.SetEvicted(ctx, second.ID); err != nil {
		t.Fatal(err)
	}

	d, err := svc.Dashboard(ctx, "2026-11", time.Date(2026, 11, 15, 12, 0, 0, 0, loc))
	if err != nil {
		t.Fatal(err)
	}
	if d.Totals.Notes != 3 || d.Totals.ActiveDays != 2 || d.Totals.Tags != 2 || d.Totals.Attachments != 1 {
		t.Fatalf("totals = %+v", d.Totals)
	}
	// 标题(2) + hello(1) 世界(2) + done(1) task(1) + open(1) task(1) = 9
	// quoted = 1, third = 1
	if d.Totals.Words != 11 {
		t.Fatalf("words = %d, want 11", d.Totals.Words)
	}
	if len(d.Daily) != 30 {
		t.Fatalf("daily points = %d", len(d.Daily))
	}
	var nov3 int
	for _, day := range d.Daily {
		if day.Date == "2026-11-03" {
			nov3 = day.Count
		}
	}
	if nov3 != 2 {
		t.Fatalf("Nov 3 count = %d", nov3)
	}
	comp := map[string]int{}
	for _, c := range d.Composition {
		comp[c.Type] = c.Count
	}
	if comp["paragraph"] != 2 || comp["heading"] != 1 || comp["todo"] != 2 || comp["quote"] != 1 {
		t.Fatalf("composition = %+v", comp)
	}
	if d.Todos.Total != 2 || d.Todos.Done != 1 {
		t.Fatalf("todos = %+v", d.Todos)
	}
	if d.Hours[9].Count != 1 || d.Hours[22].Count != 1 || d.Hours[10].Count != 1 {
		t.Fatalf("hours = %+v", d.Hours)
	}
	if len(d.TopTags) != 2 || d.TopTags[0].Name != "life" || d.TopTags[0].Count != 2 {
		t.Fatalf("top tags = %+v", d.TopTags)
	}
	if len(d.CoOccurrence) != 1 || d.CoOccurrence[0].A != "life" || d.CoOccurrence[0].B != "work" {
		t.Fatalf("co-occurrence = %+v", d.CoOccurrence)
	}
	if d.Health.Evicted != 1 || d.Health.Synced != 0 || d.Health.LocalOnly != 3 {
		t.Fatalf("health = %+v", d.Health)
	}
	if len(d.Year) != 365 {
		t.Fatalf("year points = %d", len(d.Year))
	}
}

func TestEmptyMonthZeroState(t *testing.T) {
	ctx := context.Background()
	svc, _ := testStats(t)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.Local)
	d, err := svc.Dashboard(ctx, "2026-11", now)
	if err != nil {
		t.Fatal(err)
	}
	if d.Totals.Notes != 0 || d.Totals.Words != 0 || d.Totals.Attachments != 0 {
		t.Fatalf("totals = %+v", d.Totals)
	}
	if len(d.Daily) != 30 || d.Daily[0].Count != 0 {
		t.Fatalf("daily = %+v", d.Daily[:1])
	}
	if len(d.TopTags) != 0 || len(d.CoOccurrence) != 0 {
		t.Fatalf("tags = %+v / %+v", d.TopTags, d.CoOccurrence)
	}
}

func TestMonthRangeValidation(t *testing.T) {
	if _, _, _, err := MonthRange("nope", time.Now()); err == nil {
		t.Fatal("expected invalid month error")
	}
	start, end, norm, err := MonthRange("2026-02", time.Now())
	if err != nil || start.Day() != 1 || end.Month() != time.March || norm != "2026-02" {
		t.Fatalf("range = %v %v %q %v", start, end, norm, err)
	}
}
