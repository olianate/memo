package archive_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olianate/memo/internal/archive"
	"github.com/olianate/memo/internal/config"
	"github.com/olianate/memo/internal/notion"
	"github.com/olianate/memo/internal/notion/fake"
	"github.com/olianate/memo/internal/store"
)

type fakeSecrets struct {
	token   string
	cleared int
}

func (f *fakeSecrets) SetNotionToken(_ context.Context, token string) error {
	f.token = token
	return nil
}

func (f *fakeSecrets) ClearNotionToken(_ context.Context) error {
	f.token = ""
	f.cleared++
	return nil
}

type fakeOutbox struct {
	calls int
}

func (f *fakeOutbox) EnqueueAllLocalOnly(_ context.Context) error {
	f.calls++
	return nil
}

type env struct {
	cfg     *config.Store
	link    *archive.Connector
	gateway *fake.Gateway
	secrets *fakeSecrets
	outbox  *fakeOutbox
	wakes   int
	changes int
	logs    []string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "archive.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := &env{
		cfg:     config.New(st),
		gateway: fake.New(),
		secrets: &fakeSecrets{},
		outbox:  &fakeOutbox{},
	}
	e.link = archive.New(e.cfg, e.secrets, e.outbox,
		func(string) notion.Gateway { return e.gateway },
		func() { e.wakes++ },
		func() { e.changes++ },
		func(level, event, _ string) { e.logs = append(e.logs, level+" "+event) },
	)
	return e
}

const pageID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1"
const dsID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb1"

func seedPage(g *fake.Gateway, id, title string) {
	g.SeedPage(notion.Page{
		ID:     id,
		Parent: notion.Parent{Type: "page_id"},
		Properties: map[string]notion.PropertyValue{
			"Name": {Type: "title", Title: []notion.RichText{{PlainText: title}}},
		},
	}, nil)
}

func TestConnectCreatesDatabaseUnderPage(t *testing.T) {
	e := newEnv(t)
	seedPage(e.gateway, pageID, "Projects")
	ctx := context.Background()

	result, err := e.link.Connect(ctx, archive.ConnectRequest{
		Token: "ntn_test", Kind: "page", TargetID: pageID, DatabaseName: "Notes",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.CreatedDatabase || result.DataSourceID == "" || result.DatabaseTitle != "Notes" {
		t.Fatalf("result = %+v", result)
	}
	if e.secrets.token != "ntn_test" {
		t.Fatalf("token = %q", e.secrets.token)
	}
	if e.outbox.calls != 1 || e.wakes != 1 || e.changes != 1 {
		t.Fatalf("outbox=%d wakes=%d changes=%d", e.outbox.calls, e.wakes, e.changes)
	}
	for key, want := range map[string]string{
		config.KeyNotionConfigured:   "true",
		config.KeyNotionDataSourceID: result.DataSourceID,
		config.KeyNotionParentPageID: pageID,
		config.KeySyncEnabled:        "true",
	} {
		if got := e.cfg.Get(ctx, key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	if !e.link.Connected(ctx) {
		t.Fatal("connector should report a connected archive")
	}
}

func TestConnectRefusesConflictingSchema(t *testing.T) {
	e := newEnv(t)
	e.gateway.SeedDataSource(notion.DataSource{
		ID: dsID, Title: "Shared",
		PropertyTypes: map[string]string{"Name": "title", notion.PropMemoID: "number"},
	})

	_, err := e.link.Connect(context.Background(), archive.ConnectRequest{
		Token: "ntn_test", Kind: "database", TargetID: dsID,
	})
	if err == nil || !strings.Contains(err.Error(), notion.PropMemoID) {
		t.Fatalf("err = %v, want a conflict naming %s", err, notion.PropMemoID)
	}
	if e.outbox.calls != 0 {
		t.Fatal("a refused connection must not queue memos")
	}
}

func TestConnectRequiresConfirmationForNewProperties(t *testing.T) {
	e := newEnv(t)
	e.gateway.SeedDataSource(notion.DataSource{
		ID: dsID, Title: "Shared",
		PropertyTypes: map[string]string{"Name": "title"},
	})
	ctx := context.Background()

	if _, err := e.link.Connect(ctx, archive.ConnectRequest{
		Token: "ntn_test", Kind: "database", TargetID: dsID,
	}); err == nil {
		t.Fatal("expected the schema change to need confirmation")
	}
	result, err := e.link.Connect(ctx, archive.ConnectRequest{
		Token: "ntn_test", Kind: "database", TargetID: dsID, ConfirmSchemaChanges: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.CreatedDatabase || result.TitleProperty != "Name" {
		t.Fatalf("result = %+v", result)
	}
	if e.gateway.CallsOf("UpdateDataSourceSchema") != 1 {
		t.Fatal("schema was not extended")
	}
	if e.cfg.Get(ctx, config.KeyNotionDataSourceID) != dsID {
		t.Fatal("connection was not stored")
	}
}

func TestPreviewListsMissingProperties(t *testing.T) {
	e := newEnv(t)
	e.gateway.SeedDataSource(notion.DataSource{
		ID: dsID, Title: "Shared",
		PropertyTypes: map[string]string{"Name": "title"},
	})
	preview, err := e.link.PreviewDatabase(context.Background(), "ntn_test", dsID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.TitleProperty != "Name" || len(preview.MissingProperties) == 0 {
		t.Fatalf("preview = %+v", preview)
	}
	found := false
	for _, property := range preview.MissingProperties {
		if property.Name == notion.PropMemoID && property.Type == "rich_text" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing properties = %+v", preview.MissingProperties)
	}
}

func TestValidateTokenReportsAuthorizationFailure(t *testing.T) {
	e := newEnv(t)
	e.gateway.OnCall = func(method string) error {
		if method == "Me" {
			return notion.StatusError(401, "unauthorized", "bad token")
		}
		return nil
	}
	_, err := e.link.ValidateToken(context.Background(), "ntn_wrong")
	if err == nil || !strings.Contains(err.Error(), "令牌无效") {
		t.Fatalf("err = %v", err)
	}
	if len(e.logs) != 1 || !strings.HasPrefix(e.logs[0], "error") {
		t.Fatalf("failure was not logged: %v", e.logs)
	}
}

func TestDisconnectClearsTheConnection(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.cfg.Set(ctx, config.KeyNotionConfigured, "true"); err != nil {
		t.Fatal(err)
	}
	if err := e.cfg.Set(ctx, config.KeyNotionDataSourceID, dsID); err != nil {
		t.Fatal(err)
	}
	if err := e.link.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
	if e.secrets.cleared != 1 || e.link.Connected(ctx) || e.changes != 1 {
		t.Fatalf("cleared=%d connected=%v changes=%d", e.secrets.cleared, e.link.Connected(ctx), e.changes)
	}
	if got := e.cfg.Get(ctx, config.KeyNotionDataSourceID); got != "" {
		t.Fatalf("data source = %q", got)
	}
}
