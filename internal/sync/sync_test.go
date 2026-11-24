package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olianate/memo/internal/config"
	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/notion"
	"github.com/olianate/memo/internal/notion/fake"
	"github.com/olianate/memo/internal/store"
)

type memSecrets struct{ token string }

func (m *memSecrets) NotionToken(context.Context) (string, error) { return m.token, nil }
func (m *memSecrets) SetNotionToken(_ context.Context, t string) error {
	m.token = t
	return nil
}
func (m *memSecrets) ClearNotionToken(context.Context) error { m.token = ""; return nil }

type testImporter struct {
	store *store.Store
	dir   string
	saved int
}

func (i *testImporter) SaveRemote(ctx context.Context, memoID, filename, mime string, kind domain.AttachmentKind, data []byte) (*domain.Attachment, error) {
	i.saved++
	if filename == "" {
		filename = "attachment.bin"
	}
	dir := filepath.Join(i.dir, memoID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	id := domain.NewID()
	path := filepath.Join(dir, id+"-"+filepath.Base(filename))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, err
	}
	return i.store.CreateAttachment(ctx, domain.Attachment{
		ID: id, MemoID: memoID, Kind: kind, OriginalName: filename, LocalPath: path,
		MIME: mime, Bytes: int64(len(data)),
	})
}

func (i *testImporter) DeleteFiles(_ context.Context, a domain.Attachment) error {
	if a.LocalPath != "" {
		_ = os.Remove(a.LocalPath)
	}
	if a.ThumbPath != "" {
		_ = os.Remove(a.ThumbPath)
	}
	return nil
}

type env struct {
	store  *store.Store
	cfg    *config.Store
	fake   *fake.Gateway
	svc    *Service
	imp    *testImporter
	device string
	now    *time.Time
}

func newEnv(t *testing.T, device string, shared *fake.Gateway) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), device+".db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := config.New(st)
	ctx := context.Background()
	fw := shared
	if fw == nil {
		fw = fake.New()
	}
	ds := &notion.DataSource{ID: "ds-1", DatabaseID: "db-1", Title: "memo", PropertyTypes: map[string]string{
		"名称": "title", notion.PropMemoID: "rich_text", notion.PropCreated: "date", notion.PropUpdated: "date",
		notion.PropRevision: "number", notion.PropDevice: "rich_text", notion.PropTags: "multi_select",
		notion.PropStatus: "select", notion.PropSchemaVersion: "number",
	}}
	fw.DataSources["ds-1"] = ds
	_ = cfg.SetBool(ctx, config.KeySyncEnabled, true)
	_ = cfg.SetBool(ctx, config.KeyNotionConfigured, true)
	_ = cfg.Set(ctx, config.KeyNotionDataSourceID, "ds-1")
	_ = cfg.Set(ctx, config.KeyNotionTitleProp, "名称")
	_ = cfg.Set(ctx, config.KeyWorkspaceFileLimit, "5242880")
	_ = cfg.Set(ctx, config.KeyDeviceID, device)
	_, _ = st.EnsureDevice(ctx, device)

	imp := &testImporter{store: st, dir: t.TempDir()}
	// Keep the service clock slightly ahead of real time so rows written by the
	// store (which uses time.Now) are due in the same test step.
	now := time.Now().UTC().Add(time.Minute)
	svc := New(st, cfg, &memSecrets{token: "tok"}, imp)
	svc.SetGateway(fw)
	svc.SetDeviceID(device)
	svc.SetClock(func() time.Time { return now })
	return &env{store: st, cfg: cfg, fake: fw, svc: svc, imp: imp, device: device, now: &now}
}

func TestPushCreateAdoptsPageAndClearsOutbox(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-a", nil)
	memo, err := e.store.CreateMemo(ctx, store.MemoInput{Body: "hello archive", Tags: []string{"work"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.PushOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := e.store.GetMemo(ctx, memo.ID)
	if got.NotionPageID == "" {
		t.Fatal("page id not stored")
	}
	item, _ := e.store.OutboxForMemo(ctx, memo.ID)
	if item != nil {
		t.Fatalf("outbox still pending: %+v", item)
	}
	page, ok := e.fake.Pages[got.NotionPageID]
	if !ok {
		t.Fatalf("page missing: %+v", e.fake.Pages)
	}
	if notion.MemoIDFromPage(*page) != memo.ID || notion.RevisionFromPage(*page) != 1 {
		t.Fatalf("page properties = %+v", page.Properties)
	}
	if notion.DeletedFromPage(*page) {
		t.Fatal("new page should be active")
	}
	if len(e.fake.Children[got.NotionPageID]) != 1 {
		t.Fatalf("children = %+v", e.fake.Children[got.NotionPageID])
	}
	options := e.fake.DataSources["ds-1"].Options[notion.PropTags]
	found := false
	for _, option := range options {
		if option.Name == "work" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing tag option: %+v", options)
	}
}

func TestPullRejectsUnsafeMemoID(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-b", nil)
	now := time.Now().UTC()
	e.fake.SeedPage(notion.Page{
		ID: "page-evil", CreatedTime: now.Add(-time.Hour), LastEditedTime: now,
		Properties: map[string]notion.PropertyValue{
			"名称":                {Type: "title", Title: []notion.RichText{{PlainText: "evil"}}},
			notion.PropMemoID:   {Type: "rich_text", RichText: []notion.RichText{{PlainText: "../../escape"}}},
			notion.PropCreated:  {Type: "date", Date: &notion.DateValue{Start: now.Add(-time.Hour).Format(time.RFC3339)}},
			notion.PropUpdated:  {Type: "date", Date: &notion.DateValue{Start: now.Format(time.RFC3339)}},
			notion.PropRevision: {Type: "number", Number: float64Ptr(1)},
			notion.PropStatus:   {Type: "select", Select: &notion.Option{Name: notion.StatusActive}},
		},
	}, nil)
	if err := e.svc.FullScan(ctx); err == nil {
		t.Fatal("expected unsafe Memo ID to be rejected")
	}
	if _, err := e.store.GetMemo(ctx, "../../escape"); err == nil {
		t.Fatal("unsafe memo was created")
	}
}

func TestPushUpdateReplacesBodyAndBumpsRevision(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-a", nil)
	memo, _ := e.store.CreateMemo(ctx, store.MemoInput{Body: "one"})
	if _, err := e.svc.PushOnce(ctx); err != nil {
		t.Fatal(err)
	}
	pageID := func() string {
		m, _ := e.store.GetMemo(ctx, memo.ID)
		return m.NotionPageID
	}()
	if _, err := e.store.UpdateMemo(ctx, memo.ID, "two updated", []string{"tag"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.PushOnce(ctx); err != nil {
		t.Fatal(err)
	}
	page := e.fake.Pages[pageID]
	if notion.RevisionFromPage(*page) != 2 {
		t.Fatalf("revision = %d", notion.RevisionFromPage(*page))
	}
	children := e.fake.Children[pageID]
	if len(children) != 1 || len(children[0].Paragraph.RichText) == 0 {
		t.Fatalf("children = %+v", children)
	}
	if children[0].Paragraph.RichText[0].Text.Content != "two updated" {
		t.Fatalf("child text = %+v", children[0].Paragraph.RichText)
	}
	if e.fake.CallsOf("UpdatePage") == 0 {
		t.Fatal("UpdatePage was not called")
	}
}

func TestPushBatchesLongBody(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-a", nil)
	paragraphs := make([]string, 150)
	for i := range paragraphs {
		paragraphs[i] = fmt.Sprintf("paragraph %d", i)
	}
	memo, err := e.store.CreateMemo(ctx, store.MemoInput{Body: strings.Join(paragraphs, "\n\n")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.PushOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := e.store.GetMemo(ctx, memo.ID)
	if len(e.fake.Children[got.NotionPageID]) != 150 {
		t.Fatalf("children = %d", len(e.fake.Children[got.NotionPageID]))
	}
	if e.fake.CallsOf("AppendChildren") != 1 {
		t.Fatalf("append calls = %d", e.fake.CallsOf("AppendChildren"))
	}
}

func TestTagOnlyChangePushesPropertiesWithoutReplacingBody(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-a", nil)
	memo, _ := e.store.CreateMemo(ctx, store.MemoInput{Body: "tagged body", Tags: []string{"work"}})
	if _, err := e.svc.PushOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := e.store.GetMemo(ctx, memo.ID)
	pageID := got.NotionPageID
	deletesBefore := e.fake.CallsOf("DeleteBlock")
	tags, _ := e.store.ListTags(ctx)
	if _, err := e.store.RenameTag(ctx, tags[0].ID, "career"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.PushOnce(ctx); err != nil {
		t.Fatal(err)
	}
	page := e.fake.Pages[pageID]
	names := []string{}
	for _, option := range page.Properties[notion.PropTags].MultiSelect {
		names = append(names, option.Name)
	}
	if len(names) != 1 || names[0] != "career" {
		t.Fatalf("page tags = %+v", names)
	}
	if e.fake.CallsOf("DeleteBlock") != deletesBefore {
		t.Fatal("tag-only push replaced the body")
	}
	if len(e.fake.Children[pageID]) != 1 {
		t.Fatalf("children = %d", len(e.fake.Children[pageID]))
	}
}

func TestPushDeleteWritesTombstone(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-a", nil)
	memo, _ := e.store.CreateMemo(ctx, store.MemoInput{Body: "delete me"})
	if _, err := e.svc.PushOnce(ctx); err != nil {
		t.Fatal(err)
	}
	pageID := e.fake.Pages
	if len(pageID) != 1 {
		t.Fatalf("pages = %d", len(pageID))
	}
	if err := e.store.SoftDelete(ctx, memo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.PushOnce(ctx); err != nil {
		t.Fatal(err)
	}
	for _, p := range e.fake.Pages {
		if !notion.DeletedFromPage(*p) {
			t.Fatalf("status not deleted: %+v", p.Properties)
		}
		if p.InTrash {
			t.Fatal("normal delete must not trash the page")
		}
	}
}

func TestPurgedPageIsSkippedByFullScan(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-a", nil)
	now := time.Now().UTC()
	pageID := "page-purged"
	e.fake.SeedPage(notion.Page{
		ID: pageID, CreatedTime: now.Add(-time.Hour), LastEditedTime: now,
		Properties: map[string]notion.PropertyValue{
			"名称":              {Type: "title", Title: []notion.RichText{{Type: "text", PlainText: "should stay deleted"}}},
			notion.PropMemoID: {Type: "rich_text", RichText: []notion.RichText{{Type: "text", PlainText: "memo-purged"}}},
			notion.PropStatus: {Type: "select", Select: &notion.Option{Name: notion.StatusActive}},
		},
	}, nil)
	if err := e.store.MarkPagePurged(ctx, pageID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.FullScan(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.GetMemo(ctx, "memo-purged"); err == nil {
		t.Fatal("purged page was pulled back")
	}
}

func TestPushFlushesPurgedPageTombstone(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-a", nil)
	pageID := "page-purged"
	e.fake.SeedPage(notion.Page{ID: pageID, Properties: map[string]notion.PropertyValue{}}, nil)
	if err := e.store.MarkPagePurged(ctx, pageID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.PushOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if purged, _ := e.store.IsPagePurged(ctx, pageID); purged {
		t.Fatal("tombstone marker was not cleared")
	}
	if !notion.DeletedFromPage(*e.fake.Pages[pageID]) {
		t.Fatal("purged page was not tombstoned")
	}
}

func TestPushRetriesThenSucceedsWithoutDuplicate(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-a", nil)
	memo, _ := e.store.CreateMemo(ctx, store.MemoInput{Body: "retry"})
	fail := true
	e.fake.OnCall = func(method string) error {
		if method == "CreatePage" && fail {
			return notion.StatusError(500, "internal_server_error", "boom")
		}
		return nil
	}
	if _, err := e.svc.PushOnce(ctx); err == nil {
		t.Fatal("expected push failure")
	}
	item, _ := e.store.OutboxForMemo(ctx, memo.ID)
	if item == nil || item.Attempts != 1 || item.LastError == "" {
		t.Fatalf("outbox = %+v", item)
	}
	fail = false
	*e.now = (*e.now).Add(2 * time.Minute)
	if _, err := e.svc.PushOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.fake.Pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(e.fake.Pages))
	}
	item, _ = e.store.OutboxForMemo(ctx, memo.ID)
	if item != nil {
		t.Fatalf("outbox = %+v", item)
	}
}

func TestPush503CommitAdoptsPage(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-a", nil)
	memo, _ := e.store.CreateMemo(ctx, store.MemoInput{Body: "commit"})
	e.fake.OnCall = func(method string) error {
		if method == "CreatePage" {
			return &notion.APIError{Status: 503, Code: "service_unavailable", CommittedResourceID: "page-99"}
		}
		return nil
	}
	if _, err := e.svc.PushOnce(ctx); err == nil {
		t.Fatal("expected 503")
	}
	got, _ := e.store.GetMemo(ctx, memo.ID)
	if got.NotionPageID != "page-99" {
		t.Fatalf("page id = %q", got.NotionPageID)
	}
	item, _ := e.store.OutboxForMemo(ctx, memo.ID)
	if item == nil {
		t.Fatal("outbox should remain for the follow-up update")
	}
}

func TestFullScanImportsRemoteMemos(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-b", nil)
	now := time.Now().UTC()
	pageID := "page-seed"
	e.fake.SeedPage(notion.Page{
		ID: pageID, CreatedTime: now.Add(-time.Hour), LastEditedTime: now,
		Properties: map[string]notion.PropertyValue{
			"名称":                {Type: "title", Title: []notion.RichText{{Type: "text", PlainText: "remote"}}},
			notion.PropMemoID:   {Type: "rich_text", RichText: []notion.RichText{{Type: "text", PlainText: "memo-remote"}}},
			notion.PropCreated:  {Type: "date", Date: &notion.DateValue{Start: now.Add(-time.Hour).Format(time.RFC3339)}},
			notion.PropUpdated:  {Type: "date", Date: &notion.DateValue{Start: now.Format(time.RFC3339)}},
			notion.PropRevision: {Type: "number", Number: float64Ptr(3)},
			notion.PropTags:     {Type: "multi_select", MultiSelect: []notion.Option{{Name: "remote-tag"}}},
			notion.PropStatus:   {Type: "select", Select: &notion.Option{Name: notion.StatusActive}},
		},
	}, []notion.Block{{Object: "block", Type: "paragraph", Paragraph: &notion.TextBlock{RichText: []notion.RichText{{Type: "text", PlainText: "pulled body"}}}}})
	if err := e.svc.FullScan(ctx); err != nil {
		t.Fatal(err)
	}
	memo, err := e.store.GetMemo(ctx, "memo-remote")
	if err != nil {
		t.Fatal(err)
	}
	if memo.Revision != 3 || len(memo.Tags) != 1 || memo.BodyText != "pulled body" || memo.NotionPageID != pageID {
		t.Fatalf("memo = %+v", memo)
	}
	st, _ := e.store.DeviceState(ctx)
	if st.LastFullScanAt == nil || st.LastPullAt == nil {
		t.Fatalf("device state = %+v", st)
	}
}

func TestStatusReturnsToIdleAfterPull(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-b", nil)
	var last *Status
	sawSyncing := false
	e.svc.OnEvent(func(name string, data any) {
		if name != "sync:status" {
			return
		}
		st, ok := data.(Status)
		if !ok {
			t.Fatalf("status payload = %T", data)
		}
		if st.State == domain.SyncSyncing {
			sawSyncing = true
		}
		last = &st
	})
	if err := e.svc.PullOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if !sawSyncing {
		t.Fatal("expected a syncing status event during the pull")
	}
	if last == nil || last.State != domain.SyncIdle || last.Pending != 0 {
		t.Fatalf("last status = %+v", last)
	}
}

func TestPullConflictRevisionThenUpdatedThenDevice(t *testing.T) {
	now := time.Now().UTC()
	if !RemoteWins(1, now, "a", 2, now.Add(-time.Hour), "b") {
		t.Fatal("higher revision should win")
	}
	if RemoteWins(2, now, "a", 1, now.Add(time.Hour), "b") {
		t.Fatal("lower revision should lose")
	}
	if !RemoteWins(2, now, "a", 2, now.Add(time.Minute), "b") {
		t.Fatal("newer updated should win on equal revisions")
	}
	if RemoteWins(2, now, "b", 2, now, "a") {
		t.Fatal("equal timestamps: lexicographically lower device should lose")
	}
	if !RemoteWins(2, now, "a", 2, now, "b") {
		t.Fatal("equal timestamps: higher device should win")
	}
}

func TestPullAppliesHigherRevisionAndDeletion(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-b", nil)
	now := time.Now().UTC()
	pageID := "page-conflict"
	props := map[string]notion.PropertyValue{
		"名称":                {Type: "title", Title: []notion.RichText{{PlainText: "x"}}},
		notion.PropMemoID:   {Type: "rich_text", RichText: []notion.RichText{{PlainText: "memo-1"}}},
		notion.PropCreated:  {Type: "date", Date: &notion.DateValue{Start: now.Add(-time.Hour).Format(time.RFC3339)}},
		notion.PropUpdated:  {Type: "date", Date: &notion.DateValue{Start: now.Format(time.RFC3339)}},
		notion.PropRevision: {Type: "number", Number: float64Ptr(5)},
		notion.PropDevice:   {Type: "rich_text", RichText: []notion.RichText{{PlainText: "dev-a"}}},
		notion.PropStatus:   {Type: "select", Select: &notion.Option{Name: notion.StatusActive}},
	}
	e.fake.SeedPage(notion.Page{ID: pageID, CreatedTime: now.Add(-time.Hour), LastEditedTime: now, Properties: props},
		[]notion.Block{{Object: "block", Type: "paragraph", Paragraph: &notion.TextBlock{RichText: []notion.RichText{{Type: "text", PlainText: "remote body"}}}}})
	local, _ := e.store.CreateMemo(ctx, store.MemoInput{ID: "memo-1", Body: "local body"})
	_ = e.store.MarkSynced(ctx, local.ID, pageID, 1, now.Add(-2*time.Hour))
	_ = e.store.ClearOutbox(ctx, local.ID)
	_ = e.store.SetLastPull(ctx, now.Add(-time.Hour))

	if err := e.svc.PullOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := e.store.GetMemo(ctx, "memo-1")
	if got.Revision != 5 || got.BodyText != "remote body" {
		t.Fatalf("memo = %+v", got)
	}
	item, _ := e.store.OutboxForMemo(ctx, "memo-1")
	if item != nil {
		t.Fatalf("remote win should drop outbox: %+v", item)
	}

	props[notion.PropStatus] = notion.PropertyValue{Type: "select", Select: &notion.Option{Name: notion.StatusDeleted}}
	props[notion.PropRevision] = notion.PropertyValue{Type: "number", Number: float64Ptr(6)}
	props[notion.PropUpdated] = notion.PropertyValue{Type: "date", Date: &notion.DateValue{Start: now.Add(time.Minute).Format(time.RFC3339)}}
	e.fake.Pages[pageID].Properties = props
	_ = e.store.SetLastPull(ctx, now)
	if err := e.svc.PullOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ = e.store.GetMemo(ctx, "memo-1")
	if got.DeletedAt == nil {
		t.Fatal("tombstone not applied")
	}
}

func TestPullKeepsNewerLocalEdit(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-b", nil)
	now := time.Now().UTC()
	pageID := "page-local-newer"
	props := map[string]notion.PropertyValue{
		"名称":                {Type: "title", Title: []notion.RichText{{PlainText: "x"}}},
		notion.PropMemoID:   {Type: "rich_text", RichText: []notion.RichText{{PlainText: "memo-local"}}},
		notion.PropCreated:  {Type: "date", Date: &notion.DateValue{Start: now.Add(-2 * time.Hour).Format(time.RFC3339)}},
		notion.PropUpdated:  {Type: "date", Date: &notion.DateValue{Start: now.Add(-time.Hour).Format(time.RFC3339)}},
		notion.PropRevision: {Type: "number", Number: float64Ptr(2)},
		notion.PropDevice:   {Type: "rich_text", RichText: []notion.RichText{{PlainText: "dev-a"}}},
		notion.PropStatus:   {Type: "select", Select: &notion.Option{Name: notion.StatusActive}},
	}
	e.fake.SeedPage(notion.Page{ID: pageID, CreatedTime: now.Add(-2 * time.Hour), LastEditedTime: now.Add(-time.Hour), Properties: props},
		[]notion.Block{{Object: "block", Type: "paragraph", Paragraph: &notion.TextBlock{RichText: []notion.RichText{{Type: "text", PlainText: "older remote"}}}}})
	local, _ := e.store.CreateMemo(ctx, store.MemoInput{ID: "memo-local", Body: "draft"})
	_, _ = e.store.UpdateMemo(ctx, local.ID, "still draft", nil, nil)
	_, _ = e.store.UpdateMemo(ctx, local.ID, "newer local edit", nil, nil)
	_ = e.store.MarkSynced(ctx, local.ID, pageID, 3, now)
	_ = e.store.ClearOutbox(ctx, local.ID)
	_ = e.store.SetLastPull(ctx, now.Add(-2*time.Hour))

	if err := e.svc.PullOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := e.store.GetMemo(ctx, "memo-local")
	if got.Body != "newer local edit" || got.Revision != 3 {
		t.Fatalf("older remote overwrote the local edit: %+v", got)
	}
}

func float64Ptr(v float64) *float64 { return &v }

func TestPullKeepsUnsupportedBlocksAsMarkdown(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-b", nil)
	now := time.Now().UTC()
	pageID := "page-unsupported"
	props := map[string]notion.PropertyValue{
		"名称":                {Type: "title", Title: []notion.RichText{{PlainText: "x"}}},
		notion.PropMemoID:   {Type: "rich_text", RichText: []notion.RichText{{PlainText: "memo-unsupported"}}},
		notion.PropCreated:  {Type: "date", Date: &notion.DateValue{Start: now.Add(-time.Hour).Format(time.RFC3339)}},
		notion.PropUpdated:  {Type: "date", Date: &notion.DateValue{Start: now.Format(time.RFC3339)}},
		notion.PropRevision: {Type: "number", Number: float64Ptr(1)},
		notion.PropDevice:   {Type: "rich_text", RichText: []notion.RichText{{PlainText: "dev-a"}}},
		notion.PropStatus:   {Type: "select", Select: &notion.Option{Name: notion.StatusActive}},
	}
	blocks := []notion.Block{
		{Object: "block", Type: "paragraph", Paragraph: &notion.TextBlock{RichText: []notion.RichText{{Type: "text", PlainText: "remote body"}}}},
		{Object: "block", Type: "callout", Raw: json.RawMessage(`{"object":"block","type":"callout","callout":{"rich_text":[{"type":"text","plain_text":"note"}]}}`)},
		{Object: "block", Type: "column_list", HasChildren: true, Raw: json.RawMessage(`{"object":"block","type":"column_list","has_children":true,"column_list":{}}`)},
	}
	e.fake.SeedPage(notion.Page{ID: pageID, CreatedTime: now.Add(-time.Hour), LastEditedTime: now, Properties: props}, blocks)
	_ = e.store.SetLastPull(ctx, now.Add(-time.Hour))

	if err := e.svc.PullOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := e.store.GetMemo(ctx, "memo-unsupported")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Body, "remote body") || !strings.Contains(got.Body, "note") {
		t.Fatalf("body lost textual blocks: %q", got.Body)
	}
	if !strings.Contains(got.Body, "```notion") || !strings.Contains(got.Body, `"column_list"`) {
		t.Fatalf("body lost the unrepresentable block: %q", got.Body)
	}
}

func TestPullExpandsNestedBlocks(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-b", nil)
	now := time.Now().UTC()
	pageID := "page-nested"
	props := map[string]notion.PropertyValue{
		"名称":                {Type: "title", Title: []notion.RichText{{PlainText: "x"}}},
		notion.PropMemoID:   {Type: "rich_text", RichText: []notion.RichText{{PlainText: "memo-nested"}}},
		notion.PropCreated:  {Type: "date", Date: &notion.DateValue{Start: now.Add(-time.Hour).Format(time.RFC3339)}},
		notion.PropUpdated:  {Type: "date", Date: &notion.DateValue{Start: now.Format(time.RFC3339)}},
		notion.PropRevision: {Type: "number", Number: float64Ptr(1)},
		notion.PropDevice:   {Type: "rich_text", RichText: []notion.RichText{{PlainText: "dev-a"}}},
		notion.PropStatus:   {Type: "select", Select: &notion.Option{Name: notion.StatusActive}},
	}
	e.fake.SeedPage(notion.Page{ID: pageID, CreatedTime: now.Add(-time.Hour), LastEditedTime: now, Properties: props}, []notion.Block{
		{Object: "block", Type: "toggle", HasChildren: true, Raw: json.RawMessage(`{"object":"block","type":"toggle","toggle":{"rich_text":[{"type":"text","plain_text":"summary"}]}}`)},
	})
	parent := e.fake.Children[pageID][0]
	if _, err := e.fake.AppendChildren(ctx, parent.ID, []notion.Block{{
		Object:    "block",
		Type:      "paragraph",
		Paragraph: &notion.TextBlock{RichText: []notion.RichText{{Type: "text", PlainText: "nested child"}}},
	}}); err != nil {
		t.Fatal(err)
	}
	_ = e.store.SetLastPull(ctx, now.Add(-time.Hour))

	if err := e.svc.PullOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := e.store.GetMemo(ctx, "memo-nested")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Body, "summary") || !strings.Contains(got.Body, "nested child") {
		t.Fatalf("nested block lost on pull: %q", got.Body)
	}
}

func TestTwoDevicesConvergeWithoutDuplicates(t *testing.T) {
	ctx := context.Background()
	shared := fake.New()
	a := newEnv(t, "dev-a", shared)
	b := newEnv(t, "dev-b", shared)

	memo, _ := a.store.CreateMemo(ctx, store.MemoInput{Body: "from device a", Tags: []string{"shared"}})
	if _, err := a.svc.PushOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(shared.Pages) != 1 {
		t.Fatalf("pages after A = %d", len(shared.Pages))
	}

	if err := b.svc.FullScan(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := b.store.GetMemo(ctx, memo.ID)
	if err != nil || got.Body != "from device a" || got.BodyText != "from device a" {
		t.Fatalf("B did not pull: %+v %v", got, err)
	}

	if _, err := b.store.UpdateMemo(ctx, memo.ID, "edited on b", []string{"shared"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := b.svc.PushOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(shared.Pages) != 1 {
		t.Fatalf("B created a duplicate page: %d", len(shared.Pages))
	}

	if err := a.svc.PullOnce(ctx); err != nil {
		t.Fatal(err)
	}
	gotA, _ := a.store.GetMemo(ctx, memo.ID)
	if gotA.Revision != 2 || gotA.Body != "edited on b" || gotA.BodyText != "edited on b" {
		t.Fatalf("A did not converge: %+v", gotA)
	}

	if err := b.store.SoftDelete(ctx, memo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := b.svc.PushOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.svc.PullOnce(ctx); err != nil {
		t.Fatal(err)
	}
	gotA, _ = a.store.GetMemo(ctx, memo.ID)
	if gotA.DeletedAt == nil {
		t.Fatal("deletion did not propagate to A")
	}
	if len(shared.Pages) != 1 {
		t.Fatalf("pages = %d", len(shared.Pages))
	}
}

func TestPullDoesNotWriteBodyIntoEvictedMemo(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-a", nil)
	memo, _ := e.store.CreateMemo(ctx, store.MemoInput{Body: "local body"})
	_ = e.store.MarkSynced(ctx, memo.ID, "page-ev", 1, time.Now().Add(-time.Hour))
	_ = e.store.ClearOutbox(ctx, memo.ID)
	_ = e.store.SetEvicted(ctx, memo.ID)
	now := time.Now().UTC()
	url := "https://signed.example/evicted.png"
	e.fake.Downloads[url] = []byte("bytes")
	e.fake.SeedPage(notion.Page{
		ID: "page-ev", CreatedTime: now.Add(-time.Hour), LastEditedTime: now,
		Properties: map[string]notion.PropertyValue{
			"名称":                {Type: "title", Title: []notion.RichText{{PlainText: "remote"}}},
			notion.PropMemoID:   {Type: "rich_text", RichText: []notion.RichText{{PlainText: memo.ID}}},
			notion.PropCreated:  {Type: "date", Date: &notion.DateValue{Start: now.Add(-time.Hour).Format(time.RFC3339)}},
			notion.PropUpdated:  {Type: "date", Date: &notion.DateValue{Start: now.Format(time.RFC3339)}},
			notion.PropRevision: {Type: "number", Number: float64Ptr(3)},
			notion.PropStatus:   {Type: "select", Select: &notion.Option{Name: notion.StatusActive}},
		},
	}, []notion.Block{
		{Object: "block", Type: "paragraph", Paragraph: &notion.TextBlock{RichText: []notion.RichText{{Type: "text", PlainText: "remote body"}}}},
		{Object: "block", Type: "image", Image: &notion.FileBlock{Type: "file", File: &notion.FileObject{URL: url}, Caption: []notion.RichText{{Type: "text", PlainText: "x.png"}}}},
	})
	_ = e.store.SetLastPull(ctx, now.Add(-time.Hour))
	if err := e.svc.PullOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := e.store.GetMemo(ctx, memo.ID)
	if got.LocalState != domain.LocalEvicted || got.Body != "" {
		t.Fatalf("evicted memo gained a body: %+v", got)
	}
	if got.Revision != 3 {
		t.Fatalf("metadata did not update: revision %d", got.Revision)
	}
	if e.imp.saved != 0 {
		t.Fatalf("evicted memo downloaded %d attachments", e.imp.saved)
	}
}

func TestEligibleForEviction(t *testing.T) {
	now := time.Now().UTC()
	synced := now.Add(-31 * 24 * time.Hour)
	base := domain.Memo{
		LocalState: domain.LocalPresent, NotionPageID: "p", NotionSyncedAt: &synced,
	}
	policy := RetentionPolicy{Enabled: true, Days: 30}
	if !EligibleForEviction(base, false, now, policy) {
		t.Fatal("31-day-old synced memo should be eligible")
	}
	base.KeepLocal = true
	if EligibleForEviction(base, false, now, policy) {
		t.Fatal("pinned memo must be exempt")
	}
	base.KeepLocal = false
	base.NotionPageID = ""
	if EligibleForEviction(base, false, now, policy) {
		t.Fatal("local-only memo must be exempt")
	}
	base.NotionPageID = "p"
	if EligibleForEviction(base, true, now, policy) {
		t.Fatal("pending outbox must block eviction")
	}
	base.Attachments = []domain.Attachment{{ID: "a"}}
	if EligibleForEviction(base, false, now, policy) {
		t.Fatal("unsynced attachment must block eviction")
	}
	up := now
	base.Attachments[0].UploadedAt = &up
	if !EligibleForEviction(base, false, now, policy) {
		t.Fatal("uploaded attachment should allow eviction")
	}
	if !EligibleForEviction(base, false, now, RetentionPolicy{Enabled: true, Immediate: true}) {
		t.Fatal("immediate policy should evict now")
	}
	if EligibleForEviction(base, false, now, RetentionPolicy{Enabled: true, Days: 0}) {
		t.Fatal("never policy must not evict")
	}
	base.NotionSyncedAt = &now
	if EligibleForEviction(base, false, now, policy) {
		t.Fatal("fresh memo must not evict")
	}
}

func TestEvictionKeepsTextAndHydrationRestoresBody(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-a", nil)
	memo, _ := e.store.CreateMemo(ctx, store.MemoInput{Body: "evict and hydrate"})
	if _, err := e.svc.PushOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := e.store.GetMemo(ctx, memo.ID)
	pageID := got.NotionPageID
	now := *e.now
	_ = e.store.MarkSynced(ctx, memo.ID, pageID, got.Revision, now.Add(-31*24*time.Hour))
	item, _ := e.store.OutboxForMemo(ctx, memo.ID)
	if item != nil {
		t.Fatalf("outbox = %+v", item)
	}
	*e.now = now
	if err := e.svc.EvictOnce(ctx); err != nil {
		t.Fatal(err)
	}
	evicted, _ := e.store.GetMemo(ctx, memo.ID)
	if evicted.LocalState != domain.LocalEvicted || evicted.Body != "" {
		t.Fatalf("evicted = %+v", evicted)
	}
	if evicted.BodyText != "evict and hydrate" {
		t.Fatalf("body text lost: %q", evicted.BodyText)
	}

	if err := e.svc.Hydrate(ctx, memo.ID); err != nil {
		t.Fatal(err)
	}
	hydrated, _ := e.store.GetMemo(ctx, memo.ID)
	if hydrated.LocalState != domain.LocalPresent || hydrated.BodyText != "evict and hydrate" || hydrated.LastHydratedAt == nil {
		t.Fatalf("hydrated = %+v", hydrated)
	}
}

func TestHydrateImportsAttachmentBytes(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-b", nil)
	now := time.Now().UTC()
	pageID := "page-att"
	url := "https://signed.example/a.png"
	e.fake.Downloads[url] = []byte("image-bytes")
	e.fake.SeedPage(notion.Page{
		ID: pageID, CreatedTime: now.Add(-time.Hour), LastEditedTime: now,
		Properties: map[string]notion.PropertyValue{
			"名称":                {Type: "title", Title: []notion.RichText{{PlainText: "with attachment"}}},
			notion.PropMemoID:   {Type: "rich_text", RichText: []notion.RichText{{PlainText: "memo-att"}}},
			notion.PropCreated:  {Type: "date", Date: &notion.DateValue{Start: now.Add(-time.Hour).Format(time.RFC3339)}},
			notion.PropUpdated:  {Type: "date", Date: &notion.DateValue{Start: now.Format(time.RFC3339)}},
			notion.PropRevision: {Type: "number", Number: float64Ptr(2)},
			notion.PropStatus:   {Type: "select", Select: &notion.Option{Name: notion.StatusActive}},
		},
	}, []notion.Block{
		{Object: "block", Type: "paragraph", Paragraph: &notion.TextBlock{RichText: []notion.RichText{{Type: "text", PlainText: "see image"}}}},
		{Object: "block", Type: "image", Image: &notion.FileBlock{Type: "file", File: &notion.FileObject{URL: url}, Caption: []notion.RichText{{Type: "text", PlainText: "a.png"}}}},
	})
	if err := e.svc.FullScan(ctx); err != nil {
		t.Fatal(err)
	}
	memo, _ := e.store.GetMemo(ctx, "memo-att")
	if len(memo.Attachments) != 1 || memo.Attachments[0].LocalPath == "" {
		t.Fatalf("attachments = %+v", memo.Attachments)
	}
	if memo.Attachments[0].NotionBlockID == "" {
		t.Fatal("attachment block id not recorded")
	}
	if e.imp.saved != 1 {
		t.Fatalf("saved = %d", e.imp.saved)
	}
}

func TestHydrationRestoresEvictedAttachmentBytes(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-a", nil)
	memo, _ := e.store.CreateMemo(ctx, store.MemoInput{Body: "with image"})
	path := filepath.Join(t.TempDir(), "a.png")
	if err := os.WriteFile(path, []byte("image-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	att, err := e.store.CreateAttachment(ctx, domain.Attachment{
		ID: domain.NewID(), MemoID: memo.ID, Kind: domain.AttachmentImage,
		OriginalName: "a.png", LocalPath: path, MIME: "image/png", Bytes: 11,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := "with image\n\n![a.png](attachment://" + att.ID + ")"
	if _, err := e.store.UpdateMemo(ctx, memo.ID, body, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.PushOnce(ctx); err != nil {
		t.Fatal(err)
	}
	synced, _ := e.store.GetMemo(ctx, memo.ID)
	if len(synced.Attachments) != 1 || synced.Attachments[0].NotionBlockID == "" {
		t.Fatalf("push did not wire attachment: %+v", synced.Attachments)
	}
	_ = e.store.MarkSynced(ctx, memo.ID, synced.NotionPageID, synced.Revision, (*e.now).Add(-31*24*time.Hour))
	_ = e.store.MarkAttachmentEvicted(ctx, att.ID, *e.now)
	_ = os.Remove(path)
	_ = e.store.SetEvicted(ctx, memo.ID)

	if err := e.svc.Hydrate(ctx, memo.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := e.store.GetMemo(ctx, memo.ID)
	if got.LocalState != domain.LocalPresent {
		t.Fatalf("state = %q", got.LocalState)
	}
	if len(got.Attachments) != 1 || !got.Attachments[0].Present || got.Attachments[0].LocalPath == "" {
		t.Fatalf("attachment not restored: %+v", got.Attachments)
	}
	if _, err := os.Stat(got.Attachments[0].LocalPath); err != nil {
		t.Fatalf("attachment bytes missing: %v", err)
	}
	if !strings.Contains(got.Body, "attachment://"+got.Attachments[0].ID) {
		t.Fatalf("body = %q", got.Body)
	}
}

func TestRestoreAllReportsProgress(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-a", nil)
	memo, _ := e.store.CreateMemo(ctx, store.MemoInput{Body: "restore me"})
	if _, err := e.svc.PushOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := e.store.GetMemo(ctx, memo.ID)
	_ = e.store.MarkSynced(ctx, memo.ID, got.NotionPageID, got.Revision, e.now.Add(-31*24*time.Hour))
	_ = e.store.SetEvicted(ctx, memo.ID)
	progress, err := e.svc.RestoreAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Total != 1 || progress.Completed != 1 || progress.Failed != 0 {
		t.Fatalf("progress = %+v", progress)
	}
	restored, _ := e.store.GetMemo(ctx, memo.ID)
	if restored.LocalState != domain.LocalPresent {
		t.Fatalf("state = %q", restored.LocalState)
	}
}
func TestPushKeepsUnsupportedMarkdownAndRestoresNotionFence(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "dev-a", nil)
	callout := `{"type":"callout","callout":{"rich_text":[{"type":"text","text":{"content":"note"}}]}}`
	body := "| a | b |\n| - | - |\n\n```notion\n" + callout + "\n```\n\n```notion\nnot json\n```"
	memo, err := e.store.CreateMemo(ctx, store.MemoInput{Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.PushOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := e.store.GetMemo(ctx, memo.ID)
	children := e.fake.Children[got.NotionPageID]
	if len(children) != 4 {
		t.Fatalf("children = %+v", children)
	}
	if children[0].Type != "paragraph" || children[0].Paragraph.RichText[0].Text.Content != "| a | b |" {
		t.Errorf("table line was not kept literal: %+v", children[0])
	}
	if children[1].Type != "paragraph" || children[1].Paragraph.RichText[0].Text.Content != "| - | - |" {
		t.Errorf("table separator was not kept literal: %+v", children[1])
	}
	if children[2].Type != "callout" {
		t.Errorf("notion fence restored to %q, want callout", children[2].Type)
	}
	if children[3].Type != "code" || children[3].Code == nil || children[3].Code.Language != "plain text" {
		t.Errorf("bad notion fence did not fall back to literal code: %+v", children[3])
	}
}
