// Package services is the typed Wails boundary. The frontend depends on these
// methods, never on the store, the Notion client or the filesystem.
package services

import (
	"context"
	"time"

	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/store"
)

// MemoDTO is the frontend view of a memo.
type MemoDTO struct {
	ID             string          `json:"id"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
	Revision       int64           `json:"revision"`
	Snippet        string          `json:"snippet"`
	Deleted        bool            `json:"deleted"`
	DeletedAt      string          `json:"deleted_at,omitempty"`
	KeepLocal      bool            `json:"keep_local"`
	LocalState     string          `json:"local_state"`
	NotionPageID   string          `json:"notion_page_id,omitempty"`
	NotionSyncedAt string          `json:"notion_synced_at,omitempty"`
	LastHydratedAt string          `json:"last_hydrated_at,omitempty"`
	SyncError      string          `json:"sync_error,omitempty"`
	Body           string          `json:"body"`
	Tags           []TagDTO        `json:"tags"`
	Attachments    []AttachmentDTO `json:"attachments"`
	Day            string          `json:"day"`
}

// AttachmentPayload is one file crossing the webview boundary. Data is base64
// so a batch of files travels through a single call.
type AttachmentPayload struct {
	Name string `json:"name"`
	MIME string `json:"mime"`
	Data string `json:"data"`
}

// TagDTO is one tag with its active-memo count.
type TagDTO struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Normalized string `json:"normalized"`
	Count      int    `json:"count"`
}

// AttachmentDTO is attachment metadata plus a presentation hint.
type AttachmentDTO struct {
	ID           string `json:"id"`
	MemoID       string `json:"memo_id"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	MIME         string `json:"mime"`
	Bytes        int64  `json:"bytes"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	Present      bool   `json:"present"`
	HasThumb     bool   `json:"has_thumb"`
	NotionFileID string `json:"notion_file_id,omitempty"`
}

// CountsDTO is the sidebar summary.
type CountsDTO struct {
	Notes int `json:"notes"`
	Tags  int `json:"tags"`
	Days  int `json:"days"`
}

// FilterDTO is the timeline filter.
type FilterDTO struct {
	Deleted        string `json:"deleted"` // active | deleted | all
	Tag            string `json:"tag"`
	LocalOnly      bool   `json:"local_only"`
	Synced         bool   `json:"synced"`
	WithAttachment bool   `json:"with_attachment"`
	Untagged       bool   `json:"untagged"`
	From           string `json:"from,omitempty"`
	To             string `json:"to,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

// SearchResultDTO is one result with a safe highlighted snippet.
type SearchResultDTO struct {
	Memo   MemoDTO `json:"memo"`
	Before string  `json:"before"`
	Match  string  `json:"match"`
	After  string  `json:"after"`
}

func toMemoDTO(m domain.Memo) MemoDTO {
	dto := MemoDTO{
		ID:           m.ID,
		CreatedAt:    m.CreatedAt.Local().Format(time.RFC3339),
		UpdatedAt:    m.UpdatedAt.Local().Format(time.RFC3339),
		Revision:     m.Revision,
		Snippet:      m.Snippet,
		Deleted:      m.DeletedAt != nil,
		KeepLocal:    m.KeepLocal,
		LocalState:   string(m.LocalState),
		NotionPageID: m.NotionPageID,
		SyncError:    m.SyncError,
		Body:         m.Body,
		Tags:         []TagDTO{},
		Attachments:  []AttachmentDTO{},
		Day:          m.CreatedAt.Local().Format("2006-01-02"),
	}
	if m.DeletedAt != nil {
		dto.DeletedAt = m.DeletedAt.Local().Format(time.RFC3339)
	}
	if m.NotionSyncedAt != nil {
		dto.NotionSyncedAt = m.NotionSyncedAt.Local().Format(time.RFC3339)
	}
	if m.LastHydratedAt != nil {
		dto.LastHydratedAt = m.LastHydratedAt.Local().Format(time.RFC3339)
	}
	for _, t := range m.Tags {
		dto.Tags = append(dto.Tags, TagDTO{ID: t.ID, Name: t.Name, Normalized: t.NormalizedName})
	}
	for _, a := range m.Attachments {
		dto.Attachments = append(dto.Attachments, toAttachmentDTO(a))
	}
	return dto
}

func toAttachmentDTO(a domain.Attachment) AttachmentDTO {
	return AttachmentDTO{
		ID: a.ID, MemoID: a.MemoID, Kind: string(a.Kind), Name: a.OriginalName, MIME: a.MIME,
		Bytes: a.Bytes, Width: a.Width, Height: a.Height, Present: a.Present,
		HasThumb:     a.Kind == domain.AttachmentImage && (a.ThumbPath != "" || a.LocalPath != ""),
		NotionFileID: a.NotionFileID,
	}
}

func toMemoFilter(f FilterDTO) store.MemoFilter {
	out := store.MemoFilter{
		Tag: f.Tag, LocalOnly: f.LocalOnly, Synced: f.Synced,
		WithAttachment: f.WithAttachment, Untagged: f.Untagged, Limit: f.Limit,
	}
	switch f.Deleted {
	case string(store.DeletedOnly):
		out.Deleted = store.DeletedOnly
	case string(store.DeletedAll):
		out.Deleted = store.DeletedAll
	default:
		out.Deleted = store.DeletedActive
	}
	if t, err := time.Parse(time.RFC3339, f.From); err == nil {
		out.From = &t
	}
	if t, err := time.Parse(time.RFC3339, f.To); err == nil {
		out.To = &t
	}
	return out
}

// contextBackground is used by event callbacks that outlive the call.
func contextBackground() context.Context { return context.Background() }
