package store

import (
	"context"
	"time"
)

// MarkPagePurged records a Notion page whose local memo was permanently
// deleted. The sync worker writes the tombstone to Notion later and skips the
// page if it is ever pulled again in the meantime.
func (s *Store) MarkPagePurged(ctx context.Context, pageID string) error {
	if pageID == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO purged_pages(page_id, purged_at) VALUES(?, ?)`,
		pageID, toMS(time.Now().UTC()))
	return err
}

// PurgedPageIDs lists pages waiting for a Notion tombstone, oldest first.
func (s *Store) PurgedPageIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT page_id FROM purged_pages ORDER BY purged_at, page_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ClearPurgedPage removes a completed tombstone marker.
func (s *Store) ClearPurgedPage(ctx context.Context, pageID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM purged_pages WHERE page_id=?`, pageID)
	return err
}

// IsPagePurged reports whether a remote page should be ignored because its
// local memo was permanently deleted.
func (s *Store) IsPagePurged(ctx context.Context, pageID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM purged_pages WHERE page_id=?`, pageID).Scan(&count)
	return count > 0, err
}
