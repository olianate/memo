package sync

import (
	"context"
	"errors"
	"time"

	"github.com/olianate/memo/internal/config"
	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/domain/markdown"
	"github.com/olianate/memo/internal/presence"
	"github.com/olianate/memo/internal/store"
)

// RetentionPolicy is the eviction configuration.
type RetentionPolicy struct {
	Enabled   bool
	Days      int // 0 or negative means never, unless Immediate
	Immediate bool
}

// PolicyFromConfig reads the current retention settings.
func PolicyFromConfig(ctx context.Context, cfg *config.Store) RetentionPolicy {
	return RetentionPolicy{
		Enabled:   cfg.Bool(ctx, config.KeySyncEnabled),
		Days:      cfg.Int(ctx, config.KeyRetentionDays),
		Immediate: cfg.Bool(ctx, config.KeyRetentionImmediate),
	}
}

// EligibleForEviction is the pure retention predicate. A memo is eligible only
// when sync is on, the page and every attachment are synced, nothing is queued,
// and the anchor is older than the retention window. Local-only and pinned
// memos are always exempt.
func EligibleForEviction(memo domain.Memo, pendingOutbox bool, now time.Time, p RetentionPolicy) bool {
	if !p.Enabled || memo.DeletedAt != nil {
		return false
	}
	if presence.Evicted(memo) || memo.NotionPageID == "" || memo.NotionSyncedAt == nil {
		return false
	}
	if memo.KeepLocal || pendingOutbox {
		return false
	}
	for _, a := range memo.Attachments {
		if a.UploadedAt == nil {
			return false
		}
	}
	if p.Immediate {
		return true
	}
	if p.Days <= 0 {
		return false
	}
	anchor := *memo.NotionSyncedAt
	if memo.LastHydratedAt != nil && memo.LastHydratedAt.After(anchor) {
		anchor = *memo.LastHydratedAt
	}
	return !now.Before(anchor.Add(time.Duration(p.Days) * 24 * time.Hour))
}

// EvictOnce evicts every eligible memo. Metadata, tags, body_text and the FTS
// row are retained; body_json, attachment files and thumbnails are removed.
func (s *Service) EvictOnce(ctx context.Context) error {
	policy := PolicyFromConfig(ctx, s.cfg)
	if !policy.Enabled {
		return nil
	}
	memos, err := s.allActiveMemos(ctx)
	if err != nil {
		return err
	}
	now := s.clock()
	count := 0
	for i := range memos {
		memo := &memos[i]
		item, err := s.store.OutboxForMemo(ctx, memo.ID)
		if err != nil {
			continue
		}
		if !EligibleForEviction(*memo, item != nil, now, policy) {
			continue
		}
		if s.presence == nil {
			continue
		}
		if err := s.presence.Evict(ctx, memo.ID); err != nil {
			continue
		}
		count++
	}
	if count > 0 {
		s.emit("retention:evicted", map[string]int{"count": count})
	}
	return nil
}

// Hydrate restores one evicted memo's body and attachment bytes from Notion.
func (s *Service) Hydrate(ctx context.Context, memoID string) error {
	memo, err := s.store.GetMemo(ctx, memoID)
	if err != nil {
		return err
	}
	if !presence.Evicted(*memo) {
		return nil
	}
	if memo.NotionPageID == "" {
		return errors.New("this memo was never archived to Notion")
	}
	if !s.canSync(ctx) {
		return errors.New("Notion is offline or sync is disabled")
	}
	g, _, err := s.gatewayFor(ctx)
	if err != nil {
		return err
	}
	if _, err := g.GetPage(ctx, memo.NotionPageID); err != nil {
		return err
	}
	blocks, atts, err := s.fetchPage(ctx, g, memo.NotionPageID)
	if err != nil {
		return err
	}
	blocks, err = s.importAttachments(ctx, g, memoID, blocks, atts)
	if err != nil {
		return err
	}
	now := s.clock()
	if s.presence == nil {
		return errors.New("body presence is not configured")
	}
	return s.presence.Restore(ctx, memoID, markdown.Serialize(blocks), now)
}

// RestoreProgress is emitted during restore-all.
type RestoreProgress struct {
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Failed    int    `json:"failed"`
	CurrentID string `json:"current_id,omitempty"`
}

// RestoreAll downloads every missing body and attachment, emitting progress.
func (s *Service) RestoreAll(ctx context.Context) (RestoreProgress, error) {
	memos, err := s.allActiveMemos(ctx)
	if err != nil {
		return RestoreProgress{}, err
	}
	targets := []domain.Memo{}
	for _, m := range memos {
		if presence.Evicted(m) {
			targets = append(targets, m)
		}
	}
	progress := RestoreProgress{Total: len(targets)}
	s.emit("restore:progress", progress)
	var lastErr error
	for _, m := range targets {
		progress.CurrentID = m.ID
		if err := s.Hydrate(ctx, m.ID); err != nil {
			progress.Failed++
			lastErr = err
		} else {
			progress.Completed++
		}
		s.emit("restore:progress", progress)
	}
	s.emit("restore:done", progress)
	return progress, lastErr
}

// allActiveMemos pages through the store so retention and restore are not
// capped at one list page.
func (s *Service) allActiveMemos(ctx context.Context) ([]domain.Memo, error) {
	const page = 2000
	var out []domain.Memo
	for offset := 0; ; offset += page {
		rows, err := s.store.ListMemos(ctx, store.MemoFilter{Limit: page, Offset: offset})
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
		if len(rows) < page {
			return out, nil
		}
	}
}
