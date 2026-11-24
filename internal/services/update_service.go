package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultUpdateAPI = "https://api.github.com/repos/olianate/memo/releases/latest"
	releasesPage     = "https://github.com/olianate/memo/releases"
	updateUserAgent  = "memo-updater"
)

// UpdateInfo describes the newest published release and whether this build can
// install it in place.
type UpdateInfo struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	Available      bool   `json:"available"`
	Notes          string `json:"notes"`
	PublishedAt    string `json:"published_at"`
	ReleaseURL     string `json:"release_url"`
	AssetName      string `json:"asset_name"`
	AssetSize      int64  `json:"asset_size"`
	CanInstall     bool   `json:"can_install"`
	CheckedAt      string `json:"checked_at"`

	assetURL    string
	checksumURL string
}

// UpdateProgress is emitted while a downloaded update is verified and started.
type UpdateProgress struct {
	Phase   string  `json:"phase"`
	Percent float64 `json:"percent"`
	Message string  `json:"message"`
}

// UpdateHooks are the application actions the updater needs.
type UpdateHooks interface {
	Quit()
	OpenURL(url string) error
}

// UpdateService checks GitHub Releases and installs a newer published build.
// Installers are not code-signed, so the OS may still show a confirmation.
type UpdateService struct {
	client    *http.Client
	apiURL    string
	hooks     UpdateHooks
	emit      func(string, any)
	mu        sync.Mutex
	last      UpdateInfo
	busy      bool
	announced string
}

// NewUpdateService builds the update boundary.
func NewUpdateService(hooks UpdateHooks, emit func(string, any)) *UpdateService {
	return &UpdateService{
		client: &http.Client{Timeout: 15 * time.Minute},
		apiURL: defaultUpdateAPI,
		hooks:  hooks,
		emit:   emit,
	}
}

// Start checks for updates shortly after launch and then periodically.
//
//wails:ignore
func (s *UpdateService) Start(ctx context.Context) {
	go func() {
		timer := time.NewTimer(15 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		_, _ = s.check(true)

		ticker := time.NewTicker(12 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = s.check(true)
			}
		}
	}()
}

// Check asks GitHub for the latest release. Errors are safe to show in the UI.
func (s *UpdateService) Check() (UpdateInfo, error) {
	return s.check(true)
}

func (s *UpdateService) check(announce bool) (UpdateInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	release, err := s.fetchRelease(ctx)
	if err != nil {
		return UpdateInfo{CurrentVersion: Version}, err
	}

	latest := normalizeVersion(release.TagName)
	asset, hasAsset := selectReleaseAsset(release.Assets, runtime.GOOS, runtime.GOARCH, windowsInstalledByMSI())
	info := UpdateInfo{
		CurrentVersion: Version,
		LatestVersion:  latest,
		Available:      compareVersions(latest, Version) > 0,
		Notes:          strings.TrimSpace(release.Body),
		PublishedAt:    release.PublishedAt.Format(time.RFC3339),
		ReleaseURL:     release.HTMLURL,
		CheckedAt:      time.Now().Format(time.RFC3339),
	}
	if info.ReleaseURL == "" {
		info.ReleaseURL = releasesPage
	}
	if hasAsset {
		info.AssetName = asset.Name
		info.AssetSize = asset.Size
	}
	info.CanInstall = info.Available && hasAsset && autoInstallSupported()

	var checksumURL string
	if hasAsset {
		for _, candidate := range release.Assets {
			if strings.EqualFold(candidate.Name, "SHA256SUMS.txt") {
				checksumURL = candidate.BrowserDownloadURL
				break
			}
		}
	}
	if hasAsset {
		info.assetURL = asset.BrowserDownloadURL
	}
	info.checksumURL = checksumURL

	s.mu.Lock()
	s.last = info
	announce = announce && info.Available && s.announced != latest
	if announce {
		s.announced = latest
	}
	s.mu.Unlock()

	if announce && s.emit != nil {
		s.emit("update:available", info)
	}
	return info, nil
}

// Status returns the result of the most recent check without touching the network.
func (s *UpdateService) Status() UpdateInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	info := s.last
	if info.CurrentVersion == "" {
		info.CurrentVersion = Version
	}
	return info
}

// Install downloads, verifies and starts the installer for the cached update.
// The app quits shortly after the installer starts; the helper waits for the
// process to exit, installs, and relaunches memo.
func (s *UpdateService) Install() error {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return errors.New("更新正在进行中")
	}
	s.busy = true
	s.mu.Unlock()

	ok := false
	defer func() {
		if !ok {
			s.mu.Lock()
			s.busy = false
			s.mu.Unlock()
		}
	}()

	info := s.Status()
	if !info.Available || info.AssetName == "" {
		checked, err := s.check(false)
		if err != nil {
			return err
		}
		info = checked
	}
	if !info.Available {
		return errors.New("当前已是最新版本")
	}
	if info.AssetName == "" || !autoInstallSupported() {
		return errors.New("当前平台暂不支持自动安装，请前往发布页手动下载")
	}

	assetURL := info.assetURL
	checksumURL := info.checksumURL
	if assetURL == "" {
		return errors.New("找不到适用于当前平台的更新包")
	}

	dir, err := os.MkdirTemp("", "memo-update-*")
	if err != nil {
		return fmt.Errorf("无法创建更新临时目录：%w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(dir)
		}
	}()

	installer := filepath.Join(dir, filepath.Base(info.AssetName))
	s.progress("downloading", 0, "正在准备下载更新包…")
	if err := s.downloadAsset(assetURL, installer, info.AssetSize); err != nil {
		return err
	}
	if checksumURL != "" {
		s.progress("verifying", 72, "正在校验更新包…")
		if err := s.verifyChecksum(installer, checksumURL, info.AssetName); err != nil {
			return err
		}
	}

	s.progress("installing", 90, "正在启动安装程序，应用即将退出…")
	if err := s.launchInstaller(installer); err != nil {
		return err
	}
	cleanup = false
	ok = true

	s.progress("restarting", 100, "安装完成后将自动重启 memo")
	go func() {
		time.Sleep(2 * time.Second)
		if s.hooks != nil {
			s.hooks.Quit()
		}
	}()
	return nil
}

// OpenReleasePage opens the checked release, or the releases index, in the
// system browser.
func (s *UpdateService) OpenReleasePage() error {
	s.mu.Lock()
	url := s.last.ReleaseURL
	s.mu.Unlock()
	if url == "" {
		url = releasesPage
	}
	if s.hooks == nil {
		return errors.New("当前环境无法打开发布页")
	}
	return s.hooks.OpenURL(url)
}

func (s *UpdateService) progress(phase string, percent float64, message string) {
	if s.emit == nil {
		return
	}
	s.emit("update:progress", UpdateProgress{Phase: phase, Percent: percent, Message: message})
}

func (s *UpdateService) httpClient() *http.Client {
	if s.client != nil {
		return s.client
	}
	return http.DefaultClient
}

func (s *UpdateService) api() string {
	if s.apiURL != "" {
		return s.apiURL
	}
	return defaultUpdateAPI
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type githubRelease struct {
	TagName     string         `json:"tag_name"`
	Body        string         `json:"body"`
	HTMLURL     string         `json:"html_url"`
	PublishedAt time.Time      `json:"published_at"`
	Draft       bool           `json:"draft"`
	Prerelease  bool           `json:"prerelease"`
	Assets      []releaseAsset `json:"assets"`
}

func (s *UpdateService) fetchRelease(ctx context.Context) (githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.api(), nil)
	if err != nil {
		return githubRelease{}, fmt.Errorf("无法创建更新请求：%w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", updateUserAgent+"/"+Version)

	resp, err := s.httpClient().Do(req)
	if err != nil {
		return githubRelease{}, fmt.Errorf("无法连接更新服务器，请检查网络：%w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return githubRelease{}, errors.New("暂时没有可用的发布版本")
	case http.StatusForbidden, http.StatusTooManyRequests:
		return githubRelease{}, errors.New("GitHub 暂时限制了更新检查，请稍后再试")
	default:
		return githubRelease{}, fmt.Errorf("更新检查失败（HTTP %d）", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&release); err != nil {
		return githubRelease{}, fmt.Errorf("无法解析更新信息：%w", err)
	}
	if release.TagName == "" {
		return githubRelease{}, errors.New("更新信息缺少版本号")
	}
	return release, nil
}

func (s *UpdateService) downloadAsset(url, dest string, expected int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("无法创建下载请求：%w", err)
	}
	req.Header.Set("User-Agent", updateUserAgent+"/"+Version)
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("下载更新包失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载更新包失败（HTTP %d）", resp.StatusCode)
	}

	total := resp.ContentLength
	if total <= 0 {
		total = expected
	}
	file, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("无法写入更新包：%w", err)
	}
	defer file.Close()

	buf := make([]byte, 64*1024)
	var written int64
	lastPercent := -1
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := file.Write(buf[:n]); err != nil {
				return fmt.Errorf("无法写入更新包：%w", err)
			}
			written += int64(n)
			if total > 0 {
				percent := int(float64(written) / float64(total) * 70)
				if percent > 70 {
					percent = 70
				}
				if percent != lastPercent {
					lastPercent = percent
					s.progress("downloading", float64(percent), fmt.Sprintf("正在下载更新包… %d%%", percent))
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("下载更新包失败：%w", readErr)
		}
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("无法写入更新包：%w", err)
	}
	if total > 0 && written != total {
		return fmt.Errorf("更新包下载不完整（%d/%d 字节）", written, total)
	}
	return nil
}

func (s *UpdateService) verifyChecksum(path, checksumURL, assetName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return fmt.Errorf("无法创建校验请求：%w", err)
	}
	req.Header.Set("User-Agent", updateUserAgent+"/"+Version)
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("下载校验文件失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载校验文件失败（HTTP %d）", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("读取校验文件失败：%w", err)
	}

	expected, ok := checksumFor(data, assetName)
	if !ok {
		return errors.New("更新包校验失败：校验文件缺少对应条目")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("无法读取更新包：%w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("无法校验更新包：%w", err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(expected, actual) {
		return errors.New("更新包校验失败，已取消安装")
	}
	return nil
}

func checksumFor(data []byte, assetName string) (string, bool) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if filepath.Base(name) == assetName {
			return strings.TrimSpace(fields[0]), true
		}
	}
	return "", false
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	return strings.TrimPrefix(version, "V")
}

// compareVersions compares dotted versions, treating a prerelease as older
// than the matching release (1.2.0-beta.1 < 1.2.0).
func compareVersions(a, b string) int {
	aNumbers, aPre := parseVersion(a)
	bNumbers, bPre := parseVersion(b)
	for i := range aNumbers {
		if aNumbers[i] != bNumbers[i] {
			if aNumbers[i] > bNumbers[i] {
				return 1
			}
			return -1
		}
	}
	switch {
	case len(aPre) == 0 && len(bPre) == 0:
		return 0
	case len(aPre) == 0:
		return 1
	case len(bPre) == 0:
		return -1
	}
	for i := 0; i < len(aPre) && i < len(bPre); i++ {
		if aPre[i] == bPre[i] {
			continue
		}
		aNumber, aErr := strconv.Atoi(aPre[i])
		bNumber, bErr := strconv.Atoi(bPre[i])
		switch {
		case aErr == nil && bErr == nil:
			if aNumber > bNumber {
				return 1
			}
			return -1
		case aErr == nil:
			return -1
		case bErr == nil:
			return 1
		default:
			if aPre[i] > bPre[i] {
				return 1
			}
			return -1
		}
	}
	if len(aPre) > len(bPre) {
		return 1
	}
	if len(aPre) < len(bPre) {
		return -1
	}
	return 0
}

func parseVersion(version string) ([3]int, []string) {
	var numbers [3]int
	version = normalizeVersion(version)
	if i := strings.IndexByte(version, '+'); i >= 0 {
		version = version[:i]
	}
	var prerelease []string
	if i := strings.IndexByte(version, '-'); i >= 0 {
		prerelease = strings.Split(version[i+1:], ".")
		version = version[:i]
	}
	parts := strings.Split(version, ".")
	for i := 0; i < len(parts) && i < len(numbers); i++ {
		number, err := strconv.Atoi(parts[i])
		if err == nil {
			numbers[i] = number
		}
	}
	return numbers, prerelease
}

func selectReleaseAsset(assets []releaseAsset, goos, goarch string, preferMSI bool) (releaseAsset, bool) {
	var candidates []string
	switch goos {
	case "windows":
		if preferMSI {
			if goarch == "arm64" {
				candidates = append(candidates, "memo-windows-arm64.msi")
			}
			candidates = append(candidates, "memo-windows-amd64.msi")
		} else {
			if goarch == "arm64" {
				candidates = append(candidates, "memo-windows-arm64-installer.exe")
			}
			candidates = append(candidates, "memo-windows-amd64-installer.exe")
		}
	case "darwin":
		candidates = append(candidates, "memo-macos-universal.dmg")
		candidates = append(candidates, "memo-macos-"+goarch+".dmg")
	default:
		return releaseAsset{}, false
	}
	if preferMSI {
		candidates = append(candidates, "memo-windows-amd64-installer.exe")
	}
	for _, name := range candidates {
		for _, asset := range assets {
			if strings.EqualFold(asset.Name, name) && asset.BrowserDownloadURL != "" {
				return asset, true
			}
		}
	}
	return releaseAsset{}, false
}

func autoInstallSupported() bool {
	return runtime.GOOS == "windows" || runtime.GOOS == "darwin"
}
