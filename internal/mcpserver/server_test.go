package mcpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/olianate/memo/internal/attachments"
	"github.com/olianate/memo/internal/config"
	"github.com/olianate/memo/internal/search"
	"github.com/olianate/memo/internal/services"
	"github.com/olianate/memo/internal/store"
	syncengine "github.com/olianate/memo/internal/sync"
)

type testSecrets struct{}

func (testSecrets) NotionToken(context.Context) (string, error)  { return "", nil }
func (testSecrets) SetNotionToken(context.Context, string) error { return nil }
func (testSecrets) ClearNotionToken(context.Context) error       { return nil }

func newTestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "mcp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	if _, err := st.EnsureDevice(ctx, "dev-mcp"); err != nil {
		t.Fatal(err)
	}
	cfg := config.New(st)
	att := attachments.New(st, cfg, dir)
	engine := syncengine.New(st, cfg, testSecrets{}, att)
	engine.SetDeviceID("dev-mcp")
	memo := services.NewMemoService(st, engine, att, func(string, any) {})
	searchSvc := services.NewSearchService(search.New(st))
	return New(memo, searchSvc)
}

func structured[T any](t *testing.T, result *mcp.CallToolResult) T {
	t.Helper()
	if result == nil {
		t.Fatal("nil tool result")
	}
	if result.IsError {
		raw, _ := json.Marshal(result.Content)
		t.Fatalf("tool returned an error: %s", raw)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestStreamableHTTPTools(t *testing.T) {
	svc := newTestService(t)
	if err := svc.Start(0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop() })
	running, endpoint, lastErr := svc.Status()
	if !running || endpoint == "" || lastErr != "" {
		t.Fatalf("status = %v %q %q", running, endpoint, lastErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "memo-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             endpoint,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"search_memos": false, "create_memo": false, "update_memo": false, "delete_memo": false}
	for _, tool := range tools.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("tool %s not listed", name)
		}
	}

	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	onePixelPNG := base64.StdEncoding.EncodeToString(pngData.Bytes())

	created := structured[memoOutput](t, mustCall(t, ctx, session, "create_memo", map[string]any{
		"content": "# 测试\n\nhello mcp\n\n[示例](https://example.com)",
		"tags":    []string{"work"},
		"attachments": []any{
			map[string]any{"name": "pixel.png", "mime": "image/png", "data_base64": onePixelPNG},
		},
	}))
	if created.Memo.ID == "" || created.Memo.Content == "" || len(created.Memo.Tags) != 1 {
		t.Fatalf("created = %+v", created)
	}
	if len(created.Memo.Attachments) != 1 || created.Memo.Attachments[0].Kind != "image" {
		t.Fatalf("created attachments = %+v", created.Memo.Attachments)
	}
	if len(created.Memo.Links) != 1 || created.Memo.Links[0] != "https://example.com" {
		t.Fatalf("created links = %+v", created.Memo.Links)
	}
	imageID := created.Memo.Attachments[0].ID

	attachmentPath := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(attachmentPath, []byte("attachment body"), 0o600); err != nil {
		t.Fatal(err)
	}

	found := structured[searchMemosOutput](t, mustCall(t, ctx, session, "search_memos", map[string]any{
		"query": "hello",
	}))
	if found.Count != 1 || found.Results[0].Memo.ID != created.Memo.ID {
		t.Fatalf("search = %+v", found)
	}

	updated := structured[memoOutput](t, mustCall(t, ctx, session, "update_memo", map[string]any{
		"id":      created.Memo.ID,
		"content": "changed body\n\n[新链接](https://example.com/changed)",
		"tags":    []string{"life"},
		"attachments": []any{
			map[string]any{"path": attachmentPath, "mime": "text/plain"},
		},
	}))
	if !strings.Contains(updated.Memo.Content, "changed body") || !strings.Contains(updated.Memo.Content, "[新链接](https://example.com/changed)") {
		t.Fatalf("updated content = %q", updated.Memo.Content)
	}
	if len(updated.Memo.Tags) != 1 || updated.Memo.Tags[0] != "life" {
		t.Fatalf("updated = %+v", updated)
	}
	if len(updated.Memo.Attachments) != 2 {
		t.Fatalf("attachments after update = %+v", updated.Memo.Attachments)
	}
	var keptImage, addedFile bool
	for _, attachment := range updated.Memo.Attachments {
		if attachment.ID == imageID && attachment.Kind == "image" {
			keptImage = true
		}
		if attachment.Kind == "file" && attachment.Name == "notes.txt" {
			addedFile = true
		}
	}
	if !keptImage || !addedFile {
		t.Fatalf("attachments after update = %+v", updated.Memo.Attachments)
	}
	if len(updated.Memo.Links) != 1 || updated.Memo.Links[0] != "https://example.com/changed" {
		t.Fatalf("updated links = %+v", updated.Memo.Links)
	}

	removed := structured[memoOutput](t, mustCall(t, ctx, session, "update_memo", map[string]any{
		"id":                    created.Memo.ID,
		"remove_attachment_ids": []string{imageID},
	}))
	if len(removed.Memo.Attachments) != 1 || removed.Memo.Attachments[0].Kind != "file" {
		t.Fatalf("attachments after removal = %+v", removed.Memo.Attachments)
	}

	del := structured[deleteMemoOutput](t, mustCall(t, ctx, session, "delete_memo", map[string]any{
		"id": created.Memo.ID,
	}))
	if !del.Deleted {
		t.Fatalf("delete = %+v", del)
	}
	gone := structured[searchMemosOutput](t, mustCall(t, ctx, session, "search_memos", map[string]any{
		"query": "changed",
	}))
	if gone.Count != 0 {
		t.Fatalf("deleted memo still searchable: %+v", gone)
	}
}

func mustCall(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return result
}
