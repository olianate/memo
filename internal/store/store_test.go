package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/domain/markdown"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "memo.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func blocks(text string) []domain.Block {
	return []domain.Block{{ID: domain.NewID(), Type: domain.BlockParagraph, Spans: []domain.Span{{Text: text}}}}
}

func TestCreateMemoPersistsAndIndexes(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memo.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	memo, err := s.CreateMemo(ctx, MemoInput{Body: "hello 世界", Tags: []string{"Work", "ideas"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if memo.ID == "" || memo.Revision != 1 {
		t.Fatalf("memo = %+v", memo)
	}
	if memo.BodyText != "hello 世界" {
		t.Fatalf("body text = %q", memo.BodyText)
	}
	if len(memo.Tags) != 2 || memo.Tags[0].NormalizedName != "ideas" {
		t.Fatalf("tags = %+v", memo.Tags)
	}
	if _, err := s.OutboxForMemo(ctx, memo.ID); err != nil {
		t.Fatalf("outbox: %v", err)
	}
	_ = s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	again, err := s.GetMemo(ctx, memo.ID)
	if err != nil {
		t.Fatalf("reopen get: %v", err)
	}
	if again.BodyText != "hello 世界" || again.Revision != 1 || len(again.Tags) != 2 {
		t.Fatalf("persisted memo = %+v", again)
	}

	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM memos_fts WHERE body_text MATCH '"hello"'`).Scan(&count); err != nil {
		t.Fatalf("fts query: %v", err)
	}
	if count != 1 {
		t.Fatalf("fts count = %d", count)
	}
}

func TestUpdateMemoBumpsRevisionAndReindexes(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	memo, err := s.CreateMemo(ctx, MemoInput{Body: "old text", Tags: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpdateMemo(ctx, memo.ID, "new 中文 text", []string{"b"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 {
		t.Fatalf("revision = %d", updated.Revision)
	}
	if updated.BodyText != "new 中文 text" {
		t.Fatalf("body = %q", updated.BodyText)
	}
	if len(updated.Tags) != 1 || updated.Tags[0].NormalizedName != "b" {
		t.Fatalf("tags = %+v", updated.Tags)
	}
	if updated.ContentHash != domain.HashContent("new 中文 text", []string{"b"}) {
		t.Fatalf("hash = %q", updated.ContentHash)
	}
	item, err := s.OutboxForMemo(ctx, memo.ID)
	if err != nil || item.Revision != 2 {
		t.Fatalf("outbox = %+v, %v", item, err)
	}
}

func TestUpdateMemoMetadataRefreshesHashWithoutBody(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	memo, err := s.CreateMemo(ctx, MemoInput{Body: "body stays", Tags: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpdateMemoMetadata(ctx, memo.ID, []string{"b"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 || updated.Body != "body stays" {
		t.Fatalf("metadata update touched the body/revision: %+v", updated)
	}
	if updated.ContentHash != domain.HashContent("body stays", []string{"b"}) {
		t.Fatalf("hash = %q", updated.ContentHash)
	}

	if err := s.EvictBody(ctx, memo.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	evicted, err := s.GetMemo(ctx, memo.ID)
	if err != nil {
		t.Fatal(err)
	}
	evictedHash := evicted.ContentHash
	again, err := s.UpdateMemoMetadata(ctx, memo.ID, []string{"c"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if again.LocalState != domain.LocalEvicted || again.ContentHash != evictedHash {
		t.Fatalf("evicted metadata update = %+v", again)
	}
}

func TestMarkdownBodyIsVerbatimAndDerived(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	body := "# 标题\n\n- [x] 完成\n- [ ] 未完成  \n- 项目\n\n```go\nfmt.Println(\"hi\")  \n```\n\n| a | b |\n| - | - |\n\n<div>raw</div>\n\n尾随空格  \n"
	tags := []string{"Work", "work", "ideas"}
	memo, err := s.CreateMemo(ctx, MemoInput{Body: body, Tags: tags})
	if err != nil {
		t.Fatal(err)
	}
	if memo.Body != body {
		t.Fatalf("body changed on create:\n got %q\nwant %q", memo.Body, body)
	}
	if memo.ContentHash != domain.HashContent(body, tags) {
		t.Fatalf("hash = %q", memo.ContentHash)
	}
	if got := memo.BlockStats; got.Heading != 1 || got.Todo != 2 || got.TodoDone != 1 || got.List != 1 || got.Code != 1 || got.Paragraph != 4 {
		t.Fatalf("stats = %+v", got)
	} else if got.Words != 21 || got.Characters != 64 {
		t.Fatalf("word/character stats = %+v", got)
	}
	if memo.Snippet != domain.Snippet(memo.BodyText, 200) {
		t.Fatalf("snippet = %q, text = %q", memo.Snippet, memo.BodyText)
	}
	if memo.BodyText != domain.Text(markdown.Parse(body)) {
		t.Fatalf("derived text = %q", memo.BodyText)
	}

	next := "  leading space\n\n\n<div>still literal</div>  "
	updated, err := s.UpdateMemo(ctx, memo.ID, next, []string{"ideas"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Body != next || updated.ContentHash != domain.HashContent(next, []string{"ideas"}) {
		t.Fatalf("update body/hash = %q / %q", updated.Body, updated.ContentHash)
	}

	again, err := s.GetMemo(ctx, memo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Body != next {
		t.Fatalf("reopened body = %q, want %q", again.Body, next)
	}
}

func TestSoftDeleteRestoreAndViews(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	memo, _ := s.CreateMemo(ctx, MemoInput{Body: "tombstone me"})
	if err := s.SoftDelete(ctx, memo.ID); err != nil {
		t.Fatal(err)
	}
	active, _ := s.ListMemos(ctx, MemoFilter{})
	if len(active) != 0 {
		t.Fatalf("active = %d", len(active))
	}
	trash, _ := s.ListMemos(ctx, MemoFilter{Deleted: DeletedOnly})
	if len(trash) != 1 || trash[0].DeletedAt == nil {
		t.Fatalf("trash = %+v", trash)
	}
	item, _ := s.OutboxForMemo(ctx, memo.ID)
	if item != nil && item.Op != domain.OutboxDelete {
		t.Fatalf("outbox op = %q", item.Op)
	}
	if _, err := s.Restore(ctx, memo.ID); err != nil {
		t.Fatal(err)
	}
	active, _ = s.ListMemos(ctx, MemoFilter{})
	if len(active) != 1 {
		t.Fatalf("restored = %d", len(active))
	}
}

func TestPinnedMemosSortFirstAndPersist(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	older, err := s.CreateMemo(ctx, MemoInput{Body: "older", CreatedAt: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	newer, err := s.CreateMemo(ctx, MemoInput{Body: "newer", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.ListMemos(ctx, MemoFilter{})
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if list[0].ID != newer.ID {
		t.Fatalf("newest should be first before pinning: %+v", list)
	}
	revision := older.Revision
	if err := s.SetPinned(ctx, older.ID, true); err != nil {
		t.Fatal(err)
	}
	list, err = s.ListMemos(ctx, MemoFilter{})
	if err != nil || len(list) != 2 {
		t.Fatalf("list after pin = %+v, %v", list, err)
	}
	if list[0].ID != older.ID || !list[0].Pinned {
		t.Fatalf("pinned memo should lead the timeline: %+v", list)
	}
	stored, err := s.GetMemo(ctx, older.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Pinned {
		t.Fatal("pinned flag was not persisted")
	}
	if stored.Revision != revision {
		t.Fatalf("pinning bumped revision from %d to %d", revision, stored.Revision)
	}
	if err := s.SetPinned(ctx, older.ID, false); err != nil {
		t.Fatal(err)
	}
	list, err = s.ListMemos(ctx, MemoFilter{})
	if err != nil || len(list) != 2 || list[0].ID != newer.ID {
		t.Fatalf("list after unpin = %+v, %v", list, err)
	}
	if err := s.SetPinned(ctx, "missing", true); err != domain.ErrNotFound {
		t.Fatalf("missing pin error = %v", err)
	}
}

func TestPurgedPageMarkers(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	if err := s.MarkPagePurged(ctx, "page-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkPagePurged(ctx, "page-1"); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.IsPagePurged(ctx, "page-1"); err != nil || !ok {
		t.Fatalf("page marker = %v, %v", ok, err)
	}
	ids, err := s.PurgedPageIDs(ctx)
	if err != nil || len(ids) != 1 || ids[0] != "page-1" {
		t.Fatalf("ids = %+v, %v", ids, err)
	}
	if err := s.ClearPurgedPage(ctx, "page-1"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.IsPagePurged(ctx, "page-1"); ok {
		t.Fatal("page marker was not cleared")
	}
}

func TestDeleteMemoRemovesOutboxAndSearch(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	memo, _ := s.CreateMemo(ctx, MemoInput{Body: "delete me permanently"})
	if err := s.DeleteMemo(ctx, memo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMemo(ctx, memo.ID); err == nil {
		t.Fatal("memo row survived")
	}
	if item, _ := s.OutboxForMemo(ctx, memo.ID); item != nil {
		t.Fatalf("outbox survived: %+v", item)
	}
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM memos_fts WHERE memo_id=?`, memo.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("fts rows = %d", count)
	}
}

func TestRenameAndMergeTagsDoNotTouchBodies(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	memo, _ := s.CreateMemo(ctx, MemoInput{Body: "body stays", Tags: []string{"work", "job"}})
	before, _ := s.GetMemo(ctx, memo.ID)

	tags, _ := s.ListTags(ctx)
	var work, job domain.Tag
	for _, tag := range tags {
		switch tag.NormalizedName {
		case "work":
			work = tag.Tag
		case "job":
			job = tag.Tag
		}
	}
	if _, err := s.RenameTag(ctx, work.ID, "工作"); err != nil {
		t.Fatal(err)
	}
	if err := s.MergeTags(ctx, job.ID, work.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := s.GetMemo(ctx, memo.ID)
	if after.Revision != before.Revision || after.BodyText != before.BodyText {
		t.Fatalf("body/revision changed: %+v -> %+v", before, after)
	}
	if len(after.Tags) != 1 || after.Tags[0].Name != "工作" {
		t.Fatalf("tags = %+v", after.Tags)
	}
	tags, _ = s.ListTags(ctx)
	if len(tags) != 1 || tags[0].Count != 1 {
		t.Fatalf("tag counts = %+v", tags)
	}
}

func TestTagRenameReindexesAndQueuesPropertyPush(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	memo, _ := s.CreateMemo(ctx, MemoInput{Body: "body stays", Tags: []string{"work"}})
	if err := s.ClearOutbox(ctx, memo.ID); err != nil {
		t.Fatal(err)
	}
	old := time.Unix(1_700_000_000, 0).UTC()
	if _, err := s.DB().Exec(`UPDATE memos SET updated_at=? WHERE id=?`, toMS(old), memo.ID); err != nil {
		t.Fatal(err)
	}
	tags, _ := s.ListTags(ctx)
	if _, err := s.RenameTag(ctx, tags[0].ID, "career"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetMemo(ctx, memo.ID)
	if got.Revision != 1 || got.BodyText != "body stays" {
		t.Fatalf("rename touched the body/revision: %+v", got)
	}
	if !got.UpdatedAt.After(old) {
		t.Fatalf("rename did not advance updated_at: %v", got.UpdatedAt)
	}
	if len(got.Tags) != 1 || got.Tags[0].Name != "career" {
		t.Fatalf("tags = %+v", got.Tags)
	}
	if got.ContentHash != domain.HashContent("body stays", []string{"career"}) {
		t.Fatalf("tag rename left a stale hash: %q", got.ContentHash)
	}
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM memos_fts WHERE tags_text MATCH '"career"'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("tag reindex count = %d", count)
	}
	item, _ := s.OutboxForMemo(ctx, memo.ID)
	if item == nil || item.Revision != 1 || item.Op != domain.OutboxUpsert {
		t.Fatalf("property push not queued: %+v", item)
	}
}

func TestConditionalPushBookkeeping(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	memo, _ := s.CreateMemo(ctx, MemoInput{Body: "one"})
	item, _ := s.OutboxForMemo(ctx, memo.ID)
	if item == nil {
		t.Fatal("outbox missing")
	}
	time.Sleep(3 * time.Millisecond)
	if _, err := s.UpdateMemo(ctx, memo.ID, "two", nil, nil); err != nil {
		t.Fatal(err)
	}
	// The push of revision 1 must not clear the revision-2 row.
	if err := s.ClearOutboxIfUnchanged(ctx, memo.ID, item.Revision, item.CreatedAt); err != nil {
		t.Fatal(err)
	}
	remaining, _ := s.OutboxForMemo(ctx, memo.ID)
	if remaining == nil || remaining.Revision != 2 {
		t.Fatalf("newer outbox row was cleared: %+v", remaining)
	}
	// A stale base_revision must not advance past the local edit.
	if err := s.MarkSyncedIfCurrent(ctx, memo.ID, "page-1", 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	current, _ := s.GetMemo(ctx, memo.ID)
	if current.BaseRevision != 0 {
		t.Fatalf("base revision advanced to %d", current.BaseRevision)
	}
	// A current push updates it.
	if err := s.MarkSyncedIfCurrent(ctx, memo.ID, "page-1", 2, time.Now()); err != nil {
		t.Fatal(err)
	}
	current, _ = s.GetMemo(ctx, memo.ID)
	if current.BaseRevision != 2 {
		t.Fatalf("base revision = %d", current.BaseRevision)
	}
}

func TestOutboxCoalescesAndBacksOff(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	memo, _ := s.CreateMemo(ctx, MemoInput{Body: "one"})
	if _, err := s.UpdateMemo(ctx, memo.ID, "two", nil, nil); err != nil {
		t.Fatal(err)
	}
	items, err := s.DueOutbox(ctx, time.Now().Add(time.Second), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Revision != 2 || items[0].Attempts != 0 {
		t.Fatalf("outbox = %+v", items)
	}
	if err := s.FailOutbox(ctx, memo.ID, 1, time.Now().Add(time.Hour), "boom"); err != nil {
		t.Fatal(err)
	}
	items, _ = s.DueOutbox(ctx, time.Now(), 10)
	if len(items) != 0 {
		t.Fatalf("failed item should not be due: %+v", items)
	}
	if _, err := s.UpdateMemo(ctx, memo.ID, "three", nil, nil); err != nil {
		t.Fatal(err)
	}
	items, _ = s.DueOutbox(ctx, time.Now(), 10)
	if len(items) != 1 || items[0].Attempts != 0 || items[0].Revision != 3 {
		t.Fatalf("coalesced = %+v", items)
	}
}

func TestLegacyBlockProfileIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE memos (id TEXT PRIMARY KEY, body_json TEXT NOT NULL DEFAULT '[]')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO memos(id, body_json) VALUES('m1', '[{"type":"paragraph"}]')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 2`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	_, err = Open(path)
	if err == nil || !strings.Contains(err.Error(), "block JSON") {
		t.Fatalf("legacy profile error = %v, want a clear block-JSON rejection", err)
	}
}

func TestBackupRoundTripsMarkdownBody(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	body := "# 标题\n\n\n尾随空格  \n\n| a | b |\n| - | - |\n"
	memo, err := s.CreateMemo(ctx, MemoInput{Body: body})
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup.db")
	if _, err := s.DB().ExecContext(ctx, `VACUUM INTO ?`, filepath.ToSlash(backup)); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Close() })
	got, err := restored.GetMemo(ctx, memo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != body {
		t.Fatalf("restored body = %q, want %q", got.Body, body)
	}
}

func TestApplyRemoteDoesNotEnqueue(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	remote := RemoteMemo{
		ID:        domain.NewID(),
		PageID:    "page-1",
		CreatedAt: now.Add(-time.Hour),
		UpdatedAt: now,
		Revision:  4,
		Body:      "from notion",
		Tags:      []string{"remote"},
		SyncedAt:  now,
	}
	memo, err := s.ApplyRemote(ctx, remote, domain.LocalPresent)
	if err != nil {
		t.Fatal(err)
	}
	if memo.NotionPageID != "page-1" || memo.BaseRevision != 4 || memo.Revision != 4 {
		t.Fatalf("memo = %+v", memo)
	}
	item, _ := s.OutboxForMemo(ctx, memo.ID)
	if item != nil {
		t.Fatalf("remote apply created outbox row: %+v", item)
	}
	got, _ := s.GetMemoByPageID(ctx, "page-1")
	if got.ID != memo.ID {
		t.Fatalf("page lookup = %+v", got)
	}
}

func TestEvictionKeepsMetadataAndSearchText(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	memo, _ := s.CreateMemo(ctx, MemoInput{Body: "search still works"})
	if err := s.EvictBody(ctx, memo.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetMemo(ctx, memo.ID)
	if got.LocalState != domain.LocalEvicted || got.Body != "" {
		t.Fatalf("evicted memo = %+v", got)
	}
	if got.BodyText != "search still works" {
		t.Fatalf("body text lost: %q", got.BodyText)
	}
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM memos_fts WHERE body_text MATCH '"still"'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("fts count after eviction = %d", count)
	}
}

func TestSettingsAndDeviceState(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	if err := s.SetSetting(ctx, "theme", "dark"); err != nil {
		t.Fatal(err)
	}
	v, ok, err := s.Setting(ctx, "theme")
	if err != nil || !ok || v != "dark" {
		t.Fatalf("setting = %q %v %v", v, ok, err)
	}
	state, err := s.EnsureDevice(ctx, "device-1")
	if err != nil || state.DeviceID != "device-1" {
		t.Fatalf("device = %+v %v", state, err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.SetLastPull(ctx, now); err != nil {
		t.Fatal(err)
	}
	state, _ = s.DeviceState(ctx)
	if state.LastPullAt == nil || !state.LastPullAt.Equal(now) {
		t.Fatalf("last pull = %v, want %v", state.LastPullAt, now)
	}
}

func TestCounts(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	_, _ = s.CreateMemo(ctx, MemoInput{Body: "one", Tags: []string{"a"}})
	_, _ = s.CreateMemo(ctx, MemoInput{Body: "two", Tags: []string{"a", "b"}})
	notes, tags, days, err := s.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if notes != 2 || tags != 2 || days != 1 {
		t.Fatalf("counts = %d %d %d", notes, tags, days)
	}
}

func TestClearLogs(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	if err := s.Log(ctx, "info", "sync.push", "pushed"); err != nil {
		t.Fatal(err)
	}
	if err := s.Log(ctx, "error", "sync.pull", "failed"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearLogs(ctx); err != nil {
		t.Fatal(err)
	}
	if logs, err := s.Logs(ctx, 10); err != nil || len(logs) != 0 {
		t.Fatalf("logs = %+v %v", logs, err)
	}
	if errors, err := s.Errors(ctx, 10); err != nil || len(errors) != 0 {
		t.Fatalf("errors = %+v %v", errors, err)
	}
}
