package sync

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"time"

	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/domain/markdown"
	"github.com/olianate/memo/internal/notion"
	"github.com/olianate/memo/internal/store"
)

// PullOnce fetches changes since the stored cursor with a 10-minute overlap.
func (s *Service) PullOnce(ctx context.Context) error {
	if !s.canSync(ctx) {
		return nil
	}
	state, err := s.store.DeviceState(ctx)
	if err != nil {
		return err
	}
	if state.LastPullAt == nil {
		return s.FullScan(ctx)
	}
	g, ds, err := s.gatewayFor(ctx)
	if err != nil {
		return err
	}
	s.setBusy(true)
	defer s.setBusy(false)

	cursorTime := *state.LastPullAt
	from := cursorTime.Add(-10 * time.Minute)
	newest := cursorTime
	cursor := ""
	seen := map[string]bool{}
	var lastErr error

	for page := 0; page < 100; page++ {
		resp, err := g.QueryDataSource(ctx, ds, notion.QueryRequest{
			Filter: map[string]any{
				"property": notion.PropUpdated,
				"date":     map[string]any{"on_or_after": from.UTC().Format(time.RFC3339)},
			},
			Sorts:       []notion.Sort{{Property: notion.PropUpdated, Direction: "ascending"}},
			PageSize:    100,
			StartCursor: cursor,
		})
		if err != nil {
			return err
		}
		for _, p := range resp.Results {
			if seen[p.ID] {
				continue
			}
			seen[p.ID] = true
			if err := s.applyPage(ctx, g, p); err != nil {
				lastErr = err
				continue
			}
			if u := notion.UpdatedAtFromPage(p); u.After(newest) {
				newest = u
			}
		}
		if resp.Incomplete() {
			_ = s.store.Log(ctx, "warn", "pull", "query result cap reached; switching to windowed full scan")
			if err := s.fullScanFrom(ctx, g, ds, from); err != nil {
				return err
			}
			break
		}
		if !resp.HasMore || resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}
	if lastErr != nil {
		// Do not advance the cursor past a page we failed to apply.
		return lastErr
	}
	next := newest
	if !next.After(cursorTime) {
		// Even an idle poll records the poll time, otherwise the worker would
		// pull every tick instead of every 60 s / 5 min.
		next = s.clock()
	}
	if err := s.store.SetLastPull(ctx, next); err != nil {
		return err
	}
	return nil
}

// FullScan windows by created_time to defeat the 10,000-row query cap.
func (s *Service) FullScan(ctx context.Context) error {
	if !s.canSync(ctx) {
		return nil
	}
	g, ds, err := s.gatewayFor(ctx)
	if err != nil {
		return err
	}
	state, _ := s.store.DeviceState(ctx)
	from := time.Unix(0, 0)
	if state.LastFullScanAt != nil {
		from = state.LastFullScanAt.Add(-10 * time.Minute)
	}
	if err := s.fullScanFrom(ctx, g, ds, from); err != nil {
		return err
	}
	now := s.clock()
	if err := s.store.SetLastFullScan(ctx, now); err != nil {
		return err
	}
	return s.store.SetLastPull(ctx, now)
}

func (s *Service) fullScanFrom(ctx context.Context, g notion.Gateway, ds string, from time.Time) error {
	s.setBusy(true)
	defer s.setBusy(false)

	windowStart := from
	for window := 0; window < 500; window++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		cursor := ""
		maxCreated := windowStart
		seen := map[string]bool{}
		incomplete := false
		for {
			resp, err := g.QueryDataSource(ctx, ds, notion.QueryRequest{
				Filter: map[string]any{
					"timestamp":    "created_time",
					"created_time": map[string]any{"on_or_after": windowStart.UTC().Format(time.RFC3339)},
				},
				Sorts:       []notion.Sort{{Timestamp: "created_time", Direction: "ascending"}},
				PageSize:    100,
				StartCursor: cursor,
			})
			if err != nil {
				return err
			}
			for _, p := range resp.Results {
				if seen[p.ID] {
					continue
				}
				seen[p.ID] = true
				if err := s.applyPage(ctx, g, p); err != nil {
					return err
				}
				if created := notion.CreatedAtFromPage(p); created.After(maxCreated) {
					maxCreated = created
				}
			}
			if resp.Incomplete() {
				incomplete = true
				break
			}
			if !resp.HasMore || resp.NextCursor == "" {
				break
			}
			cursor = resp.NextCursor
		}
		if !incomplete {
			return nil
		}
		next := maxCreated
		if !next.After(windowStart) {
			next = windowStart.Add(time.Millisecond)
		}
		windowStart = next
	}
	return errors.New("full scan exceeded 500 windows")
}

// applyPage merges one remote page using the revision/updated/device comparator.
func (s *Service) applyPage(ctx context.Context, g notion.Gateway, p notion.Page) error {
	if purged, err := s.store.IsPagePurged(ctx, p.ID); err == nil && purged {
		return nil
	}
	id := notion.MemoIDFromPage(p)
	if id == "" {
		id = p.ID
	}
	if !domain.IsSafeID(id) {
		return fmt.Errorf("remote page %s has an unsafe Memo ID", p.ID)
	}
	local, err := s.store.GetMemo(ctx, id)
	deleted := notion.DeletedFromPage(p) || p.InTrash
	rev := notion.RevisionFromPage(p)
	updated := notion.UpdatedAtFromPage(p)
	created := notion.CreatedAtFromPage(p)
	wasEvicted := err == nil && local.LocalState == domain.LocalEvicted

	if errors.Is(err, domain.ErrNotFound) {
		if deleted {
			return nil
		}
		blocks, atts, err := s.fetchPage(ctx, g, p.ID)
		if err != nil {
			return err
		}
		if _, err := s.store.ApplyRemote(ctx, store.RemoteMemo{
			ID: id, PageID: p.ID, CreatedAt: created, UpdatedAt: updated, Revision: rev,
			Body: markdown.Serialize(blocks), Tags: notion.TagsFromPage(p), SyncedAt: s.clock(),
		}); err != nil {
			return err
		}
		// The memo row must exist before attachment metadata can reference it.
		if !wasEvicted {
			wired, err := s.importAttachments(ctx, g, id, blocks, atts)
			if err != nil {
				return err
			}
			if len(atts) > 0 {
				if err := s.store.UpdateBody(ctx, id, markdown.Serialize(wired)); err != nil {
					return err
				}
			}
		}
		_ = s.store.Log(ctx, "info", "pull", "imported memo "+id)
		s.emit("memos:changed", map[string]string{"id": id})
		return nil
	}
	if err != nil {
		return err
	}

	remoteWins := RemoteWins(local.Revision, local.UpdatedAt, s.deviceID, rev, updated, notion.DeviceFromPage(p))
	if !remoteWins {
		if local.Revision != rev || !local.UpdatedAt.Equal(updated) {
			_ = s.store.Log(ctx, "info", "conflict", "local wins for memo "+id)
		}
		return nil
	}
	if deleted {
		if err := s.store.ApplyRemoteDelete(ctx, id, updated, rev); err != nil {
			return err
		}
		_ = s.store.Log(ctx, "info", "pull", "tombstone applied for memo "+id)
		s.emit("memos:changed", map[string]string{"id": id})
		return nil
	}
	if local.Revision == rev && local.UpdatedAt.Equal(updated) {
		// A previous pull may have committed metadata before attachment wiring.
		// Repair only when an attachment block is still unlinked.
		needsRepair := false
		for _, a := range local.Attachments {
			if a.NotionBlockID == "" {
				needsRepair = true
				break
			}
		}
		if !needsRepair {
			return nil
		}
	}
	blocks, atts, err := s.fetchPage(ctx, g, p.ID)
	if err != nil {
		return err
	}
	if _, err := s.store.ApplyRemote(ctx, store.RemoteMemo{
		ID: id, PageID: p.ID, CreatedAt: created, UpdatedAt: updated, Revision: rev,
		Body: markdown.Serialize(blocks), Tags: notion.TagsFromPage(p), SyncedAt: s.clock(),
	}); err != nil {
		return err
	}
	if !wasEvicted {
		wired, err := s.importAttachments(ctx, g, id, blocks, atts)
		if err != nil {
			return err
		}
		if len(atts) > 0 {
			if err := s.store.UpdateBody(ctx, id, markdown.Serialize(wired)); err != nil {
				return err
			}
		}
	}
	_ = s.store.Log(ctx, "info", "pull", fmt.Sprintf("memo %s updated from Notion (revision %d)", id, rev))
	s.emit("memos:changed", map[string]string{"id": id})
	return nil
}

func (s *Service) fetchPage(ctx context.Context, g notion.Gateway, pageID string) ([]domain.Block, []notion.ImportedAttachment, error) {
	budget := 0
	children, err := s.listBlockChildren(ctx, g, pageID, 0, &budget)
	if err != nil {
		return nil, nil, err
	}
	blocks, atts := notion.BlocksFromNotion(children)
	return blocks, atts, nil
}

// listBlockChildren reads a page or block subtree. Notion returns only direct
// children, so blocks with children are expanded recursively; otherwise
// nested content (toggles, columns, synced blocks) would vanish on pull.
func (s *Service) listBlockChildren(ctx context.Context, g notion.Gateway, blockID string, depth int, budget *int) ([]notion.Block, error) {
	const maxDepth = 10
	const maxBlocks = 5000
	if depth > maxDepth || *budget >= maxBlocks {
		return nil, nil
	}
	var out []notion.Block
	cursor := ""
	for len(out) < 2000 && *budget < maxBlocks {
		resp, err := g.ListChildren(ctx, blockID, cursor, 100)
		if err != nil {
			return nil, err
		}
		for _, block := range resp.Results {
			if *budget >= maxBlocks {
				break
			}
			*budget = *budget + 1
			out = append(out, block)
		}
		if !resp.HasMore || resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}
	for i := range out {
		if !out[i].HasChildren || out[i].ID == "" {
			continue
		}
		children, err := s.listBlockChildren(ctx, g, out[i].ID, depth+1, budget)
		if err != nil {
			return nil, err
		}
		out[i].Children = children
	}
	return out, nil
}

func (s *Service) importAttachments(ctx context.Context, g notion.Gateway, memoID string, blocks []domain.Block, atts []notion.ImportedAttachment) ([]domain.Block, error) {
	if len(atts) == 0 {
		return blocks, nil
	}
	ids := make([]string, len(atts))
	for i, att := range atts {
		existingID := ""
		if existing, err := s.store.FindAttachmentByBlockID(ctx, memoID, att.BlockID); err == nil {
			if existing.Present {
				ids[i] = existing.ID
				continue
			}
			existingID = existing.ID
		}
		if s.importer == nil || att.URL == "" {
			ids[i] = existingID
			continue
		}
		data, err := g.Download(ctx, att.URL)
		if err != nil {
			return nil, err
		}
		name := att.Name
		if name == "" {
			name = filenameFromURL(att.URL)
		}
		if existingID != "" {
			// Evicted rows keep their block ID but have no bytes. Re-create the
			// metadata after the download so hydration restores the file.
			if err := s.store.DeleteAttachment(ctx, existingID); err != nil {
				return nil, err
			}
		}
		created, err := s.importer.SaveRemote(ctx, memoID, name, "", att.Kind, data)
		if err != nil {
			return nil, err
		}
		ids[i] = created.ID
		if err := s.store.SetAttachmentNotionBlock(ctx, created.ID, att.BlockID); err != nil {
			return nil, err
		}
	}
	idx := 0
	for i := range blocks {
		if blocks[i].Type != domain.BlockImage && blocks[i].Type != domain.BlockFile {
			continue
		}
		if idx < len(ids) && ids[idx] != "" {
			blocks[i].AttachmentID = ids[idx]
		}
		idx++
	}
	return blocks, nil
}

func filenameFromURL(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		if base := path.Base(u.Path); base != "." && base != "/" && base != "" {
			return base
		}
	}
	return "attachment"
}
