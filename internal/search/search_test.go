package search

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"memo/internal/domain"
	"memo/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func paragraph(text string) []domain.Block {
	return []domain.Block{{ID: domain.NewID(), Type: domain.BlockParagraph, Spans: []domain.Span{{Text: text}}}}
}

func TestChineseAndEnglishQueries(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	svc := New(st)
	_, _ = st.CreateMemo(ctx, store.MemoInput{Blocks: paragraph("今天开会讨论产品路线图"), Tags: []string{"工作"}})
	_, _ = st.CreateMemo(ctx, store.MemoInput{Blocks: paragraph("hello world from the memo app")})

	results, err := svc.Search(ctx, Options{Query: "产品路线"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("3-char Chinese = %d", len(results))
	}
	results, _ = svc.Search(ctx, Options{Query: "hello"})
	if len(results) != 1 {
		t.Fatalf("English = %d", len(results))
	}
}

func TestShortQueryFallsBackToLike(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	svc := New(st)
	_, _ = st.CreateMemo(ctx, store.MemoInput{Blocks: paragraph("今天开会讨论产品路线图")})
	results, err := svc.Search(ctx, Options{Query: "开会"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("2-char Chinese = %d", len(results))
	}
	if results[0].Snippet.Match != "开会" {
		t.Fatalf("snippet = %+v", results[0].Snippet)
	}
}

func TestFiltersAndDeleted(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	svc := New(st)
	old, _ := st.CreateMemo(ctx, store.MemoInput{Blocks: paragraph("alpha note"), Tags: []string{"work"}})
	_, _ = st.CreateMemo(ctx, store.MemoInput{Blocks: paragraph("alpha other"), Tags: []string{"life"}})
	_, _ = st.CreateMemo(ctx, store.MemoInput{Blocks: paragraph("beta note"), Tags: []string{"work"}})

	results, _ := svc.Search(ctx, Options{Query: "alpha", Tag: "work"})
	if len(results) != 1 || results[0].Memo.ID != old.ID {
		t.Fatalf("tag filter = %+v", results)
	}

	future := time.Now().Add(time.Hour)
	results, _ = svc.Search(ctx, Options{Query: "alpha", From: &future})
	if len(results) != 0 {
		t.Fatalf("date filter = %d", len(results))
	}

	_ = st.SoftDelete(ctx, old.ID)
	results, _ = svc.Search(ctx, Options{Query: "alpha"})
	if len(results) != 1 {
		t.Fatalf("deleted leaked into search: %d", len(results))
	}
}

func TestRebuildIndex(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	svc := New(st)
	_, _ = st.CreateMemo(ctx, store.MemoInput{Blocks: paragraph("rebuild me later")})
	if err := st.RebuildIndex(ctx); err != nil {
		t.Fatal(err)
	}
	results, _ := svc.Search(ctx, Options{Query: "rebuild"})
	if len(results) != 1 {
		t.Fatalf("after rebuild = %d", len(results))
	}
}

func TestSnippetHighlight(t *testing.T) {
	sn := makeSnippet("before the needle after the needle", "needle")
	if sn.Before == "" || sn.Match != "needle" || sn.After == "" {
		t.Fatalf("snippet = %+v", sn)
	}
}
