package store

import (
	"context"
	"database/sql"
	"time"

	"memo/internal/domain"
)

// Setting reads one key. The second result reports presence.
func (s *Store) Setting(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return value, err == nil, err
}

// SetSetting upserts a key/value pair.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// Settings returns every stored setting.
func (s *Store) Settings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// DeleteSetting removes a key.
func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key=?`, key)
	return err
}

// EnsureDevice creates the single device row if this is the first run.
func (s *Store) EnsureDevice(ctx context.Context, deviceID string) (domain.DeviceState, error) {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO device_state (id, device_id) VALUES (1, ?)`, deviceID)
	if err != nil {
		return domain.DeviceState{}, err
	}
	return s.DeviceState(ctx)
}

// DeviceState returns the current cursor row.
func (s *Store) DeviceState(ctx context.Context) (domain.DeviceState, error) {
	var d domain.DeviceState
	var pull, full, sweep sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT device_id, last_pull_at, last_full_scan_at, last_trash_sweep_at
		FROM device_state WHERE id=1`).Scan(&d.DeviceID, &pull, &full, &sweep)
	if err != nil {
		return d, err
	}
	d.LastPullAt = msPtr(pull)
	d.LastFullScanAt = msPtr(full)
	d.LastTrashSweep = msPtr(sweep)
	return d, nil
}

// SetLastPull records the pull cursor.
func (s *Store) SetLastPull(ctx context.Context, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE device_state SET last_pull_at=? WHERE id=1`, toMS(at))
	return err
}

// SetLastFullScan records a completed full scan.
func (s *Store) SetLastFullScan(ctx context.Context, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE device_state SET last_full_scan_at=? WHERE id=1`, toMS(at))
	return err
}

// SetLastTrashSweep records the trash sweep clock.
func (s *Store) SetLastTrashSweep(ctx context.Context, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE device_state SET last_trash_sweep_at=? WHERE id=1`, toMS(at))
	return err
}
