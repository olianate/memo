package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/olianate/memo/internal/domain"
)

type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// MemoInput is a local create request. The store derives body text, snippet and
// content hash so every caller writes the same search representation.
type MemoInput struct {
	ID            string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Body          string
	Tags          []string
	AttachmentIDs []string
	KeepLocal     bool
}

// CreateMemo writes a memo, its tags, index row and outbox row atomically.
func (s *Store) CreateMemo(ctx context.Context, in MemoInput) (*domain.Memo, error) {
	if in.ID == "" {
		in.ID = domain.NewID()
	}
	now := time.Now().UTC()
	if in.CreatedAt.IsZero() {
		in.CreatedAt = now
	}
	if in.UpdatedAt.IsZero() {
		in.UpdatedAt = in.CreatedAt
	}
	derived := deriveBody(in.Body, in.Tags)
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO memos
			(id, created_at, updated_at, revision, body_md, body_text, snippet, keep_local, local_state, content_hash, block_stats)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			in.ID, toMS(in.CreatedAt), toMS(in.UpdatedAt), 1, in.Body, derived.Text,
			derived.Snippet, boolInt(in.KeepLocal), string(domain.LocalPresent), derived.Hash,
			domain.MarshalBlockStats(derived.Stats))
		if err != nil {
			return err
		}
		if err := setMemoTags(ctx, tx, in.ID, in.Tags); err != nil {
			return err
		}
		if err := attachToMemo(ctx, tx, in.ID, in.AttachmentIDs); err != nil {
			return err
		}
		if err := indexMemo(ctx, tx, in.ID, derived.Text, in.Tags); err != nil {
			return err
		}
		return enqueueTx(ctx, tx, in.ID, domain.OutboxUpsert, 1, time.Now().UTC())
	})
	if err != nil {
		return nil, fmt.Errorf("store: create memo: %w", err)
	}
	return s.GetMemo(ctx, in.ID)
}

// UpdateMemo edits the body/tags of a memo and bumps its revision.
func (s *Store) UpdateMemo(ctx context.Context, id string, body string, tags []string, keepLocal *bool) (*domain.Memo, error) {
	derived := deriveBody(body, tags)
	now := time.Now().UTC()
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		var revision int64
		if err := tx.QueryRowContext(ctx, `SELECT revision FROM memos WHERE id=?`, id).Scan(&revision); err != nil {
			if err == sql.ErrNoRows {
				return domain.ErrNotFound
			}
			return err
		}
		revision++
		q := `UPDATE memos SET body_md=?, body_text=?, snippet=?, updated_at=?, revision=?, content_hash=?, block_stats=?, local_state=?, sync_error='' WHERE id=?`
		args := []any{body, derived.Text, derived.Snippet, toMS(now), revision, derived.Hash, domain.MarshalBlockStats(derived.Stats), string(domain.LocalPresent), id}
		if keepLocal != nil {
			q = `UPDATE memos SET body_md=?, body_text=?, snippet=?, updated_at=?, revision=?, content_hash=?, block_stats=?, local_state=?, sync_error='', keep_local=? WHERE id=?`
			args = []any{body, derived.Text, derived.Snippet, toMS(now), revision, derived.Hash, domain.MarshalBlockStats(derived.Stats), string(domain.LocalPresent), boolInt(*keepLocal), id}
		}
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return err
		}
		if err := setMemoTags(ctx, tx, id, tags); err != nil {
			return err
		}
		if err := indexMemo(ctx, tx, id, derived.Text, tags); err != nil {
			return err
		}
		return enqueueTx(ctx, tx, id, domain.OutboxUpsert, revision, time.Now().UTC())
	})
	if err != nil {
		return nil, fmt.Errorf("store: update memo: %w", err)
	}
	return s.GetMemo(ctx, id)
}

// SoftDelete tombstones a memo locally and queues the Status=deleted write.
func (s *Store) SoftDelete(ctx context.Context, id string) error {
	now := time.Now().UTC()
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		var revision int64
		var pageID string
		var deleted sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT revision, notion_page_id, deleted_at FROM memos WHERE id=?`, id).
			Scan(&revision, &pageID, &deleted); err != nil {
			if err == sql.ErrNoRows {
				return domain.ErrNotFound
			}
			return err
		}
		if deleted.Valid {
			return nil
		}
		revision++
		if _, err := tx.ExecContext(ctx, `UPDATE memos SET deleted_at=?, updated_at=?, revision=? WHERE id=?`,
			toMS(now), toMS(now), revision, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM memos_fts WHERE memo_id=?`, id); err != nil {
			return err
		}
		if pageID == "" {
			_, err := tx.ExecContext(ctx, `DELETE FROM outbox WHERE memo_id=?`, id)
			return err
		}
		return enqueueTx(ctx, tx, id, domain.OutboxDelete, revision, time.Now().UTC())
	})
}

// Restore undoes a soft delete.
func (s *Store) Restore(ctx context.Context, id string) (*domain.Memo, error) {
	now := time.Now().UTC()
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		var revision int64
		var bodyText string
		if err := tx.QueryRowContext(ctx, `SELECT revision, body_text FROM memos WHERE id=?`, id).Scan(&revision, &bodyText); err != nil {
			if err == sql.ErrNoRows {
				return domain.ErrNotFound
			}
			return err
		}
		revision++
		if _, err := tx.ExecContext(ctx, `UPDATE memos SET deleted_at=NULL, updated_at=?, revision=? WHERE id=?`, toMS(now), revision, id); err != nil {
			return err
		}
		tags, err := tagNamesTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := indexMemo(ctx, tx, id, bodyText, tags); err != nil {
			return err
		}
		return enqueueTx(ctx, tx, id, domain.OutboxUpsert, revision, time.Now().UTC())
	})
	if err != nil {
		return nil, err
	}
	return s.GetMemo(ctx, id)
}

// DeleteMemo permanently removes the local row, its search entry and any
// queued sync operation. Attachment and tag rows cascade. The caller records a
// page tombstone first when a Notion page exists.
func (s *Store) DeleteMemo(ctx context.Context, id string) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		for _, stmt := range []string{
			`DELETE FROM outbox WHERE memo_id=?`,
			`DELETE FROM memos_fts WHERE memo_id=?`,
			`DELETE FROM memos WHERE id=?`,
		} {
			if _, err := tx.ExecContext(ctx, stmt, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeletedMemos returns all trashed memos with attachment metadata. It
// paginates because ListMemos caps a single page at 500 rows.
func (s *Store) DeletedMemos(ctx context.Context) ([]domain.Memo, error) {
	const batch = 500
	out := []domain.Memo{}
	for offset := 0; ; offset += batch {
		rows, err := s.ListMemos(ctx, MemoFilter{
			Deleted: DeletedOnly,
			Limit:   batch,
			Offset:  offset,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
		if len(rows) < batch {
			return out, nil
		}
	}
}

// SetKeepLocal pins or unpins a memo without bumping its revision.
func (s *Store) SetKeepLocal(ctx context.Context, id string, keep bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE memos SET keep_local=? WHERE id=?`, boolInt(keep), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// GetMemo loads one memo with its tags and attachment metadata.
func (s *Store) GetMemo(ctx context.Context, id string) (*domain.Memo, error) {
	memo, err := scanMemo(s.db.QueryRowContext(ctx, `SELECT `+memoColumns+` FROM memos WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	if err := s.hydrateRelations(ctx, memo); err != nil {
		return nil, err
	}
	return memo, nil
}

// GetMemoByPageID finds a memo by its Notion page id.
func (s *Store) GetMemoByPageID(ctx context.Context, pageID string) (*domain.Memo, error) {
	memo, err := scanMemo(s.db.QueryRowContext(ctx, `SELECT `+memoColumns+` FROM memos WHERE notion_page_id=?`, pageID))
	if err == sql.ErrNoRows {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.hydrateRelations(ctx, memo); err != nil {
		return nil, err
	}
	return memo, nil
}

func scanMemo(row *sql.Row) (*domain.Memo, error) {
	var m domain.Memo
	var created, updated, deleted, synced, hydrated sql.NullInt64
	var keepLocal int
	var state, blockStats string
	err := row.Scan(&m.ID, &created, &updated, &m.Revision, &m.Body, &m.BodyText, &m.Snippet,
		&deleted, &keepLocal, &state, &m.NotionPageID, &synced, &hydrated, &m.BaseRevision, &m.ContentHash, &m.SyncError, &blockStats)
	if err == sql.ErrNoRows {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.CreatedAt = fromMS(created.Int64)
	m.UpdatedAt = fromMS(updated.Int64)
	m.DeletedAt = msPtr(deleted)
	m.NotionSyncedAt = msPtr(synced)
	m.LastHydratedAt = msPtr(hydrated)
	m.KeepLocal = keepLocal == 1
	m.LocalState = domain.LocalState(state)
	m.BlockStats = domain.UnmarshalBlockStats(blockStats)
	m.Tags = []domain.Tag{}
	m.Attachments = []domain.Attachment{}
	return &m, nil
}

func (s *Store) hydrateRelations(ctx context.Context, m *domain.Memo) error {
	tags, err := s.tagRows(ctx, m.ID)
	if err != nil {
		return err
	}
	m.Tags = tags
	atts, err := s.attachmentsFor(ctx, m.ID)
	if err != nil {
		return err
	}
	m.Attachments = atts
	return nil
}

func (s *Store) tagRows(ctx context.Context, memoID string) ([]domain.Tag, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.name, t.normalized_name
		FROM tags t JOIN memo_tags mt ON mt.tag_id=t.id
		WHERE mt.memo_id=? ORDER BY t.normalized_name`, memoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []domain.Tag{}
	for rows.Next() {
		var t domain.Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.NormalizedName); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

func tagNamesTx(ctx context.Context, tx *sql.Tx, memoID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT t.name FROM tags t JOIN memo_tags mt ON mt.tag_id=t.id WHERE mt.memo_id=? ORDER BY t.normalized_name`, memoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// DeletedMode selects the soft-delete view.
type DeletedMode string

const (
	DeletedActive DeletedMode = "active"
	DeletedOnly   DeletedMode = "deleted"
	DeletedAll    DeletedMode = "all"
)

// MemoFilter drives the timeline and search-result queries.
type MemoFilter struct {
	Deleted        DeletedMode
	Tag            string // normalized
	LocalOnly      bool
	Synced         bool
	WithAttachment bool
	Untagged       bool
	From           *time.Time
	To             *time.Time
	Limit          int
	Offset         int
}

// ListMemos returns memos newest first.
func (s *Store) ListMemos(ctx context.Context, f MemoFilter) ([]domain.Memo, error) {
	where := []string{"1=1"}
	args := []any{}
	switch f.Deleted {
	case DeletedOnly:
		where = append(where, "m.deleted_at IS NOT NULL")
	case DeletedAll:
	default:
		where = append(where, "m.deleted_at IS NULL")
	}
	if f.Tag != "" {
		where = append(where, `EXISTS (SELECT 1 FROM memo_tags mt JOIN tags t ON t.id=mt.tag_id
			WHERE mt.memo_id=m.id AND t.normalized_name=?)`)
		args = append(args, domain.NormalizeTag(f.Tag))
	}
	if f.LocalOnly {
		where = append(where, "m.notion_page_id=''")
	}
	if f.Synced {
		where = append(where, "m.notion_page_id<>''")
	}
	if f.WithAttachment {
		where = append(where, "EXISTS (SELECT 1 FROM attachments a WHERE a.memo_id=m.id)")
	}
	if f.Untagged {
		where = append(where, "NOT EXISTS (SELECT 1 FROM memo_tags mt WHERE mt.memo_id=m.id)")
	}
	if f.From != nil {
		where = append(where, "m.created_at>=?")
		args = append(args, toMS(*f.From))
	}
	if f.To != nil {
		where = append(where, "m.created_at<?")
		args = append(args, toMS(*f.To))
	}
	limit := f.Limit
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	q := `SELECT ` + memoColumns + ` FROM memos m WHERE ` + strings.Join(where, " AND ") +
		` ORDER BY m.created_at DESC, m.id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, f.Offset)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	memos := []domain.Memo{}
	for rows.Next() {
		var m domain.Memo
		var created, updated, deleted, synced, hydrated sql.NullInt64
		var keepLocal int
		var state, blockStats string
		if err := rows.Scan(&m.ID, &created, &updated, &m.Revision, &m.Body, &m.BodyText, &m.Snippet,
			&deleted, &keepLocal, &state, &m.NotionPageID, &synced, &hydrated, &m.BaseRevision, &m.ContentHash, &m.SyncError, &blockStats); err != nil {
			return nil, err
		}
		m.CreatedAt = fromMS(created.Int64)
		m.UpdatedAt = fromMS(updated.Int64)
		m.DeletedAt = msPtr(deleted)
		m.NotionSyncedAt = msPtr(synced)
		m.LastHydratedAt = msPtr(hydrated)
		m.KeepLocal = keepLocal == 1
		m.LocalState = domain.LocalState(state)
		m.BlockStats = domain.UnmarshalBlockStats(blockStats)
		memos = append(memos, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range memos {
		if err := s.hydrateRelations(ctx, &memos[i]); err != nil {
			return nil, err
		}
	}
	return memos, nil
}

// Counts returns the sidebar totals over active memos.
func (s *Store) Counts(ctx context.Context) (notes, tags, days int, err error) {
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memos WHERE deleted_at IS NULL`).Scan(&notes); err != nil {
		return 0, 0, 0, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT t.id) FROM tags t JOIN memo_tags mt ON mt.tag_id=t.id
		JOIN memos m ON m.id=mt.memo_id WHERE m.deleted_at IS NULL`).Scan(&tags); err != nil {
		return 0, 0, 0, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT date(created_at/1000, 'unixepoch', 'localtime'))
		FROM memos WHERE deleted_at IS NULL`).Scan(&days); err != nil {
		return 0, 0, 0, err
	}
	return notes, tags, days, nil
}

// SetNotionPageID records an adopted or newly created page before the full
// push completes, so a retry does not create a duplicate page.
func (s *Store) SetNotionPageID(ctx context.Context, id, pageID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE memos SET notion_page_id=? WHERE id=?`, pageID, id)
	return err
}

// MarkSynced records a successful push.
func (s *Store) MarkSynced(ctx context.Context, id, pageID string, revision int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE memos SET notion_page_id=?, notion_synced_at=?, base_revision=?,
		sync_error='' WHERE id=?`, pageID, toMS(at), revision, id)
	return err
}

// MarkSyncedIfCurrent records a push without advancing base_revision past a
// local edit that landed while the request was in flight.
func (s *Store) MarkSyncedIfCurrent(ctx context.Context, id, pageID string, revision int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE memos SET notion_page_id=?, notion_synced_at=?,
		base_revision=CASE WHEN revision=? THEN ? ELSE base_revision END, sync_error='' WHERE id=?`,
		pageID, toMS(at), revision, revision, id)
	return err
}

// SetSyncError records a failed push without losing the outbox row.
func (s *Store) SetSyncError(ctx context.Context, id, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE memos SET sync_error=? WHERE id=?`, message, id)
	return err
}

// ClearSyncErrors clears stale sync errors once a device comes back online.
func (s *Store) ClearSyncErrors(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE memos SET sync_error='' WHERE sync_error<>''`)
	return err
}

// SetEvicted removes the body from the local row but keeps metadata and search.
func (s *Store) SetEvicted(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE memos SET body_md='', local_state=? WHERE id=?`, string(domain.LocalEvicted), id)
	return err
}

// SetPresent marks a memo's body present again after hydration.
func (s *Store) SetPresent(ctx context.Context, id string, hydratedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE memos SET local_state=?, last_hydrated_at=? WHERE id=?`,
		string(domain.LocalPresent), toMS(hydratedAt), id)
	return err
}

// RemoteMemo is a page pulled from Notion.
type RemoteMemo struct {
	ID          string
	PageID      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Revision    int64
	Body        string
	Tags        []string
	Deleted     bool
	SyncedAt    time.Time
	KeepEvicted bool
}

// ApplyRemote inserts or replaces local state from a Notion page. It never
// enqueues an outbox row: the remote copy is authoritative by definition here.
func (s *Store) ApplyRemote(ctx context.Context, r RemoteMemo) (*domain.Memo, error) {
	body := r.Body
	derived := deriveBody(body, r.Tags)
	var deletedAt any
	if r.Deleted {
		deletedAt = toMS(r.UpdatedAt)
	}
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		var exists int
		_ = tx.QueryRowContext(ctx, `SELECT 1 FROM memos WHERE id=?`, r.ID).Scan(&exists)
		state := string(domain.LocalPresent)
		if r.KeepEvicted || exists == 1 {
			var current string
			_ = tx.QueryRowContext(ctx, `SELECT local_state FROM memos WHERE id=?`, r.ID).Scan(&current)
			if current == string(domain.LocalEvicted) {
				state = current
				body = ""
			}
		}
		if exists == 1 {
			if _, err := tx.ExecContext(ctx, `UPDATE memos SET created_at=?, updated_at=?, revision=?, body_md=?, body_text=?,
				snippet=?, deleted_at=?, local_state=?, notion_page_id=?, notion_synced_at=?, base_revision=?,
				content_hash=?, block_stats=?, sync_error='' WHERE id=?`,
				toMS(r.CreatedAt), toMS(r.UpdatedAt), r.Revision, body, derived.Text, derived.Snippet,
				deletedAt, state, r.PageID, toMS(r.SyncedAt), r.Revision, derived.Hash, domain.MarshalBlockStats(derived.Stats), r.ID); err != nil {
				return err
			}
		} else {
			if _, err := tx.ExecContext(ctx, `INSERT INTO memos
				(id, created_at, updated_at, revision, body_md, body_text, snippet, deleted_at, local_state,
				 notion_page_id, notion_synced_at, base_revision, content_hash, block_stats)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				r.ID, toMS(r.CreatedAt), toMS(r.UpdatedAt), r.Revision, body, derived.Text, derived.Snippet,
				deletedAt, state, r.PageID, toMS(r.SyncedAt), r.Revision, derived.Hash, domain.MarshalBlockStats(derived.Stats)); err != nil {
				return err
			}
		}
		if err := setMemoTags(ctx, tx, r.ID, r.Tags); err != nil {
			return err
		}
		if r.Deleted {
			if _, err := tx.ExecContext(ctx, `DELETE FROM memos_fts WHERE memo_id=?`, r.ID); err != nil {
				return err
			}
		} else if err := indexMemo(ctx, tx, r.ID, derived.Text, r.Tags); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM outbox WHERE memo_id=?`, r.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetMemo(ctx, r.ID)
}

func (s *Store) attachmentsFor(ctx context.Context, memoID string) ([]domain.Attachment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, memo_id, kind, original_name, local_path, thumb_path, mime,
		bytes, width, height, sha256, notion_file_id, notion_block_id, uploaded_at, evicted_at
		FROM attachments WHERE memo_id=? ORDER BY original_name, id`, memoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Attachment{}
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

type attachmentScanner interface {
	Scan(dest ...any) error
}

func scanAttachment(sc attachmentScanner) (*domain.Attachment, error) {
	var a domain.Attachment
	var uploaded, evicted sql.NullInt64
	var kind string
	if err := sc.Scan(&a.ID, &a.MemoID, &kind, &a.OriginalName, &a.LocalPath, &a.ThumbPath, &a.MIME,
		&a.Bytes, &a.Width, &a.Height, &a.SHA256, &a.NotionFileID, &a.NotionBlockID, &uploaded, &evicted); err != nil {
		return nil, err
	}
	a.Kind = domain.AttachmentKind(kind)
	a.UploadedAt = msPtr(uploaded)
	a.EvictedAt = msPtr(evicted)
	a.Present = a.EvictedAt == nil && a.LocalPath != ""
	return &a, nil
}

func indexMemo(ctx context.Context, tx *sql.Tx, id, bodyText string, tags []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM memos_fts WHERE memo_id=?`, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO memos_fts (memo_id, body_text, tags_text) VALUES (?,?,?)`,
		id, bodyText, strings.Join(tags, " "))
	return err
}

func enqueueTx(ctx context.Context, tx *sql.Tx, memoID string, op domain.OutboxOp, revision int64, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO outbox (memo_id, op, revision, attempts, next_attempt_at, last_error, created_at)
		VALUES (?,?,?,0,?,'',?)
		ON CONFLICT(memo_id) DO UPDATE SET op=excluded.op, revision=excluded.revision, attempts=0,
		next_attempt_at=excluded.next_attempt_at, last_error='', created_at=excluded.created_at`,
		memoID, string(op), revision, toMS(now), toMS(now))
	return err
}
