package store

import (
	"context"
	"database/sql"
	"time"

	"memo/internal/domain"
)

// CreateAttachment inserts attachment metadata. Bytes are written by the
// attachment service; this row is the durable record.
func (s *Store) CreateAttachment(ctx context.Context, a domain.Attachment) (*domain.Attachment, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO attachments
		(id, memo_id, kind, original_name, local_path, thumb_path, mime, bytes, width, height, sha256)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.MemoID, string(a.Kind), a.OriginalName, a.LocalPath, a.ThumbPath, a.MIME,
		a.Bytes, a.Width, a.Height, a.SHA256)
	if err != nil {
		return nil, err
	}
	return s.GetAttachment(ctx, a.ID)
}

// GetAttachment loads attachment metadata by id.
func (s *Store) GetAttachment(ctx context.Context, id string) (*domain.Attachment, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, memo_id, kind, original_name, local_path, thumb_path, mime,
		bytes, width, height, sha256, notion_file_id, notion_block_id, uploaded_at, evicted_at
		FROM attachments WHERE id=?`, id)
	a, err := scanAttachment(row)
	if err == sql.ErrNoRows {
		return nil, domain.ErrNotFound
	}
	return a, err
}

// AttachmentIDsForMemo returns ids in insertion order for a memo.
func (s *Store) AttachmentIDsForMemo(ctx context.Context, memoID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM attachments WHERE memo_id=? ORDER BY rowid`, memoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// UpdateAttachmentPaths records where an attachment's bytes and thumbnail live.
func (s *Store) UpdateAttachmentPaths(ctx context.Context, id, localPath, thumbPath string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE attachments SET local_path=?, thumb_path=?, evicted_at=NULL WHERE id=?`, localPath, thumbPath, id)
	return err
}

// MarkAttachmentUploaded records a successful Notion upload.
func (s *Store) MarkAttachmentUploaded(ctx context.Context, id, fileID, blockID string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE attachments SET notion_file_id=?, notion_block_id=?, uploaded_at=? WHERE id=?`,
		fileID, blockID, toMS(at), id)
	return err
}

// MarkAttachmentEvicted removes local paths and stamps eviction metadata.
func (s *Store) MarkAttachmentEvicted(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE attachments SET local_path='', thumb_path='', evicted_at=? WHERE id=?`, toMS(at), id)
	return err
}

// DeleteAttachment removes metadata and detaches it from the memo.
func (s *Store) DeleteAttachment(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM attachments WHERE id=?`, id)
	return err
}

// PendingUploads lists attachments that still need a Notion file upload.
func (s *Store) PendingUploads(ctx context.Context, memoID string) ([]domain.Attachment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, memo_id, kind, original_name, local_path, thumb_path, mime,
		bytes, width, height, sha256, notion_file_id, notion_block_id, uploaded_at, evicted_at
		FROM attachments WHERE memo_id=? AND notion_file_id='' ORDER BY rowid`, memoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Attachment
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// AllAttachments returns every attachment record for diagnostics and recovery.
// It is not called on the hot path.
func (s *Store) AllAttachments(ctx context.Context) ([]domain.Attachment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, memo_id, kind, original_name, local_path, thumb_path, mime,
		bytes, width, height, sha256, notion_file_id, notion_block_id, uploaded_at, evicted_at
		FROM attachments ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Attachment
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// AttachmentBytes returns total attachment bytes grouped by kind.
func (s *Store) AttachmentBytes(ctx context.Context) (images, files int64, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kind, COALESCE(SUM(bytes),0) FROM attachments GROUP BY kind`)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var n int64
		if err := rows.Scan(&kind, &n); err != nil {
			return 0, 0, err
		}
		if kind == string(domain.AttachmentImage) {
			images = n
		} else {
			files = n
		}
	}
	return images, files, rows.Err()
}
