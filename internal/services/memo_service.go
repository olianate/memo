package services

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	"memo/internal/attachments"
	"memo/internal/domain"
	"memo/internal/store"
	"memo/internal/sync"
)

// MemoService is the capture, edit, delete and attachment boundary.
type MemoService struct {
	store       *store.Store
	sync        *sync.Service
	attachments *attachments.Service
	emit        func(name string, data any)
}

// NewMemoService builds the memo service.
func NewMemoService(st *store.Store, sy *sync.Service, at *attachments.Service, emit func(string, any)) *MemoService {
	return &MemoService{store: st, sync: sy, attachments: at, emit: emit}
}

func (s *MemoService) changed(id string) {
	if s.emit != nil {
		s.emit("memos:changed", map[string]string{"id": id})
	}
}

// List returns the timeline. Bodies are included; evicted memos return an
// empty block list and local_state=evicted.
func (s *MemoService) List(filter FilterDTO) ([]MemoDTO, error) {
	memos, err := s.store.ListMemos(contextBackground(), toMemoFilter(filter))
	if err != nil {
		return nil, err
	}
	out := make([]MemoDTO, 0, len(memos))
	for _, m := range memos {
		out = append(out, toMemoDTO(m, parseBlocks(m)))
	}
	return out, nil
}

// Get returns one memo, hydrating it first when its body was evicted. A failed
// hydration still returns the metadata so the UI can show a placeholder.
func (s *MemoService) Get(id string) (MemoDTO, error) {
	ctx := contextBackground()
	memo, err := s.store.GetMemo(ctx, id)
	if err != nil {
		return MemoDTO{}, err
	}
	if memo.LocalState == domain.LocalEvicted && s.sync != nil {
		if hydrated, err := s.sync.HydrateIfEvicted(ctx, id); err == nil {
			memo = hydrated
		}
	}
	return toMemoDTO(*memo, parseBlocks(*memo)), nil
}

// Counts returns the sidebar totals.
func (s *MemoService) Counts() (CountsDTO, error) {
	notes, tags, days, err := s.store.Counts(contextBackground())
	if err != nil {
		return CountsDTO{}, err
	}
	return CountsDTO{Notes: notes, Tags: tags, Days: days}, nil
}

// Create stores a new memo and returns it.
func (s *MemoService) Create(blocks []domain.Block, tags []string) (MemoDTO, error) {
	if len(blocks) > 500 {
		return MemoDTO{}, errors.New("a memo is limited to 500 blocks")
	}
	memo, err := s.store.CreateMemo(contextBackground(), store.MemoInput{Blocks: blocks, Tags: cleanTags(tags)})
	if err != nil {
		return MemoDTO{}, err
	}
	s.changed(memo.ID)
	return toMemoDTO(*memo, blocks), nil
}

// Update edits a memo. An evicted body is hydrated first; if that is not
// possible the edit is refused rather than overwriting the archive.
func (s *MemoService) Update(id string, blocks []domain.Block, tags []string, keepLocal bool) (MemoDTO, error) {
	if len(blocks) > 500 {
		return MemoDTO{}, errors.New("a memo is limited to 500 blocks")
	}
	ctx := contextBackground()
	memo, err := s.store.GetMemo(ctx, id)
	if err != nil {
		return MemoDTO{}, err
	}
	if memo.LocalState == domain.LocalEvicted {
		if s.sync == nil {
			return MemoDTO{}, errors.New("this memo is evicted and cannot be hydrated")
		}
		if err := s.sync.Hydrate(ctx, id); err != nil {
			return MemoDTO{}, fmt.Errorf("hydrate before editing: %w", err)
		}
	}
	if err := s.validateAttachmentIDs(ctx, id, blocks); err != nil {
		return MemoDTO{}, err
	}
	updated, err := s.store.UpdateMemo(ctx, id, blocks, cleanTags(tags), &keepLocal)
	if err != nil {
		return MemoDTO{}, err
	}
	s.changed(id)
	return toMemoDTO(*updated, blocks), nil
}

func (s *MemoService) validateAttachmentIDs(ctx context.Context, memoID string, blocks []domain.Block) error {
	for _, b := range blocks {
		if b.Type != domain.BlockImage && b.Type != domain.BlockFile {
			continue
		}
		if b.AttachmentID == "" {
			return errors.New("attachment block is missing its attachment id")
		}
		att, err := s.store.GetAttachment(ctx, b.AttachmentID)
		if err != nil || att.MemoID != memoID {
			return errors.New("attachment does not belong to this memo")
		}
	}
	return nil
}

// Delete soft-deletes a memo so the tombstone can sync.
func (s *MemoService) Delete(id string) error {
	if err := s.store.SoftDelete(contextBackground(), id); err != nil {
		return err
	}
	s.changed(id)
	return nil
}

// Restore undoes a soft delete.
func (s *MemoService) Restore(id string) (MemoDTO, error) {
	ctx := contextBackground()
	memo, err := s.store.Restore(ctx, id)
	if err != nil {
		return MemoDTO{}, err
	}
	// A previous purge may have recorded a tombstone before its local delete
	// failed. Restoring makes the page live again, so drop that marker.
	if memo.NotionPageID != "" {
		_ = s.store.ClearPurgedPage(ctx, memo.NotionPageID)
	}
	s.changed(id)
	return toMemoDTO(*memo, parseBlocks(*memo)), nil
}

// Purge permanently removes one memo from the local trash. A Notion page is
// queued for a tombstone so a later pull cannot resurrect it.
func (s *MemoService) Purge(id string) error {
	ctx := contextBackground()
	memo, err := s.store.GetMemo(ctx, id)
	if err != nil {
		return err
	}
	if memo.DeletedAt == nil {
		return errors.New("只能永久删除回收站中的笔记")
	}
	return s.purgeMemo(ctx, *memo)
}

// EmptyTrash permanently removes every memo in the trash.
func (s *MemoService) EmptyTrash() (int, error) {
	ctx := contextBackground()
	memos, err := s.store.DeletedMemos(ctx)
	if err != nil {
		return 0, err
	}
	purged := 0
	for i := range memos {
		if err := s.purgeMemo(ctx, memos[i]); err != nil {
			return purged, err
		}
		purged++
	}
	return purged, nil
}

func (s *MemoService) purgeMemo(ctx context.Context, memo domain.Memo) error {
	if memo.NotionPageID != "" {
		if err := s.store.MarkPagePurged(ctx, memo.NotionPageID); err != nil {
			return err
		}
	}
	if s.attachments != nil {
		for _, att := range memo.Attachments {
			_ = s.attachments.DeleteFiles(ctx, att)
		}
	}
	if err := s.store.DeleteMemo(ctx, memo.ID); err != nil {
		return err
	}
	s.changed(memo.ID)
	return nil
}

// SetKeepLocal pins a memo against retention eviction.
func (s *MemoService) SetKeepLocal(id string, keep bool) error {
	if err := s.store.SetKeepLocal(contextBackground(), id, keep); err != nil {
		return err
	}
	s.changed(id)
	return nil
}

// ParseMarkdown is the raw-editor → AST path. The canonical body stays blocks.
func (s *MemoService) ParseMarkdown(source string) []domain.Block {
	return markdownParse(source)
}

// SerializeMarkdown is the AST → raw-editor path.
func (s *MemoService) SerializeMarkdown(blocks []domain.Block) string {
	return markdownSerialize(blocks)
}

// AddAttachmentBase64 ingests a file from the webview. It appends the matching
// attachment block to the memo so the change is durable immediately.
func (s *MemoService) AddAttachmentBase64(memoID, name, mime, data string) (AttachmentDTO, error) {
	if len(data) > 96<<20 {
		return AttachmentDTO{}, errors.New("file is too large")
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return AttachmentDTO{}, errors.New("file payload is not valid base64")
	}
	ctx := contextBackground()
	memo, err := s.store.GetMemo(ctx, memoID)
	if err != nil {
		return AttachmentDTO{}, err
	}
	if memo.LocalState == domain.LocalEvicted {
		return AttachmentDTO{}, errors.New("hydrate this memo before adding attachments")
	}
	att, err := s.attachments.Add(ctx, memoID, name, mime, raw)
	if err != nil {
		return AttachmentDTO{}, err
	}
	blocks := parseBlocks(*memo)
	blockType := domain.BlockFile
	if att.Kind == domain.AttachmentImage {
		blockType = domain.BlockImage
	}
	blocks = append(blocks, domain.Block{
		ID: domain.NewID(), Type: blockType, AttachmentID: att.ID, Alt: att.OriginalName,
	})
	tags := make([]string, 0, len(memo.Tags))
	for _, t := range memo.Tags {
		tags = append(tags, t.Name)
	}
	if _, err := s.store.UpdateMemo(ctx, memoID, blocks, tags, nil); err != nil {
		_ = s.attachments.Remove(ctx, att.ID)
		return AttachmentDTO{}, err
	}
	s.changed(memoID)
	return toAttachmentDTO(*att), nil
}

// RemoveAttachment deletes an attachment and its block from the memo body.
func (s *MemoService) RemoveAttachment(id string) error {
	ctx := contextBackground()
	att, err := s.store.GetAttachment(ctx, id)
	if err != nil {
		return err
	}
	memo, err := s.store.GetMemo(ctx, att.MemoID)
	if err != nil {
		return err
	}
	blocks := parseBlocks(*memo)
	kept := blocks[:0]
	for _, b := range blocks {
		if b.AttachmentID != id {
			kept = append(kept, b)
		}
	}
	tags := make([]string, 0, len(memo.Tags))
	for _, t := range memo.Tags {
		tags = append(tags, t.Name)
	}
	if memo.LocalState == domain.LocalPresent {
		if _, err := s.store.UpdateMemo(ctx, memo.ID, kept, tags, nil); err != nil {
			return err
		}
	}
	if err := s.attachments.Remove(ctx, id); err != nil {
		return err
	}
	s.changed(memo.ID)
	return nil
}

// AttachmentThumb returns a data URL for an image thumbnail.
func (s *MemoService) AttachmentThumb(id string) (string, error) {
	return s.attachments.ThumbnailDataURL(contextBackground(), id)
}

// AttachmentData returns a data URL for the full image.
func (s *MemoService) AttachmentData(id string) (string, error) {
	return s.attachments.DataURL(contextBackground(), id)
}

// OpenAttachment launches the OS default application. No path crosses the
// webview boundary.
func (s *MemoService) OpenAttachment(id string) error {
	return s.attachments.Open(contextBackground(), id)
}

// Hydrate fetches an evicted memo's body and attachments from Notion.
func (s *MemoService) Hydrate(id string) (MemoDTO, error) {
	if s.sync == nil {
		return MemoDTO{}, errors.New("sync service is unavailable")
	}
	if err := s.sync.Hydrate(contextBackground(), id); err != nil {
		return MemoDTO{}, err
	}
	memo, err := s.store.GetMemo(contextBackground(), id)
	if err != nil {
		return MemoDTO{}, err
	}
	return toMemoDTO(*memo, parseBlocks(*memo)), nil
}
