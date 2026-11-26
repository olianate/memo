package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.1.0", "1.1.0", 0},
		{"v1.1.0", "1.1.0", 0},
		{"1.1.1", "1.1.0", 1},
		{"1.2.0", "1.1.9", 1},
		{"2.0.0", "1.9.9", 1},
		{"1.1.0", "1.1.1", -1},
		{"1.1.0-beta.1", "1.1.0", -1},
		{"1.1.0", "1.1.0-beta.1", 1},
		{"1.1.0-beta.2", "1.1.0-beta.1", 1},
		{"1.1.0-beta.1", "1.1.0-alpha.9", 1},
		{"1.1.0+abc", "1.1.0", 0},
		{"v1.2", "1.2.0", 0},
	}
	for _, test := range tests {
		if got := compareVersions(test.a, test.b); got != test.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", test.a, test.b, got, test.want)
		}
	}
}

func TestSelectReleaseAsset(t *testing.T) {
	assets := []releaseAsset{
		{Name: "memo-windows-amd64-installer.exe", BrowserDownloadURL: "https://example.test/memo.exe"},
		{Name: "memo-windows-amd64.msi", BrowserDownloadURL: "https://example.test/memo.msi"},
		{Name: "memo-macos-universal.dmg", BrowserDownloadURL: "https://example.test/memo.dmg"},
		{Name: "SHA256SUMS.txt", BrowserDownloadURL: "https://example.test/sums"},
	}

	asset, ok := selectReleaseAsset(assets, "windows", "amd64", false)
	if !ok || asset.Name != "memo-windows-amd64-installer.exe" {
		t.Fatalf("windows asset = %+v, %v", asset, ok)
	}
	asset, ok = selectReleaseAsset(assets, "windows", "amd64", true)
	if !ok || asset.Name != "memo-windows-amd64.msi" {
		t.Fatalf("windows MSI asset = %+v, %v", asset, ok)
	}
	asset, ok = selectReleaseAsset(assets, "windows", "arm64", false)
	if !ok || asset.Name != "memo-windows-amd64-installer.exe" {
		t.Fatalf("windows arm64 fallback = %+v, %v", asset, ok)
	}
	asset, ok = selectReleaseAsset(assets, "darwin", "arm64", false)
	if !ok || asset.Name != "memo-macos-universal.dmg" {
		t.Fatalf("darwin asset = %+v, %v", asset, ok)
	}
	if _, ok := selectReleaseAsset(assets, "linux", "amd64", false); ok {
		t.Fatal("linux should not select an automatic installer")
	}
}

func TestChecksumFor(t *testing.T) {
	data := []byte("aaaa  memo-windows-amd64-installer.exe\nbbbb *other.exe\n")
	sum, ok := checksumFor(data, "memo-windows-amd64-installer.exe")
	if !ok || sum != "aaaa" {
		t.Fatalf("checksum = %q, %v", sum, ok)
	}
	if _, ok := checksumFor(data, "missing.exe"); ok {
		t.Fatal("missing asset should not have a checksum")
	}
}

func TestUpdateServiceCheck(t *testing.T) {
	var userAgent, accept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent = r.Header.Get("User-Agent")
		accept = r.Header.Get("Accept")
		_ = json.NewEncoder(w).Encode(githubRelease{
			TagName:     "v99.0.0",
			Body:        "## 更新内容\n- 修复问题",
			HTMLURL:     "https://github.com/olianate/memo/releases/tag/v99.0.0",
			PublishedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			Assets: []releaseAsset{
				{Name: "memo-windows-amd64-installer.exe", BrowserDownloadURL: "https://example.test/memo.exe", Size: 123},
				{Name: "memo-macos-universal.dmg", BrowserDownloadURL: "https://example.test/memo.dmg", Size: 456},
				{Name: "SHA256SUMS.txt", BrowserDownloadURL: "https://example.test/sums", Size: 10},
			},
		})
	}))
	defer server.Close()

	var events []string
	svc := NewUpdateService(nil, func(name string, _ any) { events = append(events, name) })
	svc.apiURL = server.URL

	info, err := svc.Check()
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available || info.LatestVersion != "99.0.0" || info.CurrentVersion != Version {
		t.Fatalf("unexpected info: %+v", info)
	}
	switch runtime.GOOS {
	case "windows":
		if info.AssetName != "memo-windows-amd64-installer.exe" || info.AssetSize != 123 || !info.CanInstall {
			t.Fatalf("unexpected Windows asset: %+v", info)
		}
	case "darwin":
		if info.AssetName != "memo-macos-universal.dmg" || info.AssetSize != 456 || !info.CanInstall {
			t.Fatalf("unexpected macOS asset: %+v", info)
		}
	default:
		if info.AssetName != "" || info.AssetSize != 0 || info.CanInstall {
			t.Fatalf("unexpected unsupported-platform asset: %+v", info)
		}
	}
	if info.CheckedAt == "" || info.ReleaseURL == "" || !strings.Contains(info.Notes, "修复问题") {
		t.Fatalf("missing metadata: %+v", info)
	}
	if !strings.HasPrefix(userAgent, updateUserAgent+"/") || accept != "application/vnd.github+json" {
		t.Fatalf("headers: user-agent=%q accept=%q", userAgent, accept)
	}
	if len(events) != 1 || events[0] != "update:available" {
		t.Fatalf("events = %v", events)
	}

	// Re-checking the same release must not repeat the announcement.
	if _, err := svc.Check(); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("announcement repeated: %v", events)
	}

	status := svc.Status()
	if status.LatestVersion != "99.0.0" {
		t.Fatalf("status = %+v", status)
	}
}

func TestUpdateServiceAlreadyCurrent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v" + Version})
	}))
	defer server.Close()

	var events []string
	svc := NewUpdateService(nil, func(name string, _ any) { events = append(events, name) })
	svc.apiURL = server.URL

	info, err := svc.Check()
	if err != nil {
		t.Fatal(err)
	}
	if info.Available || info.CanInstall {
		t.Fatalf("unexpected update: %+v", info)
	}
	if len(events) != 0 {
		t.Fatalf("events = %v", events)
	}
}

func TestUpdateServiceStatusBeforeCheck(t *testing.T) {
	svc := NewUpdateService(nil, nil)
	status := svc.Status()
	if status.CurrentVersion != Version || status.Available {
		t.Fatalf("status = %+v", status)
	}
}

func TestUpdateServiceCheckError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer server.Close()

	svc := NewUpdateService(nil, nil)
	svc.apiURL = server.URL
	if _, err := svc.Check(); err == nil || !strings.Contains(err.Error(), "限制了更新检查") {
		t.Fatalf("error = %v", err)
	}
}

func TestUpdateServiceDownloadAndVerifyChecksum(t *testing.T) {
	payload := []byte("installer payload")
	sum := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pkg":
			_, _ = w.Write(payload)
		case "/sums":
			_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  memo.exe\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc := NewUpdateService(nil, nil)
	dest := filepath.Join(t.TempDir(), "memo.exe")
	if err := svc.downloadAsset(server.URL+"/pkg", dest, int64(len(payload))); err != nil {
		t.Fatal(err)
	}
	if err := svc.verifyChecksum(dest, server.URL+"/sums", "memo.exe"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(dest, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := svc.verifyChecksum(dest, server.URL+"/sums", "memo.exe"); err == nil || !strings.Contains(err.Error(), "校验失败") {
		t.Fatalf("tampered checksum error = %v", err)
	}
}

func TestUpdateServiceInstallWhenCurrent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v" + Version})
	}))
	defer server.Close()

	svc := NewUpdateService(nil, nil)
	svc.apiURL = server.URL
	if err := svc.Install(); err == nil || !strings.Contains(err.Error(), "最新") {
		t.Fatalf("install error = %v", err)
	}

	svc.mu.Lock()
	busy := svc.busy
	svc.mu.Unlock()
	if busy {
		t.Fatal("install failure left service busy")
	}
}
