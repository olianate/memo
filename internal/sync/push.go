package sync

import (
	"context"
	"errors"
	"fmt"
	"os"

	"memo/internal/config"
	"memo/internal/domain"
	"memo/internal/notion"
)

// PushOnce drains up to 20 due outbox rows. Pushing is single-concurrency to
// respect Notion's request budget.
func (s *Service) PushOnce(ctx context.Context) (int, error) {
	if !s.canSync(ctx) {
		return 0, nil
	}
	g, ds, err := s.gatewayFor(ctx)
	if err != nil {
		return 0, err
	}
	items, err := s.store.DueOutbox(ctx, s.clock(), 20)
	if err != nil {
		return 0, err
	}
	flushErr := s.flushPurgedPages(ctx, g)
	if len(items) == 0 {
		if flushErr != nil {
			s.recordError(flushErr)
			return 0, flushErr
		}
		return 0, nil
	}
	s.setBusy(true)
	defer s.setBusy(false)

	pushed := 0
	lastErr := flushErr
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return pushed, err
		}
		memo, err := s.store.GetMemo(ctx, item.MemoID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				_ = s.store.ClearOutbox(ctx, item.MemoID)
				continue
			}
			lastErr = err
			continue
		}
		if err := s.pushOne(ctx, g, ds, memo, item); err != nil {
			lastErr = err
			s.failPush(ctx, item, err)
			continue
		}
		pushed++
	}
	if lastErr != nil {
		s.recordError(lastErr)
		return pushed, lastErr
	}
	s.recordSuccess()
	return pushed, nil
}

// flushPurgedPages writes tombstones for memos that were permanently deleted
// from local trash. Failures are retried on later pushes.
func (s *Service) flushPurgedPages(ctx context.Context, g notion.Gateway) error {
	ids, err := s.store.PurgedPageIDs(ctx)
	if err != nil || len(ids) == 0 {
		return err
	}
	device := s.device(ctx)
	titleProp := s.cfg.GetDefault(ctx, config.KeyNotionTitleProp, notion.TitlePropertyName)
	now := s.clock()
	var firstErr error
	for _, pageID := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		memo := domain.Memo{
			ID: pageID, NotionPageID: pageID,
			CreatedAt: now, UpdatedAt: now, Revision: 1, DeletedAt: &now,
		}
		err := g.UpdatePage(ctx, pageID, notion.PropertiesForMemo(memo, device, titleProp))
		if err != nil {
			if isAPIError(err, func(e *notion.APIError) bool { return e.Status == 404 }) {
				_ = s.store.ClearPurgedPage(ctx, pageID)
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		_ = s.store.ClearPurgedPage(ctx, pageID)
		_ = s.store.Log(ctx, "info", "purge", "tombstone written for purged page "+pageID)
	}
	return firstErr
}

func (s *Service) pushOne(ctx context.Context, g notion.Gateway, ds string, memo *domain.Memo, item domain.OutboxItem) error {
	device := s.device(ctx)
	titleProp := s.cfg.GetDefault(ctx, config.KeyNotionTitleProp, notion.TitlePropertyName)
	props := notion.PropertiesForMemo(*memo, device, titleProp)

	if item.Op == domain.OutboxDelete || memo.DeletedAt != nil {
		if memo.NotionPageID == "" {
			return s.store.ClearOutbox(ctx, memo.ID)
		}
		if err := g.UpdatePage(ctx, memo.NotionPageID, props); err != nil {
			if isAPIError(err, func(e *notion.APIError) bool { return e.Status == 404 }) {
				_ = s.store.SetNotionPageID(ctx, memo.ID, "")
				_ = s.store.ClearOutbox(ctx, memo.ID)
				return nil
			}
			return err
		}
		if err := s.store.MarkSyncedIfCurrent(ctx, memo.ID, memo.NotionPageID, memo.Revision, s.clock()); err != nil {
			return err
		}
		if err := s.store.ClearOutboxIfUnchanged(ctx, memo.ID, item.Revision, item.CreatedAt); err != nil {
			return err
		}
		_ = s.store.Log(ctx, "info", "push", "tombstone written for memo "+memo.ID)
		s.emit("memos:changed", map[string]string{"id": memo.ID})
		return nil
	}

	if memo.LocalState == domain.LocalEvicted {
		return errors.New("memo body is evicted; hydrate before editing")
	}
	if err := s.ensureTagOptions(ctx, g, ds, memo.Tags); err != nil {
		return err
	}
	blocks, err := memo.Blocks()
	if err != nil {
		return err
	}
	uploads, err := s.uploadAttachments(ctx, g, memo)
	if err != nil {
		return err
	}
	mapped, err := notion.BlocksToNotion(blocks, uploads)
	if err != nil {
		return err
	}

	pageID := memo.NotionPageID
	created := false
	if pageID == "" {
		found, err := s.findPageByMemoID(ctx, g, ds, memo.ID)
		if err != nil {
			return err
		}
		if found != "" {
			pageID = found
			if err := s.store.SetNotionPageID(ctx, memo.ID, pageID); err != nil {
				return err
			}
		}
	}

	if pageID == "" {
		first := mapped
		if len(first) > 100 {
			first = first[:100]
		}
		page, err := g.CreatePage(ctx, ds, props, first)
		if err != nil {
			if isAPIError(err, func(e *notion.APIError) bool { return e.CommitLanded() }) {
				var apiErr *notion.APIError
				_ = errors.As(err, &apiErr)
				_ = s.store.SetNotionPageID(ctx, memo.ID, apiErr.CommittedResourceID)
			}
			return err
		}
		pageID = page.ID
		created = true
		if err := s.store.SetNotionPageID(ctx, memo.ID, pageID); err != nil {
			return err
		}
		if len(mapped) > 100 {
			if err := s.appendRemaining(ctx, g, pageID, mapped[100:]); err != nil {
				return err
			}
		}
	} else {
		if err := g.UpdatePage(ctx, pageID, props); err != nil {
			if isAPIError(err, func(e *notion.APIError) bool { return e.Status == 404 }) {
				_ = s.store.SetNotionPageID(ctx, memo.ID, "")
			}
			return err
		}
		// The body is replaced only when the local revision moved past what was
		// last pushed. Adoption sets BaseRevision=0, so the first update replaces.
		if created || memo.Revision != memo.BaseRevision {
			if err := s.replaceChildren(ctx, g, pageID, mapped); err != nil {
				return err
			}
		}
	}

	if len(memo.Attachments) > 0 {
		if err := s.mapAttachmentBlocks(ctx, g, pageID, memo); err != nil {
			return err
		}
	}
	if err := s.store.MarkSyncedIfCurrent(ctx, memo.ID, pageID, memo.Revision, s.clock()); err != nil {
		return err
	}
	if err := s.store.ClearOutboxIfUnchanged(ctx, memo.ID, item.Revision, item.CreatedAt); err != nil {
		return err
	}
	_ = s.store.Log(ctx, "info", "push", fmt.Sprintf("memo %s synced at revision %d", memo.ID, memo.Revision))
	s.emit("memos:changed", map[string]string{"id": memo.ID})
	return nil
}

func (s *Service) failPush(ctx context.Context, item domain.OutboxItem, err error) {
	attempts := item.Attempts + 1
	delay := backoffDelay(attempts, s.rand)
	var apiErr *notion.APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > delay {
		delay = apiErr.RetryAfter
	}
	message := err.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	_ = s.store.FailOutbox(ctx, item.MemoID, attempts, s.clock().Add(delay), message)
	_ = s.store.SetSyncError(ctx, item.MemoID, message)
	_ = s.store.Log(ctx, "error", "push", "memo "+item.MemoID+": "+message)
}

// ensureTagOptions creates missing multi-select options before a page write.
// Notion does not guarantee that a value creates its option implicitly.
func (s *Service) ensureTagOptions(ctx context.Context, g notion.Gateway, ds string, tags []domain.Tag) error {
	if len(tags) == 0 {
		return nil
	}
	source, err := g.RetrieveDataSource(ctx, ds)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, option := range source.Options[notion.PropTags] {
		have[option.Name] = true
	}
	options := append([]notion.Option{}, source.Options[notion.PropTags]...)
	for _, tag := range tags {
		if !have[tag.Name] {
			options = append(options, notion.Option{Name: tag.Name})
			have[tag.Name] = true
		}
	}
	if len(options) == len(source.Options[notion.PropTags]) {
		return nil
	}
	return g.UpdateDataSourceSchema(ctx, ds, map[string]notion.PropertySchema{
		notion.PropTags: {MultiSelect: &notion.MultiSelectSchema{Options: options}},
	})
}

func (s *Service) findPageByMemoID(ctx context.Context, g notion.Gateway, ds, memoID string) (string, error) {
	resp, err := g.QueryDataSource(ctx, ds, notion.QueryRequest{
		Filter: map[string]any{
			"property":  notion.PropMemoID,
			"rich_text": map[string]any{"equals": memoID},
		},
		PageSize: 1,
	})
	if err != nil {
		return "", err
	}
	for _, p := range resp.Results {
		if notion.MemoIDFromPage(p) == memoID {
			return p.ID, nil
		}
	}
	return "", nil
}

func (s *Service) uploadAttachments(ctx context.Context, g notion.Gateway, memo *domain.Memo) (map[string]string, error) {
	uploads := map[string]string{}
	limit := int64(s.cfg.Int(ctx, config.KeyWorkspaceFileLimit))
	for i := range memo.Attachments {
		a := &memo.Attachments[i]
		if a.NotionFileID != "" {
			uploads[a.ID] = a.NotionFileID
			continue
		}
		if a.LocalPath == "" {
			return nil, fmt.Errorf("attachment %q has no local bytes", a.OriginalName)
		}
		data, err := os.ReadFile(a.LocalPath)
		if err != nil {
			return nil, fmt.Errorf("read attachment %q: %w", a.OriginalName, err)
		}
		if limit > 0 && int64(len(data)) > limit {
			return nil, fmt.Errorf("attachment %q is over the workspace file limit (%d bytes)", a.OriginalName, limit)
		}
		fileID, err := g.UploadFile(ctx, a.OriginalName, a.MIME, data)
		if err != nil {
			return nil, err
		}
		if err := s.store.MarkAttachmentUploaded(ctx, a.ID, fileID, "", s.clock()); err != nil {
			return nil, err
		}
		uploads[a.ID] = fileID
	}
	return uploads, nil
}

func (s *Service) appendRemaining(ctx context.Context, g notion.Gateway, pageID string, blocks []notion.Block) error {
	for len(blocks) > 0 {
		chunk := blocks
		if len(chunk) > 100 {
			chunk = chunk[:100]
		}
		if _, err := g.AppendChildren(ctx, pageID, chunk); err != nil {
			return err
		}
		blocks = blocks[len(chunk):]
	}
	return nil
}

func (s *Service) replaceChildren(ctx context.Context, g notion.Gateway, pageID string, blocks []notion.Block) error {
	existing := []notion.Block{}
	cursor := ""
	for {
		resp, err := g.ListChildren(ctx, pageID, cursor, 100)
		if err != nil {
			return err
		}
		existing = append(existing, resp.Results...)
		if !resp.HasMore {
			break
		}
		cursor = resp.NextCursor
	}
	for _, b := range existing {
		if err := g.DeleteBlock(ctx, b.ID); err != nil {
			if isAPIError(err, func(e *notion.APIError) bool { return e.Status == 404 }) {
				continue
			}
			return err
		}
	}
	return s.appendRemaining(ctx, g, pageID, blocks)
}

// mapAttachmentBlocks pairs image/file children with attachment metadata so a
// later hydration can find the block again.
func (s *Service) mapAttachmentBlocks(ctx context.Context, g notion.Gateway, pageID string, memo *domain.Memo) error {
	blocks, err := memo.Blocks()
	if err != nil {
		return err
	}
	attachmentIDs := []string{}
	for _, b := range blocks {
		if (b.Type == domain.BlockImage || b.Type == domain.BlockFile) && b.AttachmentID != "" {
			attachmentIDs = append(attachmentIDs, b.AttachmentID)
		}
	}
	if len(attachmentIDs) == 0 {
		return nil
	}
	children := []notion.Block{}
	cursor := ""
	for {
		resp, err := g.ListChildren(ctx, pageID, cursor, 100)
		if err != nil {
			return err
		}
		children = append(children, resp.Results...)
		if !resp.HasMore {
			break
		}
		cursor = resp.NextCursor
	}
	idx := 0
	for _, child := range children {
		if child.Type != "image" && child.Type != "file" {
			continue
		}
		if idx >= len(attachmentIDs) {
			break
		}
		_ = s.store.SetAttachmentNotionBlock(ctx, attachmentIDs[idx], child.ID)
		idx++
	}
	return nil
}

// TrashSweep detects manual Notion trashes. It is best-effort because search
// indexing lags; Status=deleted remains the primary deletion feed.
func (s *Service) TrashSweep(ctx context.Context) error {
	if !s.canSync(ctx) {
		return nil
	}
	g, _, err := s.gatewayFor(ctx)
	if err != nil {
		return err
	}
	pages, err := g.SearchTrash(ctx)
	if err != nil {
		return err
	}
	now := s.clock()
	for _, p := range pages {
		id := notion.MemoIDFromPage(p)
		if id == "" {
			id = p.ID
		}
		memo, err := s.store.GetMemo(ctx, id)
		if err != nil {
			continue
		}
		if memo.DeletedAt != nil {
			continue
		}
		rev := notion.RevisionFromPage(p)
		if err := s.store.ApplyRemoteDelete(ctx, id, now, rev); err != nil {
			continue
		}
		_ = s.store.Log(ctx, "info", "trash", "manual Notion trash detected for memo "+id)
		s.emit("memos:changed", map[string]string{"id": id})
	}
	return s.store.SetLastTrashSweep(ctx, now)
}
