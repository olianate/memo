// Package memoedit is the single write path for memo bodies. A caller describes
// one edit as a change-set; the module decides whether the body must be
// hydrated, applies tags and attachments together, and commits exactly one
// revision and one sync outbox entry.
package memoedit

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/olianate/memo/internal/attachments"
	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/domain/markdown"
	"github.com/olianate/memo/internal/store"
)

var (
	// ErrNotFound reports a memo or attachment that is not in the store.
	ErrNotFound = domain.ErrNotFound
	// ErrEvicted reports a body edit on an evicted memo that could not hydrate.
	ErrEvicted = errors.New("memo body is evicted")
	// ErrAttachmentTooLarge reports a file above the module's own ceiling.
	ErrAttachmentTooLarge = errors.New("attachment is too large")
	// ErrAttachmentNotOwned reports an attachment that belongs to another memo.
	ErrAttachmentNotOwned = errors.New("attachment does not belong to this memo")
)

// maxFileBytes is the module ceiling; the attachment service still enforces the
// workspace and Notion upload limits underneath.
const maxFileBytes = 64 << 20

// File is one attachment to ingest with an edit.
type File struct {
	Name string
	MIME string
	Data []byte
}

// Draft is a new memo. Files are attached in the same transaction, so a memo
// created with images is still revision 1.
type Draft struct {
	Body      string
	Tags      []string
	Files     []File
	KeepLocal bool
}

// Change is one edit. Nil pointers mean "leave untouched"; an empty Body
// pointer clears the body. AddFiles and RemoveAttachmentIDs apply to the same
// revision as the body and tag changes.
type Change struct {
	Body                *string
	Tags                *[]string
	KeepLocal           *bool
	AddFiles            []File
	RemoveAttachmentIDs []string
	// PreserveAttachments keeps attachment lines from the current body that the
	// new body does not mention. A content-only edit uses it so images are not
	// silently dropped; a full-body save leaves it false.
	PreserveAttachments bool
}

// Presence materialises a memo body before an edit. presence.Keeper is the
// production adapter; tests pass the real keeper or a stub.
type Presence interface {
	Evicted(m domain.Memo) bool
	Ensure(ctx context.Context, id string) (*domain.Memo, error)
}

// Editor applies memo edits against the store.
type Editor struct {
	store    *store.Store
	files    *attachments.Service
	presence Presence
	onChange func(string)
}

// New builds the editor. presence hydrates an evicted body before a body edit;
// onChange is called after every committed edit.
func New(st *store.Store, files *attachments.Service, presence Presence, onChange func(string)) *Editor {
	return &Editor{store: st, files: files, presence: presence, onChange: onChange}
}

// Create writes a new memo, its tags, its files and its outbox row.
func (e *Editor) Create(ctx context.Context, d Draft) (*domain.Memo, error) {
	if len(d.Files) > 0 && e.files == nil {
		return nil, errors.New("attachment storage is unavailable")
	}
	id := domain.NewID()
	body := d.Body
	staged := make([]domain.Attachment, 0, len(d.Files))
	for _, f := range d.Files {
		if err := e.checkSize(f); err != nil {
			e.discardStaged(ctx, staged)
			return nil, err
		}
		att, err := e.files.Stage(ctx, id, f.Name, f.MIME, f.Data)
		if err != nil {
			e.discardStaged(ctx, staged)
			return nil, err
		}
		staged = append(staged, *att)
		body = markdown.AppendAttachment(body, blockTypeFor(att.Kind), att.ID, att.OriginalName)
	}
	memo, err := e.store.CreateMemo(ctx, store.MemoInput{
		ID:          id,
		Body:        body,
		Tags:        normalizeTags(d.Tags),
		Attachments: staged,
		KeepLocal:   d.KeepLocal,
	})
	if err != nil {
		e.discardStaged(ctx, staged)
		return nil, err
	}
	e.notify(memo.ID)
	return memo, nil
}

// Edit applies one change-set. Metadata-only edits never hydrate; body or
// attachment edits hydrate first and refuse the whole edit when that fails.
func (e *Editor) Edit(ctx context.Context, id string, c Change) (*domain.Memo, error) {
	memo, err := e.store.GetMemo(ctx, id)
	if err != nil {
		return nil, err
	}
	touchesBody := c.Body != nil || len(c.AddFiles) > 0 || len(c.RemoveAttachmentIDs) > 0
	if touchesBody {
		if e.presence == nil {
			return nil, fmt.Errorf("%w: no hydrator is configured", ErrEvicted)
		}
		if e.presence.Evicted(*memo) {
			hydrated, err := e.hydrateForEdit(ctx, id)
			if err != nil {
				return nil, err
			}
			memo = hydrated
		}
	}

	body := memo.Body
	if c.Body != nil {
		body = *c.Body
		if c.PreserveAttachments {
			body = preserveAttachments(memo.Body, body)
		}
	}

	removed, body, err := e.detach(ctx, id, body, c.RemoveAttachmentIDs)
	if err != nil {
		return nil, err
	}

	added, body, err := e.attach(ctx, id, body, c.AddFiles)
	if err != nil {
		e.discardAdded(ctx, added)
		return nil, err
	}

	tags := tagNames(memo.Tags)
	if c.Tags != nil {
		tags = normalizeTags(*c.Tags)
	}
	keepLocal := memo.KeepLocal
	if c.KeepLocal != nil {
		keepLocal = *c.KeepLocal
	}

	changed := body != memo.Body || !sameTags(tags, tagNames(memo.Tags)) || keepLocal != memo.KeepLocal
	result := memo
	if changed {
		if touchesBody {
			blocks, err := e.validateBodyAttachments(ctx, id, body)
			if err != nil {
				e.discardAdded(ctx, added)
				return nil, err
			}
			result, err = e.store.UpdateMemoDerived(ctx, id, body, tags, &keepLocal, store.Derive(body, tags, blocks))
		} else {
			result, err = e.store.UpdateMemoMetadata(ctx, id, tags, keepLocal)
		}
		if err != nil {
			e.discardAdded(ctx, added)
			return nil, err
		}
	}

	// Attachment rows are deleted only after the body no longer references
	// them, so a failed edit cannot leave a body pointing at missing files.
	for _, att := range removed {
		if err := e.files.Remove(ctx, att.ID); err != nil {
			_ = e.store.Log(ctx, "warn", "memoedit", fmt.Sprintf("attachment %s was detached but its files remain: %v", att.ID, err))
		}
	}
	if len(removed) > 0 {
		if fresh, err := e.store.GetMemo(ctx, id); err == nil {
			result = fresh
		}
	}
	if changed || len(removed) > 0 {
		e.notify(id)
	}
	return result, nil
}

func (e *Editor) hydrateForEdit(ctx context.Context, id string) (*domain.Memo, error) {
	if e.presence == nil {
		return nil, fmt.Errorf("%w: no hydrator is configured", ErrEvicted)
	}
	memo, err := e.presence.Ensure(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEvicted, err)
	}
	if e.presence.Evicted(*memo) {
		return nil, fmt.Errorf("%w: the body is still not available locally", ErrEvicted)
	}
	return memo, nil
}

func (e *Editor) attach(ctx context.Context, memoID, body string, files []File) ([]domain.Attachment, string, error) {
	if len(files) == 0 {
		return nil, body, nil
	}
	if e.files == nil {
		return nil, body, errors.New("attachment storage is unavailable")
	}
	added := make([]domain.Attachment, 0, len(files))
	for _, f := range files {
		if err := e.checkSize(f); err != nil {
			return added, body, err
		}
		att, err := e.files.Add(ctx, memoID, f.Name, f.MIME, f.Data)
		if err != nil {
			return added, body, err
		}
		added = append(added, *att)
		body = markdown.AppendAttachment(body, blockTypeFor(att.Kind), att.ID, att.OriginalName)
	}
	return added, body, nil
}

func (e *Editor) detach(ctx context.Context, memoID, body string, ids []string) ([]domain.Attachment, string, error) {
	if len(ids) == 0 {
		return nil, body, nil
	}
	removed := make([]domain.Attachment, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		att, err := e.store.GetAttachment(ctx, id)
		if err != nil {
			return nil, body, err
		}
		if att.MemoID != memoID {
			return nil, body, fmt.Errorf("%w: %s", ErrAttachmentNotOwned, id)
		}
		body = markdown.RemoveAttachment(body, id)
		removed = append(removed, *att)
	}
	return removed, body, nil
}

// validateBodyAttachments returns the parsed body after checking that every
// attachment it references belongs to this memo. The blocks are handed back so
// the write path reuses the single parse.
func (e *Editor) validateBodyAttachments(ctx context.Context, memoID, body string) ([]domain.Block, error) {
	blocks := markdown.Parse(body)
	for _, block := range blocks {
		if block.Type != domain.BlockImage && block.Type != domain.BlockFile {
			continue
		}
		if block.AttachmentID == "" {
			return nil, errors.New("attachment block is missing its attachment id")
		}
		att, err := e.store.GetAttachment(ctx, block.AttachmentID)
		if err != nil || att.MemoID != memoID {
			return nil, fmt.Errorf("%w: %s", ErrAttachmentNotOwned, block.AttachmentID)
		}
	}
	return blocks, nil
}

func (e *Editor) checkSize(f File) error {
	if int64(len(f.Data)) > maxFileBytes {
		return fmt.Errorf("%w: %s exceeds %d MiB", ErrAttachmentTooLarge, f.Name, maxFileBytes>>20)
	}
	return nil
}

func (e *Editor) discardStaged(ctx context.Context, staged []domain.Attachment) {
	for _, att := range staged {
		_ = e.files.DeleteFiles(ctx, att)
	}
}

func (e *Editor) discardAdded(ctx context.Context, added []domain.Attachment) {
	for _, att := range added {
		if err := e.files.Remove(ctx, att.ID); err != nil {
			_ = e.store.Log(ctx, "warn", "memoedit", fmt.Sprintf("rollback of attachment %s failed: %v", att.ID, err))
		}
	}
}

func (e *Editor) notify(id string) {
	if e.onChange != nil {
		e.onChange(id)
	}
}

func blockTypeFor(kind domain.AttachmentKind) domain.BlockType {
	if kind == domain.AttachmentImage {
		return domain.BlockImage
	}
	return domain.BlockFile
}

// preserveAttachments re-appends image and file lines from the current body
// that the next body no longer mentions.
func preserveAttachments(currentBody, nextBody string) string {
	referenced := make(map[string]bool)
	for _, block := range markdown.Parse(nextBody) {
		if block.AttachmentID != "" {
			referenced[block.AttachmentID] = true
		}
	}
	for _, block := range markdown.Parse(currentBody) {
		if block.Type != domain.BlockImage && block.Type != domain.BlockFile {
			continue
		}
		if block.AttachmentID == "" || referenced[block.AttachmentID] {
			continue
		}
		nextBody = markdown.AppendAttachment(nextBody, block.Type, block.AttachmentID, block.Alt)
	}
	return nextBody
}

func normalizeTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = domain.CleanTagName(tag)
		if tag != "" && !strings.Contains(tag, " ") {
			out = append(out, tag)
		}
	}
	return out
}

func tagNames(tags []domain.Tag) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, t.Name)
	}
	return out
}

func sameTags(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, t := range a {
		counts[strings.ToLower(t)]++
	}
	for _, t := range b {
		key := strings.ToLower(t)
		if counts[key] == 0 {
			return false
		}
		counts[key]--
	}
	return true
}
