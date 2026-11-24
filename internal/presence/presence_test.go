package presence

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/olianate/memo/internal/attachments"
	"github.com/olianate/memo/internal/config"
	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/domain/markdown"
	"github.com/olianate/memo/internal/store"
)

type env struct {
	store  *store.Store
	files  *attachments.Service
	now    time.Time
	events []string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "memo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &env{
		store: st,
		files: attachments.New(st, config.New(st), dir),
		now:   time.Now().UTC().Truncate(time.Millisecond),
	}
}

func (e *env) keeper(restore func(context.Context, string) error) *Keeper {
	return New(e.store, e.files, restore, func() time.Time { return e.now }, func(name string, data any) {
		e.events = append(e.events, name)
	})
}

func TestEnsureDoesNotRestoreAPresentBody(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	memo, err := e.store.CreateMemo(ctx, store.MemoInput{Body: "already here"})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	k := e.keeper(func(context.Context, string) error {
		calls++
		return errors.New("restore should not run")
	})
	got, err := k.Ensure(ctx, memo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "already here" || calls != 0 {
		t.Fatalf("body = %q, restore calls = %d", got.Body, calls)
	}
}

func TestEnsureRestoresAnEvictedBody(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	memo, err := e.store.CreateMemo(ctx, store.MemoInput{Body: "evicted text"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.EvictBody(ctx, memo.ID, e.now); err != nil {
		t.Fatal(err)
	}
	k := e.keeper(func(ctx context.Context, id string) error {
		return e.store.HydrateMemo(ctx, id, "archived", e.now)
	})
	got, err := k.Ensure(ctx, memo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "archived" || got.LocalState != domain.LocalPresent {
		t.Fatalf("memo = %+v", got)
	}
	if got.LastHydratedAt == nil {
		t.Fatal("last_hydrated_at was not recorded")
	}
}

func TestEnsureReportsUnavailableMetadata(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	memo, err := e.store.CreateMemo(ctx, store.MemoInput{Body: "gone"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.EvictBody(ctx, memo.ID, e.now); err != nil {
		t.Fatal(err)
	}
	k := e.keeper(func(context.Context, string) error { return errors.New("Notion is offline") })
	got, err := k.Ensure(ctx, memo.ID)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if got == nil || got.ID != memo.ID {
		t.Fatalf("metadata = %+v", got)
	}
}

func TestEvictClearsBytesAndKeepsSearch(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	memo, err := e.store.CreateMemo(ctx, store.MemoInput{Body: "search still works"})
	if err != nil {
		t.Fatal(err)
	}
	att, err := e.files.Add(ctx, memo.ID, "a.txt", "text/plain", []byte("bytes"))
	if err != nil {
		t.Fatal(err)
	}
	path := att.LocalPath
	body := markdown.AppendAttachment(memo.Body, domain.BlockFile, att.ID, att.OriginalName)
	if _, err := e.store.UpdateMemo(ctx, memo.ID, body, nil, nil); err != nil {
		t.Fatal(err)
	}
	k := e.keeper(nil)
	if err := k.Evict(ctx, memo.ID); err != nil {
		t.Fatal(err)
	}
	got, err := e.store.GetMemo(ctx, memo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LocalState != domain.LocalEvicted || got.Body != "" {
		t.Fatalf("evicted memo = %+v", got)
	}
	if got.BodyText != "search still works" {
		t.Fatalf("body text lost: %q", got.BodyText)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].Present || got.Attachments[0].LocalPath != "" {
		t.Fatalf("attachment still present: %+v", got.Attachments)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("attachment file still on disk: %v", err)
	}
	if err := k.Evict(ctx, memo.ID); err != nil {
		t.Fatalf("second eviction: %v", err)
	}
	if len(e.events) != 1 {
		t.Fatalf("events = %v, want one memos:changed", e.events)
	}
}

func TestMergeRemoteKeepsAnEvictedBodyAbsent(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	memo, err := e.store.CreateMemo(ctx, store.MemoInput{Body: "local"})
	if err != nil {
		t.Fatal(err)
	}
	k := e.keeper(nil)
	remote := store.RemoteMemo{
		ID: memo.ID, PageID: "page-1", CreatedAt: e.now.Add(-time.Hour), UpdatedAt: e.now,
		Revision: 2, Body: "remote text", Tags: []string{"remote"}, SyncedAt: e.now,
	}
	got, applied, err := k.MergeRemote(ctx, remote)
	if err != nil {
		t.Fatal(err)
	}
	if !applied || got.Body != "remote text" {
		t.Fatalf("present merge: applied=%v memo=%+v", applied, got)
	}

	if err := e.store.EvictBody(ctx, memo.ID, e.now); err != nil {
		t.Fatal(err)
	}
	remote.Revision = 3
	remote.Body = "newer remote text"
	remote.UpdatedAt = e.now.Add(time.Minute)
	got, applied, err = k.MergeRemote(ctx, remote)
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("a pull resurrected an evicted body")
	}
	if got.Body != "" || got.LocalState != domain.LocalEvicted {
		t.Fatalf("body = %q state = %q", got.Body, got.LocalState)
	}
	if got.BodyText != "newer remote text" {
		t.Fatalf("search text = %q", got.BodyText)
	}
	if got.Revision != 3 {
		t.Fatalf("revision = %d", got.Revision)
	}
}
