// Package attachments owns image compression, thumbnails, local file storage
// and the narrow open-with-the-OS-default-app action. The webview never sees an
// arbitrary filesystem path.
package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/olianate/memo/internal/config"
	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/platform"
	"github.com/olianate/memo/internal/store"
)

const (
	maxLongEdge = 2560
	thumbEdge   = 320
	jpegQuality = 85
	// Notion single-part uploads are capped at 20 MB even on paid plans with a
	// larger workspace file ceiling.
	maxSinglePart = 20 << 20
)

// Service stores attachments under the app data directory.
type Service struct {
	store   *store.Store
	cfg     *config.Store
	dataDir string
}

// New returns an attachment service rooted at dataDir.
func New(st *store.Store, cfg *config.Store, dataDir string) *Service {
	return &Service{store: st, cfg: cfg, dataDir: dataDir}
}

// Add ingests a picked, pasted or dropped file for a memo. Images are resized
// and re-encoded; other files are stored as-is up to the workspace limit.
func (s *Service) Add(ctx context.Context, memoID, filename, mime string, data []byte) (*domain.Attachment, error) {
	if _, err := s.store.GetMemo(ctx, memoID); err != nil {
		return nil, fmt.Errorf("memo not found")
	}
	if len(data) == 0 {
		return nil, errors.New("file is empty")
	}
	kind := domain.AttachmentFile
	if strings.HasPrefix(strings.ToLower(mime), "image/") {
		kind = domain.AttachmentImage
	}
	if kind == domain.AttachmentImage {
		processed, width, height, err := compressImage(data)
		if err != nil {
			return nil, fmt.Errorf("image could not be read: %w", err)
		}
		data = processed
		max := s.effectiveLimit(ctx)
		if max > 0 && int64(len(data)) > max {
			return nil, fmt.Errorf("image is still %s after compression; the upload limit is %s", humanBytes(int64(len(data))), humanBytes(max))
		}
		return s.save(ctx, memoID, filename, "image/jpeg", kind, data, width, height, true)
	}
	max := s.effectiveLimit(ctx)
	if max > 0 && int64(len(data)) > max {
		return nil, fmt.Errorf("file is %s; the upload limit is %s", humanBytes(int64(len(data))), humanBytes(max))
	}
	return s.save(ctx, memoID, filename, mime, kind, data, 0, 0, false)
}

// SaveRemote implements sync.AttachmentImporter for hydrated or pulled files.
func (s *Service) SaveRemote(ctx context.Context, memoID, filename, mime string, kind domain.AttachmentKind, data []byte) (*domain.Attachment, error) {
	if mime == "" {
		mime = http.DetectContentType(data)
	}
	if kind == domain.AttachmentImage {
		processed, width, height, err := compressImage(data)
		if err != nil {
			// Notion may return an image format Go cannot decode; keep the bytes.
			return s.save(ctx, memoID, filename, mime, kind, data, 0, 0, true)
		}
		return s.save(ctx, memoID, filename, "image/jpeg", kind, processed, width, height, true)
	}
	return s.save(ctx, memoID, filename, mime, kind, data, 0, 0, false)
}

func (s *Service) save(ctx context.Context, memoID, filename, mime string, kind domain.AttachmentKind, data []byte, width, height int, makeThumb bool) (*domain.Attachment, error) {
	if !domain.IsSafeID(memoID) {
		return nil, errors.New("invalid memo id")
	}
	id := domain.NewID()
	name := sanitizeFilename(filename)
	dir := filepath.Join(s.dataDir, "attachments", memoID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	localPath := filepath.Join(dir, id+"-"+name)
	if err := os.WriteFile(localPath, data, 0o644); err != nil {
		return nil, err
	}
	thumbPath := ""
	if makeThumb {
		thumbData, err := thumbnail(data)
		if err == nil {
			thumbPath = filepath.Join(dir, id+"-thumb.jpg")
			if err := os.WriteFile(thumbPath, thumbData, 0o644); err != nil {
				thumbPath = ""
			}
		}
	}
	sum := sha256.Sum256(data)
	att, err := s.store.CreateAttachment(ctx, domain.Attachment{
		ID: id, MemoID: memoID, Kind: kind, OriginalName: name,
		LocalPath: localPath, ThumbPath: thumbPath, MIME: mime, Bytes: int64(len(data)),
		Width: width, Height: height, SHA256: hex.EncodeToString(sum[:]),
	})
	if err != nil {
		_ = os.Remove(localPath)
		if thumbPath != "" {
			_ = os.Remove(thumbPath)
		}
		return nil, err
	}
	return att, nil
}

// DeleteFiles removes an attachment's bytes and thumbnail (eviction or delete).
func (s *Service) DeleteFiles(_ context.Context, a domain.Attachment) error {
	if a.LocalPath != "" {
		_ = os.Remove(a.LocalPath)
	}
	if a.ThumbPath != "" {
		_ = os.Remove(a.ThumbPath)
	}
	return nil
}

// DeleteAllFiles removes the attachment directory. Used by the explicit wipe
// action; the SQLite rows are cleared separately.
func (s *Service) DeleteAllFiles() error {
	dir := filepath.Join(s.dataDir, "attachments")
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return os.MkdirAll(dir, 0o755)
}

// Remove deletes metadata and files.
func (s *Service) Remove(ctx context.Context, id string) error {
	a, err := s.store.GetAttachment(ctx, id)
	if err != nil {
		return err
	}
	_ = s.DeleteFiles(ctx, *a)
	return s.store.DeleteAttachment(ctx, id)
}

// ThumbnailDataURL returns a data URL for the 320 px thumbnail, loading the
// original image and generating a thumbnail on demand after a restart.
func (s *Service) ThumbnailDataURL(ctx context.Context, id string) (string, error) {
	a, err := s.store.GetAttachment(ctx, id)
	if err != nil {
		return "", err
	}
	if a.ThumbPath != "" {
		if data, err := os.ReadFile(a.ThumbPath); err == nil {
			return dataURL("image/jpeg", data), nil
		}
	}
	if a.LocalPath != "" {
		if data, err := os.ReadFile(a.LocalPath); err == nil {
			if thumb, err := thumbnail(data); err == nil {
				return dataURL("image/jpeg", thumb), nil
			}
			return dataURL(a.MIME, data), nil
		}
	}
	return "", errors.New("attachment bytes are not available locally")
}

// DataURL returns the full image for the viewer sheet.
func (s *Service) DataURL(ctx context.Context, id string) (string, error) {
	a, err := s.store.GetAttachment(ctx, id)
	if err != nil {
		return "", err
	}
	if a.LocalPath == "" {
		return "", errors.New("attachment bytes are not available locally")
	}
	data, err := os.ReadFile(a.LocalPath)
	if err != nil {
		return "", err
	}
	return dataURL(a.MIME, data), nil
}

// Open launches a local file with the OS default application.
func (s *Service) Open(ctx context.Context, id string) error {
	a, err := s.store.GetAttachment(ctx, id)
	if err != nil {
		return err
	}
	if a.LocalPath == "" {
		return errors.New("attachment is evicted; hydrate the memo first")
	}
	if _, err := os.Stat(a.LocalPath); err != nil {
		return errors.New("attachment file is missing")
	}
	return platform.OpenFile(a.LocalPath)
}

func (s *Service) maxUpload(ctx context.Context) int {
	if s.cfg == nil {
		return 5 << 20
	}
	return s.cfg.Int(ctx, config.KeyWorkspaceFileLimit)
}

// effectiveLimit is the smaller of the workspace ceiling and Notion's
// single-part upload limit.
func (s *Service) effectiveLimit(ctx context.Context) int64 {
	max := int64(s.maxUpload(ctx))
	if max <= 0 || max > maxSinglePart {
		return maxSinglePart
	}
	return max
}

// compressImage decodes, fits inside 2560 px, and encodes JPEG quality 85.
func compressImage(data []byte) ([]byte, int, int, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, err
	}
	img = fit(img, maxLongEdge, color.White)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, 0, 0, err
	}
	b := img.Bounds()
	return buf.Bytes(), b.Dx(), b.Dy(), nil
}

func thumbnail(data []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	img = fit(img, thumbEdge, color.White)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// fit scales the longest edge to max and composites onto an opaque background
// so transparent PNGs do not turn black in JPEG.
func fit(src image.Image, max int, bg color.Color) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return src
	}
	scale := 1.0
	if w > max || h > max {
		if w >= h {
			scale = float64(max) / float64(w)
		} else {
			scale = float64(max) / float64(h)
		}
	}
	nw, nh := int(float64(w)*scale+0.5), int(float64(h)*scale+0.5)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	return dst
}

func dataURL(mime string, data []byte) string {
	if mime == "" {
		mime = http.DetectContentType(data)
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func sanitizeFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '.', r == '-', r == '_', r == ' ':
			return r
		default:
			return '_'
		}
	}, name)
	name = strings.Trim(name, ". ")
	if name == "" || name == "." || name == ".." {
		name = "attachment"
	}
	runes := []rune(name)
	if len(runes) > 100 {
		name = string(runes[len(runes)-100:])
	}
	return name
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Ensure the GIF/PNG/JPEG decoders are registered even if only one is directly
// referenced elsewhere.
var (
	_ = gif.Decode
	_ = png.Decode
)
