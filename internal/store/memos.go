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
	// Attachments are rows whose bytes are already on disk. They are inserted in
	// the same transaction as the memo so a new memo with files is one revision.
	Attachments []domain.Attachment
	KeepLocal   bool
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
		for _, att := range in.Attachments {
			att.MemoID = in.ID
			if att.ID == "" {
				att.ID = domain.NewID()
			}
			if err := insertAttachment(ctx, tx, att); err != nil {
				return err
			}
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
	return s.UpdateMemoDerived(ctx, id, body, tags, keepLocal, deriveBody(body, tags))
}

// UpdateMemoDerived is UpdateMemo for callers that already parsed the body and
// computed its projection, so an edit that validated the body does not parse it
// twice.
func (s *Store) UpdateMemoDerived(ctx context.Context, id string, body string, tags []string, keepLocal *bool, derived Derived) (*domain.Memo, error) {
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		_, err := writeMemoTx(ctx, tx, id, memoWrite{
			Body:         &body,
			Tags:         tags,
			KeepLocal:    keepLocal,
			Derived:      &derived,
			LocalState:   domain.LocalPresent,
			BumpRevision: true,
			Enqueue:      true,
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("store: update memo: %w", err)
	}
	return s.GetMemo(ctx, id)
}

// UpdateMemoMetadata edits tags and the keep-local flag while leaving the body
// untouched, so an evicted memo stays evicted and a metadata-only edit costs no
// re-derivation. It still bumps the revision: tags are synced properties.
func (s *Store) UpdateMemoMetadata(ctx context.Context, id string, tags []string, keepLocal bool) (*domain.Memo, error) {
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		_, err := writeMemoTx(ctx, tx, id, memoWrite{
			Tags:         tags,
			KeepLocal:    &keepLocal,
			BumpRevision: true,
			Enqueue:      true,
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("store: update memo metadata: %w", err)
	}
	return s.GetMemo(ctx, id)
}

// memoWrite is one local memo update. A nil Body leaves the body untouched;
// otherwise Derived may carry an already-parsed projection. LocalState is empty
// to preserve the current state or set explicitly by a body write.
type memoWrite struct {
	Body         *string
	Tags         []string
	KeepLocal    *bool
	Derived      *Derived
	LocalState   domain.LocalState
	BumpRevision bool
	Enqueue      bool
}

// writeMemoTx owns every mutable memo column and the derived search row in one
// transaction, so update paths cannot drift on revision, hash, FTS or outbox.
func writeMemoTx(ctx context.Context, tx *sql.Tx, id string, w memoWrite) (int64, error) {
	var revision int64
	var bodyMD, bodyText string
	if err := tx.QueryRowContext(ctx,
		`SELECT revision, body_md, body_text FROM memos WHERE id=?`, id).
		Scan(&revision, &bodyMD, &bodyText); err != nil {
		if err == sql.ErrNoRows {
			return 0, domain.ErrNotFound
		}
		return 0, err
	}
	if w.BumpRevision {
		revision++
	}
	now := time.Now().UTC()
	if w.Body != nil {
		bodyMD = *w.Body
		derived := w.Derived
		if derived == nil {
			d := deriveBody(*w.Body, w.Tags)
			derived = &d
		}
		bodyText = derived.Text
		state := w.LocalState
		if state == "" {
			state = domain.LocalPresent
		}
		sets := []string{"body_md=?", "body_text=?", "snippet=?", "updated_at=?", "revision=?",
			"content_hash=?", "block_stats=?", "local_state=?", "sync_error=''"}
		args := []any{bodyMD, bodyText, derived.Snippet, toMS(now), revision, derived.Hash,
			domain.MarshalBlockStats(derived.Stats), string(state)}
		if w.KeepLocal != nil {
			sets = append(sets, "keep_local=?")
			args = append(args, boolInt(*w.KeepLocal))
		}
		args = append(args, id)
		if _, err := tx.ExecContext(ctx,
			`UPDATE memos SET `+strings.Join(sets, ", ")+` WHERE id=?`, args...); err != nil {
			return 0, err
		}
	} else {
		sets := []string{"updated_at=?", "revision=?", "sync_error=''"}
		args := []any{toMS(now), revision}
		if w.LocalState != "" {
			sets = append(sets, "local_state=?")
			args = append(args, string(w.LocalState))
		}
		if w.KeepLocal != nil {
			sets = append(sets, "keep_local=?")
			args = append(args, boolInt(*w.KeepLocal))
		}
		// A metadata edit still changes the synced content hash. An evicted body
		// has no local markdown, so its stored hash stays intact.
		if w.Tags != nil && bodyMD != "" {
			sets = append(sets, "content_hash=?")
			args = append(args, domain.HashContent(bodyMD, w.Tags))
		}
		args = append(args, id)
		if _, err := tx.ExecContext(ctx,
			`UPDATE memos SET `+strings.Join(sets, ", ")+` WHERE id=?`, args...); err != nil {
			return 0, err
		}
	}
	if w.Tags != nil {
		if err := setMemoTags(ctx, tx, id, w.Tags); err != nil {
			return 0, err
		}
	}
	if err := indexMemo(ctx, tx, id, bodyText, w.Tags); err != nil {
		return 0, err
	}
	if w.Enqueue {
		if err := enqueueTx(ctx, tx, id, domain.OutboxUpsert, revision, now); err != nil {
			return 0, err
		}
	}
	return revision, nil
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

// SetKeepLocal marks a memo to be excluded from retention eviction without
// bumping its revision.
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

// SetPinned moves a memo to the top of the timeline (or back) without bumping
// its revision. Pinning is a local-only preference and is never synced.
func (s *Store) SetPinned(ctx context.Context, id string, pinned bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE memos SET pinned=? WHERE id=?`, boolInt(pinned), id)
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

type memoScanner interface {
	Scan(dest ...any) error
}

func scanMemoRow(sc memoScanner) (*domain.Memo, error) {
	var m domain.Memo
	var created, updated, deleted, synced, hydrated sql.NullInt64
	var keepLocal, pinned int
	var state, blockStats string
	if err := sc.Scan(&m.ID, &created, &updated, &m.Revision, &m.Body, &m.BodyText, &m.Snippet,
		&deleted, &keepLocal, &pinned, &state, &m.NotionPageID, &synced, &hydrated, &m.BaseRevision, &m.ContentHash, &m.SyncError, &blockStats); err != nil {
		return nil, err
	}
	m.CreatedAt = fromMS(created.Int64)
	m.UpdatedAt = fromMS(updated.Int64)
	m.DeletedAt = msPtr(deleted)
	m.NotionSyncedAt = msPtr(synced)
	m.LastHydratedAt = msPtr(hydrated)
	m.KeepLocal = keepLocal == 1
	m.Pinned = pinned == 1
	m.LocalState = domain.LocalState(state)
	m.BlockStats = domain.UnmarshalBlockStats(blockStats)
	m.Tags = []domain.Tag{}
	m.Attachments = []domain.Attachment{}
	return &m, nil
}

func scanMemo(row *sql.Row) (*domain.Memo, error) {
	m, err := scanMemoRow(row)
	if err == sql.ErrNoRows {
		return nil, domain.ErrNotFound
	}
	return m, err
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
		` ORDER BY m.pinned DESC, m.created_at DESC, m.id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, f.Offset)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	memos := []domain.Memo{}
	for rows.Next() {
		m, err := scanMemoRow(rows)
		if err != nil {
			return nil, err
		}
		memos = append(memos, *m)
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

// EvictBody removes the body and every attachment's local paths in one
// transaction while keeping metadata, tags, derived text and the search row.
func (s *Store) EvictBody(ctx context.Context, id string, at time.Time) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE memos SET body_md='', local_state=? WHERE id=?`,
			string(domain.LocalEvicted), id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE attachments SET local_path='', thumb_path='', evicted_at=? WHERE memo_id=?`,
			toMS(at), id)
		return err
	})
}

// RemoteMemo is a page pulled from Notion.
type RemoteMemo struct {
	ID        string
	PageID    string
	CreatedAt time.Time
	UpdatedAt time.Time
	Revision  int64
	Body      string
	Tags      []string
	Deleted   bool
	SyncedAt  time.Time
}

// ApplyRemote inserts or replaces local state from a Notion page. It never
// enqueues an outbox row: the remote copy is authoritative by definition here.
// The caller supplies the resulting local state; evicted keeps the body empty
// while the remote text still feeds body_text and the search index.
func (s *Store) ApplyRemote(ctx context.Context, r RemoteMemo, localState domain.LocalState) (*domain.Memo, error) {
	body := r.Body
	if localState == "" {
		localState = domain.LocalPresent
	}
	if localState == domain.LocalEvicted {
		body = ""
	}
	state := string(localState)
	// Derived text always comes from the remote body, so an evicted memo stays
	// searchable after a pull.
	derived := deriveBody(r.Body, r.Tags)
	var deletedAt any
	if r.Deleted {
		deletedAt = toMS(r.UpdatedAt)
	}
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		var exists int
		_ = tx.QueryRowContext(ctx, `SELECT 1 FROM memos WHERE id=?`, r.ID).Scan(&exists)
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
