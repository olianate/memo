package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/olianate/memo/internal/config"
	"github.com/olianate/memo/internal/store"
)

// Hooks are the small set of platform actions the settings screen needs.
type Hooks interface {
	ApplyHotkey(accelerator string) error
	ApplyStartAtLogin(enabled bool) error
	OpenPath(path string) error
}

// SettingsDTO mirrors the persisted settings for the UI.
type SettingsDTO struct {
	Theme              string `json:"theme"`
	Hotkey             string `json:"hotkey"`
	StartAtLogin       bool   `json:"start_at_login"`
	RetentionDays      int    `json:"retention_days"`
	ImmediateEviction  bool   `json:"immediate_eviction"`
	SyncEnabled        bool   `json:"sync_enabled"`
	SyncPaused         bool   `json:"sync_paused"`
	NotionConnected    bool   `json:"notion_connected"`
	WorkspaceName      string `json:"workspace_name"`
	WorkspaceFileLimit int64  `json:"workspace_file_limit"`
	DataDir            string `json:"data_dir"`
	Onboarded          bool   `json:"onboarded"`
	HotkeyError        string `json:"hotkey_error,omitempty"`
	Version            string `json:"version"`
}

// BackupResult describes an exported local backup.
type BackupResult struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	At    string `json:"at"`
}

// TokenClearer removes the Notion PAT from the OS keychain.
type TokenClearer interface {
	ClearNotionToken(context.Context) error
}

// SettingsService owns preferences, backup, logs and index rebuild.
type SettingsService struct {
	store   *store.Store
	cfg     *config.Store
	dataDir string
	hooks   Hooks
	secrets TokenClearer
}

// NewSettingsService builds the settings boundary.
func NewSettingsService(st *store.Store, cfg *config.Store, dataDir string, hooks Hooks, secrets TokenClearer) *SettingsService {
	return &SettingsService{store: st, cfg: cfg, dataDir: dataDir, hooks: hooks, secrets: secrets}
}

// Get returns all settings.
func (s *SettingsService) Get() SettingsDTO {
	ctx := context.Background()
	limit := int64(s.cfg.Int(ctx, config.KeyWorkspaceFileLimit))
	return SettingsDTO{
		Theme:              s.cfg.Get(ctx, config.KeyTheme),
		Hotkey:             s.cfg.Get(ctx, config.KeyHotkey),
		StartAtLogin:       s.cfg.Bool(ctx, config.KeyStartAtLogin),
		RetentionDays:      s.cfg.Int(ctx, config.KeyRetentionDays),
		ImmediateEviction:  s.cfg.Bool(ctx, config.KeyRetentionImmediate),
		SyncEnabled:        s.cfg.Bool(ctx, config.KeySyncEnabled),
		SyncPaused:         s.cfg.Bool(ctx, config.KeySyncPaused),
		NotionConnected:    s.cfg.Bool(ctx, config.KeyNotionConfigured),
		WorkspaceName:      s.cfg.Get(ctx, config.KeyNotionWorkspace),
		WorkspaceFileLimit: limit,
		DataDir:            s.dataDir,
		Onboarded:          s.cfg.Bool(ctx, config.KeyOnboarded),
		HotkeyError:        s.cfg.Get(ctx, config.KeyHotkeyError),
		Version:            Version,
	}
}

// SetTheme persists light, dark, or system.
func (s *SettingsService) SetTheme(theme string) error {
	switch theme {
	case "light", "dark", "system":
	default:
		return errors.New("theme must be light, dark, or system")
	}
	return s.cfg.Set(context.Background(), config.KeyTheme, theme)
}

// SetHotkey applies and persists a new capture accelerator.
func (s *SettingsService) SetHotkey(accelerator string) error {
	accelerator = strings.TrimSpace(accelerator)
	if accelerator == "" {
		return errors.New("hotkey is required")
	}
	if s.hooks != nil {
		if err := s.hooks.ApplyHotkey(accelerator); err != nil {
			return err
		}
	}
	return s.cfg.Set(context.Background(), config.KeyHotkey, accelerator)
}

// SetStartAtLogin applies and persists login startup.
func (s *SettingsService) SetStartAtLogin(enabled bool) error {
	if s.hooks != nil {
		if err := s.hooks.ApplyStartAtLogin(enabled); err != nil {
			return err
		}
	}
	return s.cfg.SetBool(context.Background(), config.KeyStartAtLogin, enabled)
}

// SetRetention changes the eviction window. Accepted: 7, 30, 90, or -1 never.
func (s *SettingsService) SetRetention(days int, immediate bool) error {
	switch days {
	case 7, 30, 90, -1:
	default:
		return errors.New("retention must be 7, 30, 90, or never")
	}
	ctx := context.Background()
	if err := s.cfg.SetInt(ctx, config.KeyRetentionDays, days); err != nil {
		return err
	}
	return s.cfg.SetBool(ctx, config.KeyRetentionImmediate, immediate)
}

// DeleteLocalData wipes memos, tags, attachments, the index and the sync log.
// It disables sync so a deleted local library is not immediately re-pulled from
// Notion. Notion itself is never modified.
func (s *SettingsService) DeleteLocalData() error {
	ctx := context.Background()
	dir := filepath.Join(s.dataDir, "attachments")
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := s.store.WipeLocalData(ctx); err != nil {
		return err
	}
	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.secrets != nil {
		note(s.secrets.ClearNotionToken(ctx))
	}
	note(s.cfg.SetBool(ctx, config.KeySyncEnabled, false))
	note(s.cfg.SetBool(ctx, config.KeySyncPaused, false))
	note(s.cfg.SetBool(ctx, config.KeyNotionConfigured, false))
	for _, key := range []string{
		config.KeyNotionDataSourceID, config.KeyNotionDatabaseID, config.KeyNotionParentPageID,
		config.KeyNotionTitleProp, config.KeyNotionWorkspace,
	} {
		note(s.cfg.Set(ctx, key, ""))
	}
	return firstErr
}

// RebuildIndex re-creates the FTS rows from local metadata and bodies.
func (s *SettingsService) RebuildIndex() error {
	return s.store.RebuildIndex(context.Background())
}

// CompleteOnboarding marks first-run setup as done (skip or Notion connect).
func (s *SettingsService) CompleteOnboarding() error {
	return s.cfg.SetBool(context.Background(), config.KeyOnboarded, true)
}

// DataDir returns where the database and attachments live.
func (s *SettingsService) DataDir() string { return s.dataDir }

// OpenDataDir reveals the data directory in the OS file manager.
func (s *SettingsService) OpenDataDir() error {
	if s.hooks == nil {
		return nil
	}
	return s.hooks.OpenPath(s.dataDir)
}

// ExportBackup copies the SQLite database and attachment files into a
// timestamped folder under the data directory.
func (s *SettingsService) ExportBackup() (BackupResult, error) {
	dir, err := s.backupDir("backup")
	if err != nil {
		return BackupResult{}, err
	}
	dbPath := filepath.Join(dir, "memo.db")
	if err := s.vacuumInto(dbPath); err != nil {
		return BackupResult{}, err
	}
	if err := copyDir(filepath.Join(s.dataDir, "attachments"), filepath.Join(dir, "attachments")); err != nil && !os.IsNotExist(err) {
		return BackupResult{}, err
	}
	size := dirSize(dir)
	return BackupResult{Path: dir, Bytes: size, At: time.Now().Local().Format(time.RFC3339)}, nil
}

// ExportLogs writes the bounded sync log to a text file.
func (s *SettingsService) ExportLogs() (string, error) {
	dir, err := s.backupDir("logs")
	if err != nil {
		return "", err
	}
	logs, err := s.store.Logs(context.Background(), 200)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, l := range logs {
		fmt.Fprintf(&sb, "%s\t%s\t%s\t%s\n", l.At.Local().Format(time.RFC3339), l.Level, l.Event, l.Message)
	}
	path := filepath.Join(dir, "sync.log")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func (s *SettingsService) backupDir(kind string) (string, error) {
	dir := filepath.Join(s.dataDir, "backups", kind+"-"+time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func (s *SettingsService) vacuumInto(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	escaped := strings.ReplaceAll(path, "'", "''")
	_, err := s.store.DB().Exec(`VACUUM INTO '` + escaped + `'`)
	return err
}

func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}

func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}
