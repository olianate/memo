package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"memo/internal/domain"
)

// setMemoTags replaces a memo's tag set, creating tags on demand and removing
// orphaned tags. It never rewrites the memo body or revision.
func setMemoTags(ctx context.Context, tx *sql.Tx, memoID string, names []string) error {
	clean := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		name = domain.CleanTagName(name)
		if name == "" {
			continue
		}
		norm := domain.NormalizeTag(name)
		if seen[norm] {
			continue
		}
		seen[norm] = true
		clean = append(clean, name)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memo_tags WHERE memo_id=?`, memoID); err != nil {
		return err
	}
	for _, name := range clean {
		norm := domain.NormalizeTag(name)
		var tagID string
		err := tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE normalized_name=?`, norm).Scan(&tagID)
		if err == sql.ErrNoRows {
			tagID = domain.NewID()
			if _, err := tx.ExecContext(ctx, `INSERT INTO tags (id, name, normalized_name) VALUES (?,?,?)`, tagID, name, norm); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO memo_tags (memo_id, tag_id) VALUES (?,?)`, memoID, tagID); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM tags WHERE id NOT IN (SELECT tag_id FROM memo_tags)`)
	return err
}

func attachToMemo(ctx context.Context, tx *sql.Tx, memoID string, attachmentIDs []string) error {
	for _, id := range attachmentIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE attachments SET memo_id=? WHERE id=?`, memoID, id); err != nil {
			return err
		}
	}
	return nil
}

// TagCount is a sidebar tag row.
type TagCount struct {
	domain.Tag
	Count int `json:"count"`
}

// ListTags returns active-memo tag counts, most used first.
func (s *Store) ListTags(ctx context.Context) ([]TagCount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.name, t.normalized_name, COUNT(mt.memo_id)
		FROM tags t
		JOIN memo_tags mt ON mt.tag_id=t.id
		JOIN memos m ON m.id=mt.memo_id AND m.deleted_at IS NULL
		GROUP BY t.id ORDER BY COUNT(mt.memo_id) DESC, t.name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TagCount{}
	for rows.Next() {
		var t TagCount
		if err := rows.Scan(&t.ID, &t.Name, &t.NormalizedName, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SuggestTags returns tags whose name contains the query, most used first.
func (s *Store) SuggestTags(ctx context.Context, query string, limit int) ([]TagCount, error) {
	if limit <= 0 {
		limit = 8
	}
	all, err := s.ListTags(ctx)
	if err != nil {
		return nil, err
	}
	q := domain.NormalizeTag(query)
	if q == "" {
		if len(all) > limit {
			return all[:limit], nil
		}
		return all, nil
	}
	out := []TagCount{}
	for _, t := range all {
		if strings.Contains(t.NormalizedName, q) {
			out = append(out, t)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

// RenameTag changes a tag's display name without touching memo bodies or
// revisions. The affected memos are re-indexed and queued so the Notion
// multi_select property follows the rename.
func (s *Store) RenameTag(ctx context.Context, id, newName string) (*domain.Tag, error) {
	clean := domain.CleanTagName(newName)
	if clean == "" {
		return nil, fmt.Errorf("tag name is required")
	}
	norm := domain.NormalizeTag(clean)
	var tag domain.Tag
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT id, name, normalized_name FROM tags WHERE id=?`, id).
			Scan(&tag.ID, &tag.Name, &tag.NormalizedName); err != nil {
			if err == sql.ErrNoRows {
				return domain.ErrNotFound
			}
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tags SET name=?, normalized_name=? WHERE id=?`, clean, norm, id); err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("a tag named %q already exists", clean)
			}
			return err
		}
		tag.Name = clean
		tag.NormalizedName = norm
		return refreshTagMemos(ctx, tx, id)
	})
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

// MergeTags moves every memo from source into target and deletes source.
func (s *Store) MergeTags(ctx context.Context, sourceID, targetID string) error {
	if sourceID == targetID {
		return nil
	}
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tags WHERE id IN (?,?)`, sourceID, targetID).Scan(&n); err != nil {
			return err
		}
		if n != 2 {
			return domain.ErrNotFound
		}
		affected, err := memoIDsForTags(ctx, tx, sourceID, targetID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO memo_tags (memo_id, tag_id)
			SELECT memo_id, ? FROM memo_tags WHERE tag_id=?`, targetID, sourceID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM memo_tags WHERE tag_id=?`, sourceID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM tags WHERE id=?`, sourceID); err != nil {
			return err
		}
		for _, memoID := range affected {
			if err := refreshMemoMetadata(ctx, tx, memoID); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteTag removes a tag from every memo without touching bodies.
func (s *Store) DeleteTag(ctx context.Context, id string) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		affected, err := memoIDsForTags(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM memo_tags WHERE tag_id=?`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM tags WHERE id=?`, id); err != nil {
			return err
		}
		for _, memoID := range affected {
			if err := refreshMemoMetadata(ctx, tx, memoID); err != nil {
				return err
			}
		}
		return nil
	})
}

func memoIDsForTags(ctx context.Context, tx *sql.Tx, tagIDs ...string) ([]string, error) {
	if len(tagIDs) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(tagIDs)), ",")
	args := make([]any, len(tagIDs))
	for i, id := range tagIDs {
		args[i] = id
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT memo_id FROM memo_tags WHERE tag_id IN (`+placeholders+`)`, args...)
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

func refreshTagMemos(ctx context.Context, tx *sql.Tx, tagID string) error {
	ids, err := memoIDsForTags(ctx, tx, tagID)
	if err != nil {
		return err
	}
	for _, memoID := range ids {
		if err := refreshMemoMetadata(ctx, tx, memoID); err != nil {
			return err
		}
	}
	return nil
}

// refreshMemoMetadata re-indexes a memo after a tag-only change and queues a
// property push. The revision is intentionally unchanged.
func refreshMemoMetadata(ctx context.Context, tx *sql.Tx, memoID string) error {
	var revision int64
	var bodyText string
	var deleted sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT revision, body_text, deleted_at FROM memos WHERE id=?`, memoID).
		Scan(&revision, &bodyText, &deleted); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if deleted.Valid {
		return nil
	}
	tags, err := tagNamesTx(ctx, tx, memoID)
	if err != nil {
		return err
	}
	if err := indexMemo(ctx, tx, memoID, bodyText, tags); err != nil {
		return err
	}
	return enqueueTx(ctx, tx, memoID, domain.OutboxUpsert, revision, time.Now().UTC())
}

// RebuildIndex re-creates every FTS row from local metadata and present bodies.
func (s *Store) RebuildIndex(ctx context.Context) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM memos_fts`); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id, body_text, deleted_at FROM memos`)
		if err != nil {
			return err
		}
		type row struct {
			id       string
			bodyText string
			deleted  bool
		}
		var items []row
		for rows.Next() {
			var r row
			var deleted sql.NullInt64
			if err := rows.Scan(&r.id, &r.bodyText, &deleted); err != nil {
				rows.Close()
				return err
			}
			r.deleted = deleted.Valid
			items = append(items, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		sort.Slice(items, func(i, j int) bool { return items[i].id < items[j].id })
		for _, item := range items {
			if item.deleted {
				continue
			}
			tags, err := tagNamesTx(ctx, tx, item.id)
			if err != nil {
				return err
			}
			if err := indexMemo(ctx, tx, item.id, item.bodyText, tags); err != nil {
				return err
			}
		}
		return nil
	})
}
