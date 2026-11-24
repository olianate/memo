package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/olianate/memo/internal/domain"
)

// ApplyRemoteDelete applies a Status=deleted tombstone without enqueueing a
// write. The remote copy is the winner; the local body is kept for the trash
// view but removed from search.
func (s *Store) ApplyRemoteDelete(ctx context.Context, id string, updatedAt time.Time, revision int64) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE memos SET deleted_at=?, updated_at=?, revision=CASE WHEN revision>? THEN revision ELSE ? END,
			notion_synced_at=?, base_revision=? WHERE id=?`,
			toMS(updatedAt), toMS(updatedAt), revision, revision, toMS(updatedAt), revision, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM memos_fts WHERE memo_id=?`, id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM outbox WHERE memo_id=?`, id)
		return err
	})
}

// HydrateMemo restores a body fetched from Notion without changing the memo's
// revision or timestamps. The retention clock resets via last_hydrated_at.
func (s *Store) HydrateMemo(ctx context.Context, id string, body string, at time.Time) error {
	derived := deriveBody(body, nil)
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE memos SET body_md=?, body_text=?, snippet=?, content_hash=?,
			block_stats=?, local_state=?, last_hydrated_at=? WHERE id=?`,
			body, derived.Text, derived.Snippet, derived.Hash,
			domain.MarshalBlockStats(derived.Stats), string(domain.LocalPresent), toMS(at), id); err != nil {
			return err
		}
		tags, err := tagNamesTx(ctx, tx, id)
		if err != nil {
			return err
		}
		return indexMemo(ctx, tx, id, derived.Text, tags)
	})
}

// UpdateBody rewrites a memo's body after a pull without changing its
// revision, timestamps, local state or retention clock.
func (s *Store) UpdateBody(ctx context.Context, id string, body string) error {
	derived := deriveBody(body, nil)
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE memos SET body_md=?, body_text=?, snippet=?, content_hash=?,
			block_stats=? WHERE id=?`,
			body, derived.Text, derived.Snippet, derived.Hash,
			domain.MarshalBlockStats(derived.Stats), id); err != nil {
			return err
		}
		tags, err := tagNamesTx(ctx, tx, id)
		if err != nil {
			return err
		}
		return indexMemo(ctx, tx, id, derived.Text, tags)
	})
}

// SetAttachmentNotionBlock records the Notion block that carries an attachment.
func (s *Store) SetAttachmentNotionBlock(ctx context.Context, id, blockID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE attachments SET notion_block_id=? WHERE id=?`, blockID, id)
	return err
}

// FindAttachmentByBlockID finds an already-imported attachment block.
func (s *Store) FindAttachmentByBlockID(ctx context.Context, memoID, blockID string) (*domain.Attachment, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, memo_id, kind, original_name, local_path, thumb_path, mime,
		bytes, width, height, sha256, notion_file_id, notion_block_id, uploaded_at, evicted_at
		FROM attachments WHERE memo_id=? AND notion_block_id=?`, memoID, blockID)
	a, err := scanAttachment(row)
	if err == sql.ErrNoRows {
		return nil, domain.ErrNotFound
	}
	return a, err
}
