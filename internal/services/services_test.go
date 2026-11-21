package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"
	"time"

	"memo/internal/attachments"
	"memo/internal/config"
	"memo/internal/domain"
	"memo/internal/notion"
	"memo/internal/notion/fake"
	"memo/internal/search"
	"memo/internal/stats"
	"memo/internal/store"
	syncengine "memo/internal/sync"
)

type testSecrets struct{ token string }

func (s *testSecrets) NotionToken(context.Context) (string, error) { return s.token, nil }
func (s *testSecrets) SetNotionToken(_ context.Context, t string) error {
	s.token = t
	return nil
}
func (s *testSecrets) ClearNotionToken(context.Context) error { s.token = ""; return nil }

type testHooks struct {
	hotkey string
	login  bool
	opened string
}

func (h *testHooks) ApplyHotkey(a string) error     { h.hotkey = a; return nil }
func (h *testHooks) ApplyStartAtLogin(v bool) error { h.login = v; return nil }
func (h *testHooks) OpenPath(p string) error        { h.opened = p; return nil }

type testEnv struct {
	store    *store.Store
	cfg      *config.Store
	memos    *MemoService
	tags     *TagService
	search   *SearchService
	stats    *StatsService
	settings *SettingsService
	sync     *SyncService
	engine   *syncengine.Service
	secrets  *testSecrets
	hooks    *testHooks
	dir      string
}

func newServiceEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "svc.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	_, _ = st.EnsureDevice(context.Background(), "dev-test")
	cfg := config.New(st)
	kc := &testSecrets{}
	att := attachments.New(st, cfg, dir)
	engine := syncengine.New(st, cfg, kc, att)
	engine.SetDeviceID("dev-test")
	hooks := &testHooks{}
	emit := func(string, any) {}
	return &testEnv{
		store: st, cfg: cfg, dir: dir, secrets: kc, hooks: hooks, engine: engine,
		memos:    NewMemoService(st, engine, att, emit),
		tags:     NewTagService(st, emit),
		search:   NewSearchService(search.New(st)),
		stats:    NewStatsService(stats.New(st)),
		settings: NewSettingsService(st, cfg, dir, hooks, kc),
		sync:     NewSyncService(engine, cfg, st, kc, emit),
	}
}

func paragraph(text string) []domain.Block {
	return []domain.Block{{ID: domain.NewID(), Type: domain.BlockParagraph, Spans: []domain.Span{{Text: text}}}}
}

func TestMemoLifecycle(t *testing.T) {
	e := newServiceEnv(t)
	created, err := e.memos.Create(paragraph("hello"), []string{"work"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Day == "" || len(created.Blocks) != 1 || len(created.Tags) != 1 {
		t.Fatalf("created = %+v", created)
	}
	list, err := e.memos.List(FilterDTO{})
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v %v", list, err)
	}
	updated, err := e.memos.Update(created.ID, paragraph("edited"), []string{"life"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 || updated.Blocks[0].PlainText() != "edited" || !updated.KeepLocal {
		t.Fatalf("updated = %+v", updated)
	}
	got, err := e.memos.Get(created.ID)
	if err != nil || got.Blocks[0].PlainText() != "edited" {
		t.Fatalf("get = %+v %v", got, err)
	}
	if err := e.memos.Delete(created.ID); err != nil {
		t.Fatal(err)
	}
	active, _ := e.memos.List(FilterDTO{})
	if len(active) != 0 {
		t.Fatalf("active = %d", len(active))
	}
	trash, _ := e.memos.List(FilterDTO{Deleted: "deleted"})
	if len(trash) != 1 {
		t.Fatalf("trash = %d", len(trash))
	}
	restored, err := e.memos.Restore(created.ID)
	if err != nil || restored.Deleted {
		t.Fatalf("restore = %+v %v", restored, err)
	}
	counts, _ := e.memos.Counts()
	if counts.Notes != 1 || counts.Tags != 1 || counts.Days != 1 {
		t.Fatalf("counts = %+v", counts)
	}
}

func TestPurgeMemoAndEmptyTrash(t *testing.T) {
	e := newServiceEnv(t)
	ctx := context.Background()
	first, _ := e.memos.Create(paragraph("first"), nil)
	second, _ := e.memos.Create(paragraph("second"), nil)
	if err := e.store.SetNotionPageID(ctx, first.ID, "page-first"); err != nil {
		t.Fatal(err)
	}
	if err := e.memos.Delete(first.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.memos.Delete(second.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.memos.Purge(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.GetMemo(ctx, first.ID); err == nil {
		t.Fatal("purged memo still exists")
	}
	if item, _ := e.store.OutboxForMemo(ctx, first.ID); item != nil {
		t.Fatalf("outbox row survived purge: %+v", item)
	}
	purged, err := e.store.IsPagePurged(ctx, "page-first")
	if err != nil || !purged {
		t.Fatalf("page tombstone = %v, %v", purged, err)
	}

	if err := e.memos.Purge(second.ID); err != nil {
		t.Fatal(err)
	}
	// A live memo cannot be purged directly.
	third, _ := e.memos.Create(paragraph("third"), nil)
	if err := e.memos.Purge(third.ID); err == nil {
		t.Fatal("live memo was purged")
	}
	_ = e.memos.Delete(third.ID)

	count, err := e.memos.EmptyTrash()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("emptied = %d", count)
	}
	if trash, _ := e.memos.List(FilterDTO{Deleted: "deleted"}); len(trash) != 0 {
		t.Fatalf("trash after empty = %d", len(trash))
	}
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for y := 0; y < 30; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{R: 20, G: 180, B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAttachmentAddAppendsBlockAndRemove(t *testing.T) {
	e := newServiceEnv(t)
	memo, _ := e.memos.Create(paragraph("with image"), nil)
	encoded := base64.StdEncoding.EncodeToString(testPNG(t))
	att, err := e.memos.AddAttachmentBase64(memo.ID, "shot.png", "image/png", encoded)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := e.memos.Get(memo.ID)
	if len(got.Attachments) != 1 || got.Attachments[0].ID != att.ID {
		t.Fatalf("attachments = %+v", got.Attachments)
	}
	if len(got.Blocks) != 2 || got.Blocks[1].AttachmentID != att.ID || got.Blocks[1].Type != domain.BlockImage {
		t.Fatalf("blocks = %+v", got.Blocks)
	}
	thumb, err := e.memos.AttachmentThumb(att.ID)
	if err != nil || thumb == "" {
		t.Fatalf("thumb = %v", err)
	}
	if err := e.memos.RemoveAttachment(att.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := e.memos.Get(memo.ID)
	if len(after.Attachments) != 0 || len(after.Blocks) != 1 {
		t.Fatalf("after remove = %+v", after)
	}
}

func TestTagsRenameMergeAndSearch(t *testing.T) {
	e := newServiceEnv(t)
	first, _ := e.memos.Create(paragraph("alpha work note"), []string{"work", "job"})
	_, _ = e.memos.Create(paragraph("beta life note"), []string{"life"})
	tags, err := e.tags.List()
	if err != nil || len(tags) != 3 {
		t.Fatalf("tags = %+v %v", tags, err)
	}
	var work, job domain.Tag
	for _, tag := range tags {
		switch tag.Normalized {
		case "work":
			work = domain.Tag{ID: tag.ID, Name: tag.Name, NormalizedName: tag.Normalized}
		case "job":
			job = domain.Tag{ID: tag.ID, Name: tag.Name, NormalizedName: tag.Normalized}
		}
	}
	renamed, err := e.tags.Rename(work.ID, "工作")
	if err != nil || renamed.Name != "工作" {
		t.Fatalf("rename = %+v %v", renamed, err)
	}
	if err := e.tags.Merge(job.ID, work.ID); err != nil {
		t.Fatal(err)
	}
	memo, _ := e.memos.Get(first.ID)
	if len(memo.Tags) != 1 || memo.Tags[0].Name != "工作" {
		t.Fatalf("memo tags = %+v", memo.Tags)
	}
	results, err := e.search.Query("alpha", "", "", "")
	if err != nil || len(results) != 1 || results[0].Memo.ID != first.ID {
		t.Fatalf("search = %+v %v", results, err)
	}
	if results[0].Match != "alpha" {
		t.Fatalf("snippet = %+v", results[0])
	}
	suggest, _ := e.tags.Suggest("工")
	if len(suggest) != 1 {
		t.Fatalf("suggest = %+v", suggest)
	}
}

func TestSettingsApplyAndBackup(t *testing.T) {
	e := newServiceEnv(t)
	if err := e.settings.SetTheme("dark"); err != nil {
		t.Fatal(err)
	}
	if err := e.settings.SetHotkey("CmdOrCtrl+Alt+Space"); err != nil {
		t.Fatal(err)
	}
	if err := e.settings.SetStartAtLogin(true); err != nil {
		t.Fatal(err)
	}
	if err := e.settings.SetRetention(90, true); err != nil {
		t.Fatal(err)
	}
	if err := e.settings.SetRetention(12, false); err == nil {
		t.Fatal("invalid retention accepted")
	}
	got := e.settings.Get()
	if got.Theme != "dark" || got.Hotkey != "CmdOrCtrl+Alt+Space" || !got.StartAtLogin || got.RetentionDays != 90 || !got.ImmediateEviction {
		t.Fatalf("settings = %+v", got)
	}
	if e.hooks.hotkey != "CmdOrCtrl+Alt+Space" || !e.hooks.login {
		t.Fatalf("hooks = %+v", e.hooks)
	}
	_, _ = e.memos.Create(paragraph("backup me"), nil)
	backup, err := e.settings.ExportBackup()
	if err != nil {
		t.Fatal(err)
	}
	if backup.Path == "" || backup.Bytes == 0 {
		t.Fatalf("backup = %+v", backup)
	}
	if err := e.settings.RebuildIndex(); err != nil {
		t.Fatal(err)
	}
	if err := e.settings.CompleteOnboarding(); err != nil {
		t.Fatal(err)
	}
	if !e.settings.Get().Onboarded {
		t.Fatal("onboarding flag not set")
	}
	if logs, err := e.settings.ExportLogs(); err != nil || logs == "" {
		t.Fatalf("logs = %q %v", logs, err)
	}
	e.secrets.token = "tok"
	if err := e.settings.DeleteLocalData(); err != nil {
		t.Fatal(err)
	}
	if e.secrets.token != "" {
		t.Fatal("wipe did not clear the keychain token")
	}
	if list, _ := e.memos.List(FilterDTO{}); len(list) != 0 {
		t.Fatalf("wipe left memos: %d", len(list))
	}
	if e.cfg.Bool(context.Background(), config.KeyNotionConfigured) {
		t.Fatal("wipe left Notion configured")
	}
}

func TestSyncResolveArchiveTargetAcceptsLinks(t *testing.T) {
	e := newServiceEnv(t)
	gw := fake.New()
	const id = "0123456789abcdef0123456789abcdef"
	gw.SeedPage(notion.Page{
		ID:  id,
		URL: "https://www.notion.so/archive-" + id,
		Properties: map[string]notion.PropertyValue{
			notion.TitlePropertyName: {Type: "title", Title: []notion.RichText{{Type: "text", Text: &notion.TextContent{Content: "memo archive"}}}},
		},
	}, nil)
	const dsID = "abcdef0123456789abcdef0123456789"
	gw.SeedDataSource(notion.DataSource{
		ID:         dsID,
		DatabaseID: "11112222333344445555666677778888",
		Title:      "shared memo database",
		PropertyTypes: map[string]string{
			notion.TitlePropertyName: "title",
		},
	})
	e.sync.client = func(token string) notion.Gateway { return gw }

	target, err := e.sync.ResolveArchiveTarget("tok", "https://www.notion.so/archive-0123456789abcdef0123456789abcdef?pvs=4")
	if err != nil {
		t.Fatal(err)
	}
	if target.Kind != "page" || target.ID != id || target.Title != "memo archive" {
		t.Fatalf("page target = %+v", target)
	}

	db, err := e.sync.ResolveArchiveTarget("tok", "https://www.notion.so/shared-"+dsID)
	if err != nil {
		t.Fatal(err)
	}
	if db.Kind != "database" || db.ID != dsID || db.Title != "shared memo database" {
		t.Fatalf("database target = %+v", db)
	}

	const rowID = "fedcba9876543210fedcba9876543210"
	gw.SeedPage(notion.Page{
		ID:     rowID,
		Parent: notion.Parent{Type: "data_source_id"},
		Properties: map[string]notion.PropertyValue{
			notion.TitlePropertyName: {Type: "title", Title: []notion.RichText{{Type: "text", Text: &notion.TextContent{Content: "a row"}}}},
		},
	}, nil)
	if _, err := e.sync.ResolveArchiveTarget("tok", rowID); err == nil {
		t.Fatal("database row accepted as an archive target")
	}
}

func TestStatsDashboardBoundary(t *testing.T) {
	e := newServiceEnv(t)
	_, _ = e.memos.Create(paragraph("hello 世界"), []string{"work"})
	month := time.Now().Format("2006-01")
	dashboard, err := e.stats.Dashboard(month)
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.Totals.Notes != 1 || dashboard.Month != month {
		t.Fatalf("dashboard = %+v", dashboard)
	}
}

func TestSyncConnectWithFakeWorkspace(t *testing.T) {
	e := newServiceEnv(t)
	gw := fake.New()
	const parentID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	gw.SeedPage(notion.Page{
		ID:  parentID,
		URL: "https://notion.so/archive",
		Properties: map[string]notion.PropertyValue{
			notion.TitlePropertyName: {Type: "title", Title: []notion.RichText{{Type: "text", Text: &notion.TextContent{Content: "Archive"}}}},
		},
	}, nil)
	gw.SeedDataSource(notion.DataSource{
		ID: "abcdef0123456789abcdef0123456789", DatabaseID: "11112222333344445555666677778888",
		Title: "Shared database", PropertyTypes: map[string]string{notion.TitlePropertyName: "title"},
	})
	e.sync.client = func(token string) notion.Gateway { return gw }
	if _, err := e.memos.Create(paragraph("local first"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.sync.ValidateToken("tok"); err != nil {
		t.Fatal(err)
	}
	pages, err := e.sync.ParentPages("tok", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pages.Pages) != 1 || pages.Pages[0].ID != parentID {
		t.Fatalf("pages = %+v", pages)
	}
	if len(pages.Databases) != 1 || pages.Databases[0].Title != "Shared database" {
		t.Fatalf("databases = %+v", pages.Databases)
	}
	result, err := e.sync.Connect(ConnectRequest{Token: "tok", Kind: "page", TargetID: parentID, DatabaseName: "我的备忘"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Workspace.Name != "Test Workspace" || result.DataSourceID == "" || result.TitleProperty == "" {
		t.Fatalf("connect = %+v", result)
	}
	if !result.CreatedDatabase || result.DatabaseTitle != "我的备忘" {
		t.Fatalf("created database = %+v", result)
	}
	if e.secrets.token != "tok" {
		t.Fatal("token not stored in the keychain fake")
	}
	if !e.cfg.Bool(context.Background(), config.KeySyncEnabled) {
		t.Fatal("sync not enabled")
	}
	if _, ok, _ := e.store.Setting(context.Background(), config.KeyNotionDataSourceID); !ok {
		t.Fatal("data source not stored")
	}
	if err := e.sync.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if e.secrets.token != "" || e.cfg.Bool(context.Background(), config.KeyNotionConfigured) {
		t.Fatal("disconnect did not clear state")
	}
	// Local notes survive disconnect.
	if list, _ := e.memos.List(FilterDTO{}); len(list) != 1 {
		t.Fatalf("local notes = %d", len(list))
	}
}

func TestSyncConnectUsesSharedDatabase(t *testing.T) {
	e := newServiceEnv(t)
	gw := fake.New()
	const dsID = "abcdef0123456789abcdef0123456789"
	gw.SeedDataSource(notion.DataSource{
		ID:            dsID,
		DatabaseID:    "11112222333344445555666677778888",
		Title:         "已有 memo 数据库",
		PropertyTypes: map[string]string{notion.TitlePropertyName: "title"},
	})
	e.sync.client = func(token string) notion.Gateway { return gw }

	list, err := e.sync.ParentPages("tok", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Databases) != 1 || list.Databases[0].ID != dsID || list.Databases[0].Title != "已有 memo 数据库" {
		t.Fatalf("databases = %+v", list.Databases)
	}
	if gw.CallsOf("CreateMemoDatabase") != 0 {
		t.Fatal("database search created a database")
	}

	preview, err := e.sync.PreviewArchiveDatabase("tok", dsID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.TitleProperty != notion.TitlePropertyName || len(preview.Conflicts) != 0 || len(preview.MissingProperties) == 0 {
		t.Fatalf("preview = %+v", preview)
	}
	foundMemoID := false
	for _, prop := range preview.MissingProperties {
		if prop.Name == notion.PropMemoID {
			foundMemoID = true
		}
	}
	if !foundMemoID {
		t.Fatalf("Memo ID missing from preview: %+v", preview.MissingProperties)
	}
	if gw.CallsOf("UpdateDataSourceSchema") != 0 {
		t.Fatal("preview changed the schema")
	}

	if _, err := e.sync.Connect(ConnectRequest{Token: "tok", Kind: "database", TargetID: dsID}); err == nil {
		t.Fatal("connect changed the schema without confirmation")
	}
	if gw.CallsOf("UpdateDataSourceSchema") != 0 || e.secrets.token != "" {
		t.Fatal("unconfirmed connect modified state")
	}

	result, err := e.sync.Connect(ConnectRequest{
		Token: "tok", Kind: "database", TargetID: dsID, ConfirmSchemaChanges: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.CreatedDatabase {
		t.Fatal("existing database reported as created")
	}
	if result.DataSourceID != dsID || result.DatabaseTitle != "已有 memo 数据库" || result.TitleProperty != notion.TitlePropertyName {
		t.Fatalf("connect = %+v", result)
	}
	if gw.CallsOf("CreateMemoDatabase") != 0 {
		t.Fatal("connect created a database instead of using the shared one")
	}
	if gw.CallsOf("UpdateDataSourceSchema") != 1 {
		t.Fatalf("schema updates = %d", gw.CallsOf("UpdateDataSourceSchema"))
	}
	ds := gw.DataSources[dsID]
	if ds.PropertyTypes[notion.PropMemoID] != "rich_text" || ds.PropertyTypes[notion.PropTags] != "multi_select" {
		t.Fatalf("schema not completed: %+v", ds.PropertyTypes)
	}
	if e.cfg.Get(context.Background(), config.KeyNotionDataSourceID) != dsID {
		t.Fatal("data source not stored")
	}
	if e.secrets.token != "tok" {
		t.Fatal("token not stored in the keychain fake")
	}
}

func TestSyncConnectSharedDatabaseRejectsTypeConflict(t *testing.T) {
	e := newServiceEnv(t)
	gw := fake.New()
	const dsID = "abcdef0123456789abcdef0123456789"
	gw.SeedDataSource(notion.DataSource{
		ID:         dsID,
		DatabaseID: "11112222333344445555666677778888",
		Title:      "冲突数据库",
		PropertyTypes: map[string]string{
			notion.TitlePropertyName: "title",
			notion.PropTags:          "rich_text",
		},
	})
	e.sync.client = func(token string) notion.Gateway { return gw }
	preview, err := e.sync.PreviewArchiveDatabase("tok", dsID)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Conflicts) != 1 || preview.Conflicts[0].Name != notion.PropTags {
		t.Fatalf("conflicts = %+v", preview.Conflicts)
	}
	if _, err := e.sync.Connect(ConnectRequest{
		Token: "tok", Kind: "database", TargetID: dsID, ConfirmSchemaChanges: true,
	}); err == nil {
		t.Fatal("type conflict was not reported")
	}
	if e.cfg.Bool(context.Background(), config.KeyNotionConfigured) {
		t.Fatal("failed connect marked Notion configured")
	}
}

func TestSyncConnectReadySharedDatabaseNeedsNoConfirmation(t *testing.T) {
	e := newServiceEnv(t)
	gw := fake.New()
	const dsID = "abcdef0123456789abcdef0123456789"
	propertyTypes := map[string]string{notion.TitlePropertyName: "title"}
	for name, schema := range notion.AppSchema(notion.TitlePropertyName) {
		propertyTypes[name] = notion.PropertyTypeName(schema)
	}
	gw.SeedDataSource(notion.DataSource{
		ID: dsID, DatabaseID: "11112222333344445555666677778888",
		Title: "完整数据库", PropertyTypes: propertyTypes,
	})
	e.sync.client = func(token string) notion.Gateway { return gw }
	preview, err := e.sync.PreviewArchiveDatabase("tok", dsID)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.MissingProperties) != 0 || len(preview.Conflicts) != 0 {
		t.Fatalf("preview = %+v", preview)
	}
	if _, err := e.sync.Connect(ConnectRequest{Token: "tok", Kind: "database", TargetID: dsID}); err != nil {
		t.Fatal(err)
	}
	if gw.CallsOf("UpdateDataSourceSchema") != 0 {
		t.Fatal("ready database was patched")
	}
}
