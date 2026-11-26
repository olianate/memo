package services

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/olianate/memo/internal/attachments"
	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/memoedit"
	"github.com/olianate/memo/internal/presence"
	"github.com/olianate/memo/internal/store"
)

// MemoService is the capture, edit, delete and attachment boundary.
type MemoService struct {
	store       *store.Store
	presence    *presence.Keeper
	attachments *attachments.Service
	editor      *memoedit.Editor
	emit        func(name string, data any)
}

// NewMemoService builds the memo service. The editor is the shared write path
// for memo bodies, so the MCP server and the webview apply the same rules.
func NewMemoService(st *store.Store, at *attachments.Service, emit func(string, any),
	editor *memoedit.Editor, keeper *presence.Keeper) *MemoService {
	return &MemoService{store: st, presence: keeper, attachments: at, editor: editor, emit: emit}
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
		out = append(out, toMemoDTO(m))
	}
	return out, nil
}

// Get returns one memo, hydrating it first when its body was evicted. A failed
// hydration still returns the metadata so the UI can show a placeholder.
func (s *MemoService) Get(id string) (MemoDTO, error) {
	ctx := contextBackground()
	memo, err := s.presence.Ensure(ctx, id)
	if err != nil {
		if memo == nil {
			return MemoDTO{}, err
		}
		_ = s.store.Log(ctx, "warn", "memo", fmt.Sprintf("body of %s is unavailable: %v", id, err))
	}
	return toMemoDTO(*memo), nil
}

// Counts returns the sidebar totals.
func (s *MemoService) Counts() (CountsDTO, error) {
	notes, tags, days, err := s.store.Counts(contextBackground())
	if err != nil {
		return CountsDTO{}, err
	}
	return CountsDTO{Notes: notes, Tags: tags, Days: days}, nil
}

// Create stores a new memo body and returns it.
func (s *MemoService) Create(body string, tags []string) (MemoDTO, error) {
	memo, err := s.editor.Create(contextBackground(), memoedit.Draft{Body: body, Tags: tags})
	if err != nil {
		return MemoDTO{}, err
	}
	return toMemoDTO(*memo), nil
}

// Update edits a memo body. An evicted body is hydrated first; if that is not
// possible the edit is refused rather than overwriting the archive.
func (s *MemoService) Update(id string, body string, tags []string, keepLocal bool) (MemoDTO, error) {
	updated, err := s.editor.Edit(contextBackground(), id, memoedit.Change{
		Body:      &body,
		Tags:      &tags,
		KeepLocal: &keepLocal,
	})
	if err != nil {
		return MemoDTO{}, err
	}
	return toMemoDTO(*updated), nil
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
	return toMemoDTO(*memo), nil
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

// trashRetention is how long a memo stays recoverable in the local trash.
const trashRetention = 30 * 24 * time.Hour

// PurgeExpiredTrash permanently removes memos that have been in the trash for
// at least 30 days. It runs independently of Notion sync; purged pages are
// tombstoned and flushed by the next sync that reaches the server.
func (s *MemoService) PurgeExpiredTrash() (int, error) {
	return s.purgeExpiredTrash(contextBackground(), time.Now().UTC())
}

func (s *MemoService) purgeExpiredTrash(ctx context.Context, now time.Time) (int, error) {
	memos, err := s.store.DeletedMemos(ctx)
	if err != nil {
		return 0, err
	}
	purged := 0
	for i := range memos {
		memo := memos[i]
		if memo.DeletedAt == nil || now.Before(memo.DeletedAt.Add(trashRetention)) {
			continue
		}
		if err := s.purgeMemo(ctx, memo); err != nil {
			return purged, err
		}
		purged++
	}
	if purged > 0 {
		_ = s.store.Log(ctx, "info", "trash", fmt.Sprintf("自动清理了 %d 条超过 30 天的回收站笔记", purged))
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

// SetPinned moves a memo to the top of the timeline. Pinning is local-only and
// does not create a new revision or a sync change.
func (s *MemoService) SetPinned(id string, pinned bool) error {
	if err := s.store.SetPinned(contextBackground(), id, pinned); err != nil {
		return err
	}
	s.changed(id)
	return nil
}

// CreateWithAttachments stores a new memo and its files as one revision, so a
// capture with images never passes through an empty draft memo.
func (s *MemoService) CreateWithAttachments(body string, tags []string, files []AttachmentPayload) (MemoDTO, error) {
	decoded, err := decodePayloads(files)
	if err != nil {
		return MemoDTO{}, err
	}
	memo, err := s.editor.Create(contextBackground(), memoedit.Draft{Body: body, Tags: tags, Files: decoded})
	if err != nil {
		return MemoDTO{}, err
	}
	return toMemoDTO(*memo), nil
}

// AddAttachmentBase64 ingests a file from the webview. It appends the matching
// attachment reference to the Markdown body so the change is durable immediately.
func (s *MemoService) AddAttachmentBase64(memoID, name, mime, data string) (AttachmentDTO, error) {
	added, err := s.AddAttachments(memoID, []AttachmentPayload{{Name: name, MIME: mime, Data: data}})
	if err != nil {
		return AttachmentDTO{}, err
	}
	if len(added) != 1 {
		return AttachmentDTO{}, errors.New("attachment was written but is missing from the memo")
	}
	return added[0], nil
}

// AddAttachments ingests several files as one revision and one sync outbox
// entry, and returns the attachments it added.
func (s *MemoService) AddAttachments(memoID string, files []AttachmentPayload) ([]AttachmentDTO, error) {
	decoded, err := decodePayloads(files)
	if err != nil {
		return nil, err
	}
	if len(decoded) == 0 {
		return []AttachmentDTO{}, nil
	}
	ctx := contextBackground()
	before, err := s.store.GetMemo(ctx, memoID)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(before.Attachments))
	for _, att := range before.Attachments {
		known[att.ID] = true
	}
	memo, err := s.editor.Edit(ctx, memoID, memoedit.Change{AddFiles: decoded})
	if err != nil {
		return nil, err
	}
	added := make([]AttachmentDTO, 0, len(decoded))
	for _, att := range memo.Attachments {
		if !known[att.ID] {
			added = append(added, toAttachmentDTO(att))
		}
	}
	if len(added) != len(decoded) {
		return nil, errors.New("an attachment was written but is missing from the memo")
	}
	return added, nil
}

func decodePayloads(files []AttachmentPayload) ([]memoedit.File, error) {
	out := make([]memoedit.File, 0, len(files))
	for _, f := range files {
		if len(f.Data) > 96<<20 {
			return nil, errors.New("file is too large")
		}
		raw, err := base64.StdEncoding.DecodeString(f.Data)
		if err != nil {
			return nil, errors.New("file payload is not valid base64")
		}
		out = append(out, memoedit.File{Name: f.Name, MIME: f.MIME, Data: raw})
	}
	return out, nil
}

// RemoveAttachment deletes an attachment and its block from the memo body.
func (s *MemoService) RemoveAttachment(id string) error {
	ctx := contextBackground()
	att, err := s.store.GetAttachment(ctx, id)
	if err != nil {
		return err
	}
	_, err = s.editor.Edit(ctx, att.MemoID, memoedit.Change{RemoveAttachmentIDs: []string{id}})
	return err
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
	if s.presence == nil {
		return MemoDTO{}, errors.New("sync service is unavailable")
	}
	memo, err := s.presence.Ensure(contextBackground(), id)
	if err != nil {
		return MemoDTO{}, err
	}
	return toMemoDTO(*memo), nil
}
