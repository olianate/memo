package memoedit_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olianate/memo/internal/attachments"
	"github.com/olianate/memo/internal/config"
	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/memoedit"
	"github.com/olianate/memo/internal/presence"
	"github.com/olianate/memo/internal/store"
)

type env struct {
	store  *store.Store
	editor *memoedit.Editor
	files  *attachments.Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "edit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := config.New(st)
	files := attachments.New(st, cfg, dir)
	return &env{
		store:  st,
		files:  files,
		editor: memoedit.New(st, files, presence.New(st, files, nil, nil, nil), nil),
	}
}

func (e *env) withHydrate(hydrate func(context.Context, string) error) {
	e.editor = memoedit.New(e.store, e.files, presence.New(e.store, e.files, hydrate, nil, nil), nil)
}

func textFile(name, body string) memoedit.File {
	return memoedit.File{Name: name, MIME: "text/plain", Data: []byte(body)}
}

func TestCreateWithFilesIsOneRevision(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	memo, err := e.editor.Create(ctx, memoedit.Draft{
		Body:  "with file",
		Tags:  []string{"#work", "two words", ""},
		Files: []memoedit.File{textFile("notes.txt", "hello")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if memo.Revision != 1 {
		t.Fatalf("revision = %d", memo.Revision)
	}
	if len(memo.Tags) != 1 || memo.Tags[0].Name != "work" {
		t.Fatalf("tags = %+v", memo.Tags)
	}
	if len(memo.Attachments) != 1 {
		t.Fatalf("attachments = %+v", memo.Attachments)
	}
	att := memo.Attachments[0]
	if !strings.Contains(memo.Body, "attachment://"+att.ID) {
		t.Fatalf("body = %q", memo.Body)
	}
	if _, err := os.Stat(att.LocalPath); err != nil {
		t.Fatalf("file was not written: %v", err)
	}
}

func TestEditBodyAndTagsIsOneRevision(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	memo, err := e.editor.Create(ctx, memoedit.Draft{Body: "first", Tags: []string{"work"}})
	if err != nil {
		t.Fatal(err)
	}
	body := "second"
	tags := []string{"life"}
	keep := true
	updated, err := e.editor.Edit(ctx, memo.ID, memoedit.Change{Body: &body, Tags: &tags, KeepLocal: &keep})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 || updated.Body != "second" || !updated.KeepLocal {
		t.Fatalf("updated = %+v", updated)
	}
	if len(updated.Tags) != 1 || updated.Tags[0].Name != "life" {
		t.Fatalf("tags = %+v", updated.Tags)
	}
}

func TestNoOpEditDoesNotBumpRevision(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	memo, _ := e.editor.Create(ctx, memoedit.Draft{Body: "same", Tags: []string{"work"}})
	body := memo.Body
	tags := []string{"work"}
	keep := memo.KeepLocal
	result, err := e.editor.Edit(ctx, memo.ID, memoedit.Change{Body: &body, Tags: &tags, KeepLocal: &keep})
	if err != nil {
		t.Fatal(err)
	}
	if result.Revision != 1 {
		t.Fatalf("no-op bumped revision to %d", result.Revision)
	}
}

func TestMetadataEditKeepsBodyEvicted(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	memo, _ := e.editor.Create(ctx, memoedit.Draft{Body: "archived", Tags: []string{"work"}})
	if err := e.store.EvictBody(ctx, memo.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	tags := []string{"life"}
	updated, err := e.editor.Edit(ctx, memo.ID, memoedit.Change{Tags: &tags})
	if err != nil {
		t.Fatal(err)
	}
	if updated.LocalState != domain.LocalEvicted || updated.Body != "" {
		t.Fatalf("metadata edit resurrected the body: %+v", updated)
	}
	if updated.Revision != 2 || len(updated.Tags) != 1 || updated.Tags[0].Name != "life" {
		t.Fatalf("updated = %+v", updated)
	}
}

func TestBodyEditRefusesWhenHydrationFails(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	memo, _ := e.editor.Create(ctx, memoedit.Draft{Body: "archived"})
	if err := e.store.EvictBody(ctx, memo.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	body := "rewritten"
	if _, err := e.editor.Edit(ctx, memo.ID, memoedit.Change{Body: &body}); !errors.Is(err, memoedit.ErrEvicted) {
		t.Fatalf("err = %v, want ErrEvicted", err)
	}
}

func TestBodyEditHydratesBeforeWriting(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	memo, _ := e.editor.Create(ctx, memoedit.Draft{Body: "archived"})
	if err := e.store.EvictBody(ctx, memo.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	e.withHydrate(func(ctx context.Context, id string) error {
		return e.store.HydrateMemo(ctx, id, "archived", time.Now().UTC())
	})
	body := "rewritten"
	updated, err := e.editor.Edit(ctx, memo.ID, memoedit.Change{Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Body != "rewritten" || updated.LocalState != domain.LocalPresent {
		t.Fatalf("updated = %+v", updated)
	}
}

func TestRemoveAttachmentDeletesRowAndLine(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	memo, err := e.editor.Create(ctx, memoedit.Draft{Body: "keep", Files: []memoedit.File{textFile("notes.txt", "hi")}})
	if err != nil {
		t.Fatal(err)
	}
	att := memo.Attachments[0]
	updated, err := e.editor.Edit(ctx, memo.ID, memoedit.Change{RemoveAttachmentIDs: []string{att.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Attachments) != 0 || strings.Contains(updated.Body, "attachment://") {
		t.Fatalf("updated = %+v", updated)
	}
	if _, err := os.Stat(att.LocalPath); !os.IsNotExist(err) {
		t.Fatalf("attachment file still exists: %v", err)
	}
}

func TestRemoveRejectsForeignKey(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, _ := e.editor.Create(ctx, memoedit.Draft{Body: "owner", Files: []memoedit.File{textFile("a.txt", "a")}})
	other, _ := e.editor.Create(ctx, memoedit.Draft{Body: "other"})
	_, err := e.editor.Edit(ctx, other.ID, memoedit.Change{RemoveAttachmentIDs: []string{owner.Attachments[0].ID}})
	if !errors.Is(err, memoedit.ErrAttachmentNotOwned) {
		t.Fatalf("err = %v, want ErrAttachmentNotOwned", err)
	}
}

func TestPreserveAttachmentsOnContentEdit(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	memo, _ := e.editor.Create(ctx, memoedit.Draft{Body: "old", Files: []memoedit.File{textFile("a.txt", "a")}})
	attID := memo.Attachments[0].ID
	body := "new body"
	updated, err := e.editor.Edit(ctx, memo.ID, memoedit.Change{Body: &body, PreserveAttachments: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Attachments) != 1 || !strings.Contains(updated.Body, "attachment://"+attID) {
		t.Fatalf("attachment was dropped: %+v", updated)
	}
}

func TestUnmentionedAttachmentBecomesLoose(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	memo, _ := e.editor.Create(ctx, memoedit.Draft{Body: "old", Files: []memoedit.File{textFile("a.txt", "a")}})
	body := "new body"
	updated, err := e.editor.Edit(ctx, memo.ID, memoedit.Change{Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Attachments) != 1 || strings.Contains(updated.Body, "attachment://") {
		t.Fatalf("loose attachment was not kept: %+v", updated)
	}
}

func TestEditOnMissingMemo(t *testing.T) {
	e := newEnv(t)
	body := "x"
	if _, err := e.editor.Edit(context.Background(), "missing", memoedit.Change{Body: &body}); !errors.Is(err, memoedit.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestBodyEditWithoutPresenceIsRefused(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	memo, err := e.editor.Create(ctx, memoedit.Draft{Body: "before"})
	if err != nil {
		t.Fatal(err)
	}
	editor := memoedit.New(e.store, e.files, nil, nil)
	body := "after"
	if _, err := editor.Edit(ctx, memo.ID, memoedit.Change{Body: &body}); !errors.Is(err, memoedit.ErrEvicted) {
		t.Fatalf("err = %v, want ErrEvicted", err)
	}
	current, err := e.store.GetMemo(ctx, memo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Body != "before" {
		t.Fatalf("body = %q, want it untouched", current.Body)
	}
}
