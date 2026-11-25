// Package store is the single local source of truth: memos, tags, attachments,
// the sync outbox, settings, device state and the search index.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schemaVersion = 4

// Store wraps the SQLite database. All methods are safe for concurrent use.
type Store struct {
	db   *sql.DB
	path string
}

// Open opens (creating if needed) the database at path and runs migrations.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("store: create data dir: %w", err)
		}
	}
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

// DB exposes the handle to sibling packages that own query shapes (search,
// stats). It is deliberately not exposed through Wails.
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	if version >= schemaVersion {
		return nil
	}
	if version == 0 {
		if _, err := s.db.Exec(schemaV1); err != nil {
			return fmt.Errorf("store: migrate v1: %w", err)
		}
		if _, err := s.db.Exec(schemaV2); err != nil {
			return fmt.Errorf("store: migrate v2: %w", err)
		}
		if _, err := s.db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
			return fmt.Errorf("store: set schema version: %w", err)
		}
		return nil
	}
	if version < 2 {
		if _, err := s.db.Exec(schemaV2); err != nil {
			return fmt.Errorf("store: migrate v2: %w", err)
		}
	}
	if version < 3 {
		hasBody, err := s.hasColumn("memos", "body_md")
		if err != nil {
			return err
		}
		if !hasBody {
			legacy, err := s.hasColumn("memos", "body_json")
			if err != nil {
				return err
			}
			if legacy {
				var stale int
				if err := s.db.QueryRow(`SELECT COUNT(*) FROM memos
					WHERE body_json IS NOT NULL AND TRIM(body_json) NOT IN ('', '[]')`).Scan(&stale); err != nil {
					return fmt.Errorf("store: inspect legacy body: %w", err)
				}
				if stale > 0 {
					return fmt.Errorf("store: this profile stores memos as block JSON from an older version; " +
						"this release cannot read it and will not silently discard the bodies. " +
						"Reset local data or restore a backup made by this version")
				}
			}
			if _, err := s.db.Exec(`ALTER TABLE memos ADD COLUMN body_md TEXT NOT NULL DEFAULT ''`); err != nil {
				return fmt.Errorf("store: migrate v3: %w", err)
			}
		}
	}
	if version < 4 {
		hasPinned, err := s.hasColumn("memos", "pinned")
		if err != nil {
			return err
		}
		if !hasPinned {
			if _, err := s.db.Exec(`ALTER TABLE memos ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0`); err != nil {
				return fmt.Errorf("store: migrate v4: %w", err)
			}
		}
	}
	if _, err := s.db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
		return fmt.Errorf("store: set schema version: %w", err)
	}
	return nil
}

func (s *Store) hasColumn(table, column string) (bool, error) {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, fmt.Errorf("store: inspect %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

const schemaV1 = `
CREATE TABLE IF NOT EXISTS memos (
  id TEXT PRIMARY KEY,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  revision INTEGER NOT NULL DEFAULT 1,
  body_md TEXT NOT NULL DEFAULT '',
  body_text TEXT NOT NULL DEFAULT '',
  snippet TEXT NOT NULL DEFAULT '',
  deleted_at INTEGER,
  keep_local INTEGER NOT NULL DEFAULT 0,
  pinned INTEGER NOT NULL DEFAULT 0,
  local_state TEXT NOT NULL DEFAULT 'present',
  notion_page_id TEXT NOT NULL DEFAULT '',
  notion_synced_at INTEGER,
  last_hydrated_at INTEGER,
  base_revision INTEGER NOT NULL DEFAULT 0,
  content_hash TEXT NOT NULL DEFAULT '',
  sync_error TEXT NOT NULL DEFAULT '',
  block_stats TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_memos_created ON memos(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_memos_notion ON memos(notion_page_id);
CREATE INDEX IF NOT EXISTS idx_memos_deleted ON memos(deleted_at);

CREATE TABLE IF NOT EXISTS tags (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  normalized_name TEXT NOT NULL UNIQUE
);
CREATE INDEX IF NOT EXISTS idx_tags_normalized ON tags(normalized_name);

CREATE TABLE IF NOT EXISTS memo_tags (
  memo_id TEXT NOT NULL REFERENCES memos(id) ON DELETE CASCADE,
  tag_id TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (memo_id, tag_id)
);
CREATE INDEX IF NOT EXISTS idx_memo_tags_tag ON memo_tags(tag_id);

CREATE TABLE IF NOT EXISTS attachments (
  id TEXT PRIMARY KEY,
  memo_id TEXT NOT NULL REFERENCES memos(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  original_name TEXT NOT NULL,
  local_path TEXT NOT NULL DEFAULT '',
  thumb_path TEXT NOT NULL DEFAULT '',
  mime TEXT NOT NULL DEFAULT '',
  bytes INTEGER NOT NULL DEFAULT 0,
  width INTEGER NOT NULL DEFAULT 0,
  height INTEGER NOT NULL DEFAULT 0,
  sha256 TEXT NOT NULL DEFAULT '',
  notion_file_id TEXT NOT NULL DEFAULT '',
  notion_block_id TEXT NOT NULL DEFAULT '',
  uploaded_at INTEGER,
  evicted_at INTEGER
);
CREATE INDEX IF NOT EXISTS idx_attachments_memo ON attachments(memo_id);

CREATE TABLE IF NOT EXISTS outbox (
  memo_id TEXT PRIMARY KEY,
  op TEXT NOT NULL,
  revision INTEGER NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at INTEGER NOT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_outbox_next ON outbox(next_attempt_at);

CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS device_state (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  device_id TEXT NOT NULL,
  last_pull_at INTEGER,
  last_full_scan_at INTEGER,
  last_trash_sweep_at INTEGER
);

CREATE TABLE IF NOT EXISTS sync_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  at INTEGER NOT NULL,
  level TEXT NOT NULL,
  event TEXT NOT NULL,
  message TEXT NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS memos_fts USING fts5(
  memo_id UNINDEXED,
  body_text,
  tags_text,
  tokenize='trigram'
);
`

const schemaV2 = `
CREATE TABLE IF NOT EXISTS purged_pages (
  page_id TEXT PRIMARY KEY,
  purged_at INTEGER NOT NULL
);
`

const memoColumns = `id, created_at, updated_at, revision, body_md, body_text, snippet,
  deleted_at, keep_local, pinned, local_state, notion_page_id, notion_synced_at,
  last_hydrated_at, base_revision, content_hash, sync_error, block_stats`

func toMS(t time.Time) int64 { return t.UTC().UnixMilli() }

func fromMS(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

func nullableMS(t *time.Time) any {
	if t == nil {
		return nil
	}
	return toMS(*t)
}

func msPtr(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := fromMS(v.Int64)
	return &t
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func withTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// WipeLocalData deletes every memo, tag, attachment row and sync log. Settings
// and the device identity survive so the next launch starts clean.
func (s *Store) WipeLocalData(ctx context.Context) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		for _, stmt := range []string{
			`DELETE FROM memo_tags`,
			`DELETE FROM attachments`,
			`DELETE FROM outbox`,
			`DELETE FROM purged_pages`,
			`DELETE FROM memos`,
			`DELETE FROM memos_fts`,
			`DELETE FROM tags`,
			`DELETE FROM sync_log`,
		} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	})
}

// Log appends a bounded sync event. Tokens and bodies must never be passed in.
func (s *Store) Log(ctx context.Context, level, event, message string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sync_log (at, level, event, message) VALUES (?,?,?,?)`,
		toMS(time.Now()), level, event, message)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM sync_log WHERE id NOT IN (SELECT id FROM sync_log ORDER BY id DESC LIMIT 200)`)
	return err
}

// SyncLog is one recent sync event.
type SyncLog struct {
	ID      int64     `json:"id"`
	At      time.Time `json:"at"`
	Level   string    `json:"level"`
	Event   string    `json:"event"`
	Message string    `json:"message"`
}

// Logs returns recent sync events, newest first.
func (s *Store) Logs(ctx context.Context, limit int) ([]SyncLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.queryLogs(ctx, `SELECT id, at, level, event, message FROM sync_log ORDER BY id DESC LIMIT ?`, limit)
}

// Errors returns recent error-level events, newest first. Frontend reports use
// the same table, so one export contains the full reproduction trail.
func (s *Store) Errors(ctx context.Context, limit int) ([]SyncLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.queryLogs(ctx, `SELECT id, at, level, event, message FROM sync_log WHERE level = 'error' ORDER BY id DESC LIMIT ?`, limit)
}

// ClearLogs deletes every local sync and error log entry.
func (s *Store) ClearLogs(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sync_log`)
	return err
}

func (s *Store) queryLogs(ctx context.Context, query string, limit int) ([]SyncLog, error) {
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SyncLog, 0, limit)
	for rows.Next() {
		var e SyncLog
		var at int64
		if err := rows.Scan(&e.ID, &at, &e.Level, &e.Event, &e.Message); err != nil {
			return nil, err
		}
		e.At = fromMS(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}
