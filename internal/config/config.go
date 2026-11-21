// Package config stores typed settings over the SQLite key/value table. The
// Notion token is deliberately absent: it lives in the OS keychain.
package config

import (
	"context"
	"strconv"

	"memo/internal/store"
)

// Keys used across the app. Values are strings on disk; helpers parse them.
const (
	KeySyncEnabled        = "sync.enabled"
	KeySyncPaused         = "sync.paused"
	KeyRetentionDays      = "retention.days"
	KeyRetentionImmediate = "retention.immediate"
	KeyTheme              = "ui.theme"
	KeyHotkey             = "ui.hotkey"
	KeyStartAtLogin       = "ui.start_at_login"
	KeyStickyX            = "ui.sticky_x"
	KeyStickyY            = "ui.sticky_y"
	KeyHotkeyError        = "ui.hotkey_error"
	KeyNotionConfigured   = "notion.configured"
	KeyNotionDataSourceID = "notion.data_source_id"
	KeyNotionDatabaseID   = "notion.database_id"
	KeyNotionParentPageID = "notion.parent_page_id"
	KeyNotionTitleProp    = "notion.title_property"
	KeyNotionWorkspace    = "notion.workspace_name"
	KeyWorkspaceFileLimit = "notion.file_limit"
	KeyDeviceID           = "device.id"
	KeyLastSyncAt         = "sync.last_success_at"
	KeyLastPullAt         = "sync.last_pull_at"
	KeyOnboarded          = "app.onboarded"
)

// Defaults are used when a key is absent.
var Defaults = map[string]string{
	KeyRetentionDays:      "30",
	KeyRetentionImmediate: "false",
	KeyTheme:              "light",
	KeyHotkey:             "CmdOrCtrl+Shift+Space",
	KeyStartAtLogin:       "false",
	KeyWorkspaceFileLimit: "5242880",
}

// Store is a typed view of the settings table.
type Store struct{ store *store.Store }

// New wraps a store.
func New(s *store.Store) *Store { return &Store{store: s} }

// Get returns a setting or its default.
func (c *Store) Get(ctx context.Context, key string) string {
	if c == nil || c.store == nil {
		return Defaults[key]
	}
	if v, ok, err := c.store.Setting(ctx, key); err == nil && ok {
		return v
	}
	return Defaults[key]
}

// GetDefault is Get with an explicit fallback.
func (c *Store) GetDefault(ctx context.Context, key, fallback string) string {
	if c == nil || c.store == nil {
		return fallback
	}
	if v, ok, err := c.store.Setting(ctx, key); err == nil && ok {
		return v
	}
	return fallback
}

// Bool reads a boolean setting.
func (c *Store) Bool(ctx context.Context, key string) bool {
	v, _ := strconv.ParseBool(c.Get(ctx, key))
	return v
}

// Int reads an integer setting.
func (c *Store) Int(ctx context.Context, key string) int {
	v, _ := strconv.Atoi(c.Get(ctx, key))
	return v
}

// Set writes a setting.
func (c *Store) Set(ctx context.Context, key, value string) error {
	return c.store.SetSetting(ctx, key, value)
}

// SetBool writes a boolean setting.
func (c *Store) SetBool(ctx context.Context, key string, value bool) error {
	return c.store.SetSetting(ctx, key, strconv.FormatBool(value))
}

// SetInt writes an integer setting.
func (c *Store) SetInt(ctx context.Context, key string, value int) error {
	return c.store.SetSetting(ctx, key, strconv.Itoa(value))
}

// All returns every stored setting.
func (c *Store) All(ctx context.Context) (map[string]string, error) {
	return c.store.Settings(ctx)
}
