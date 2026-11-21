package store

import (
	"context"
	"database/sql"
	"time"

	"memo/internal/domain"
)

// DueOutbox returns the next pending rows that may be attempted now.
func (s *Store) DueOutbox(ctx context.Context, now time.Time, limit int) ([]domain.OutboxItem, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT memo_id, op, revision, attempts, next_attempt_at, last_error, created_at
		FROM outbox WHERE next_attempt_at<=? ORDER BY next_attempt_at, created_at LIMIT ?`, toMS(now), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.OutboxItem
	for rows.Next() {
		item, err := scanOutbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

// OutboxForMemo returns the pending row for one memo, if any.
func (s *Store) OutboxForMemo(ctx context.Context, memoID string) (*domain.OutboxItem, error) {
	row := s.db.QueryRowContext(ctx, `SELECT memo_id, op, revision, attempts, next_attempt_at, last_error, created_at
		FROM outbox WHERE memo_id=?`, memoID)
	item, err := scanOutbox(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return item, err
}

type outboxScanner interface{ Scan(dest ...any) error }

func scanOutbox(sc outboxScanner) (*domain.OutboxItem, error) {
	var item domain.OutboxItem
	var op string
	var next, created int64
	if err := sc.Scan(&item.MemoID, &op, &item.Revision, &item.Attempts, &next, &item.LastError, &created); err != nil {
		return nil, err
	}
	item.Op = domain.OutboxOp(op)
	item.NextAttemptAt = fromMS(next)
	item.CreatedAt = fromMS(created)
	return &item, nil
}

// OutboxCounts returns pending and failed-looking counts for the dashboard.
func (s *Store) OutboxCounts(ctx context.Context) (pending, retrying int, err error) {
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox`).Scan(&pending); err != nil {
		return 0, 0, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE attempts>0`).Scan(&retrying); err != nil {
		return 0, 0, err
	}
	return pending, retrying, nil
}

// ClearOutbox drops a memo's pending row after a successful push.
func (s *Store) ClearOutbox(ctx context.Context, memoID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM outbox WHERE memo_id=?`, memoID)
	return err
}

// ClearOutboxIfUnchanged removes the attempted row only when no newer change
// replaced it during the network round trip.
func (s *Store) ClearOutboxIfUnchanged(ctx context.Context, memoID string, revision int64, createdAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM outbox WHERE memo_id=? AND revision<=? AND created_at<=?`,
		memoID, revision, toMS(createdAt))
	return err
}

// FailOutbox records a retry with backoff and the last error.
func (s *Store) FailOutbox(ctx context.Context, memoID string, attempts int, next time.Time, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE outbox SET attempts=?, next_attempt_at=?, last_error=? WHERE memo_id=?`,
		attempts, toMS(next), message, memoID)
	return err
}

// EnqueueAllLocalOnly queues every memo that has no Notion page. Used when sync
// is turned on for the first time.
func (s *Store) EnqueueAllLocalOnly(ctx context.Context) error {
	now := time.Now().UTC()
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id, revision FROM memos WHERE notion_page_id='' AND deleted_at IS NULL`)
		if err != nil {
			return err
		}
		type pair struct {
			id       string
			revision int64
		}
		var items []pair
		for rows.Next() {
			var p pair
			if err := rows.Scan(&p.id, &p.revision); err != nil {
				rows.Close()
				return err
			}
			items = append(items, p)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, item := range items {
			if err := enqueueTx(ctx, tx, item.id, domain.OutboxUpsert, item.revision, now); err != nil {
				return err
			}
		}
		return nil
	})
}
