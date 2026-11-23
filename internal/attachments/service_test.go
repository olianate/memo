package attachments

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olianate/memo/internal/config"
	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/store"
)

func testService(t *testing.T) (*Service, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "att.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := config.New(st)
	_ = cfg.SetInt(context.Background(), config.KeyWorkspaceFileLimit, 5<<20)
	dataDir := filepath.Join(dir, "data")
	return New(st, cfg, dataDir), st, dataDir
}

func pngImage(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 200, B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func createMemo(t *testing.T, st *store.Store) *domain.Memo {
	t.Helper()
	memo, err := st.CreateMemo(context.Background(), store.MemoInput{
		Blocks: []domain.Block{{ID: domain.NewID(), Type: domain.BlockParagraph, Spans: []domain.Span{{Text: "memo"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return memo
}

func TestImageCompressionAndThumbnail(t *testing.T) {
	ctx := context.Background()
	svc, st, dataDir := testService(t)
	memo := createMemo(t, st)
	att, err := svc.Add(ctx, memo.ID, "screen shot.png", "image/png", pngImage(t, 3000, 1000))
	if err != nil {
		t.Fatal(err)
	}
	if att.Kind != domain.AttachmentImage || att.MIME != "image/jpeg" {
		t.Fatalf("attachment = %+v", att)
	}
	if att.Width != 2560 || att.Height != 853 {
		t.Fatalf("dimensions = %dx%d", att.Width, att.Height)
	}
	if att.Bytes > int64(5<<20) {
		t.Fatalf("bytes = %d", att.Bytes)
	}
	if att.ThumbPath == "" {
		t.Fatal("thumbnail missing")
	}
	if _, err := os.Stat(att.ThumbPath); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(att.LocalPath, filepath.Join(dataDir, "attachments", memo.ID)) {
		t.Fatalf("path = %s", att.LocalPath)
	}
	thumb, err := svc.ThumbnailDataURL(ctx, att.ID)
	if err != nil || !strings.HasPrefix(thumb, "data:image/jpeg;base64,") {
		t.Fatalf("thumb = %q, %v", thumb[:20], err)
	}
	full, err := svc.DataURL(ctx, att.ID)
	if err != nil || !strings.HasPrefix(full, "data:image/jpeg;base64,") {
		t.Fatalf("full = %v", err)
	}
}

func TestFileLimitAndSanitization(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := testService(t)
	memo := createMemo(t, st)
	_ = config.New(st).SetInt(ctx, config.KeyWorkspaceFileLimit, 10)
	if _, err := svc.Add(ctx, memo.ID, "too-big.txt", "text/plain", []byte("more than ten bytes")); err == nil {
		t.Fatal("expected a size rejection")
	}
	_ = config.New(st).SetInt(ctx, config.KeyWorkspaceFileLimit, 5<<20)
	att, err := svc.Add(ctx, memo.ID, "../../evil name?.txt", "text/plain", []byte("ok"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(att.OriginalName, "..") || strings.Contains(att.OriginalName, "/") || strings.Contains(att.OriginalName, "?") {
		t.Fatalf("name = %q", att.OriginalName)
	}
	if att.Kind != domain.AttachmentFile {
		t.Fatalf("kind = %q", att.Kind)
	}
}

func TestAttachmentSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "att.db")
	st, _ := store.Open(path)
	cfg := config.New(st)
	_ = cfg.SetInt(ctx, config.KeyWorkspaceFileLimit, 5<<20)
	dataDir := filepath.Join(dir, "data")
	svc := New(st, cfg, dataDir)
	memo := createMemo(t, st)
	att, err := svc.Add(ctx, memo.ID, "note.txt", "text/plain", []byte("persist me"))
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	st2, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	again, err := st2.GetAttachment(ctx, att.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.OriginalName != "note.txt" || again.Bytes != 10 || again.LocalPath == "" {
		t.Fatalf("again = %+v", again)
	}
	data, err := os.ReadFile(again.LocalPath)
	if err != nil || string(data) != "persist me" {
		t.Fatalf("bytes = %q, %v", data, err)
	}
}

func TestSaveRemoteRejectsUnsafeMemoID(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := testService(t)
	if _, err := svc.SaveRemote(ctx, "../../escape", "x.txt", "text/plain", domain.AttachmentFile, []byte("x")); err == nil {
		t.Fatal("unsafe memo id accepted")
	}
}

func TestDeleteFiles(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := testService(t)
	memo := createMemo(t, st)
	att, _ := svc.Add(ctx, memo.ID, "gone.txt", "text/plain", []byte("x"))
	path := att.LocalPath
	if err := svc.DeleteFiles(ctx, *att); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
}
