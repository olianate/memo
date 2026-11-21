package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// LocalState describes whether a memo's body and attachments are on disk.
type LocalState string

const (
	LocalPresent LocalState = "present"
	LocalEvicted LocalState = "evicted"
)

// BlockType is the canonical block discriminator in body_json.
type BlockType string

const (
	BlockParagraph BlockType = "paragraph"
	BlockHeading   BlockType = "heading"
	BlockBullet    BlockType = "bullet"
	BlockOrdered   BlockType = "ordered"
	BlockTodo      BlockType = "todo"
	BlockQuote     BlockType = "quote"
	BlockCode      BlockType = "code"
	BlockDivider   BlockType = "divider"
	BlockImage     BlockType = "image"
	BlockFile      BlockType = "file"
)

// Span is a run of text with inline marks. Marks are intentionally flat: the
// Notion mapper needs the final annotation set anyway.
type Span struct {
	Text   string `json:"text"`
	Bold   bool   `json:"bold,omitempty"`
	Italic bool   `json:"italic,omitempty"`
	Strike bool   `json:"strike,omitempty"`
	Code   bool   `json:"code,omitempty"`
	Link   string `json:"link,omitempty"`
}

// Block is one element of the canonical memo body. Markdown is a projection of
// this AST, not a second source of truth.
type Block struct {
	ID           string    `json:"id"`
	Type         BlockType `json:"type"`
	Level        int       `json:"level,omitempty"`
	Checked      bool      `json:"checked,omitempty"`
	Language     string    `json:"language,omitempty"`
	Spans        []Span    `json:"spans,omitempty"`
	AttachmentID string    `json:"attachment_id,omitempty"`
	Alt          string    `json:"alt,omitempty"`
}

// PlainText returns the block's text with Markdown syntax stripped.
func (b Block) PlainText() string {
	if b.Type == BlockDivider || b.Type == BlockImage || b.Type == BlockFile {
		return ""
	}
	var sb strings.Builder
	for _, span := range b.Spans {
		sb.WriteString(span.Text)
	}
	return sb.String()
}

// Text returns the plain text of all text blocks, one block per line.
func Text(blocks []Block) string {
	lines := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b.Type == BlockDivider || b.Type == BlockImage || b.Type == BlockFile {
			continue
		}
		lines = append(lines, b.PlainText())
	}
	return strings.Join(lines, "\n")
}

// Snippet returns the first ~200 characters of text, whitespace collapsed.
func Snippet(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if limit <= 0 {
		limit = 200
	}
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "…"
}

// HashContent is the per-memo content hash written to Notion-adjacent state and
// used to skip no-op pushes. Tags are part of the content.
func HashContent(blocks []Block, tags []string) string {
	payload := struct {
		Blocks []Block  `json:"blocks"`
		Tags   []string `json:"tags"`
	}{Blocks: blocks, Tags: sortedUnique(tags)}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// MarshalBlocks serializes the block AST for storage.
func MarshalBlocks(blocks []Block) (string, error) {
	if blocks == nil {
		blocks = []Block{}
	}
	data, err := json.Marshal(blocks)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// UnmarshalBlocks parses body_json. A missing body is an empty body.
func UnmarshalBlocks(raw string) ([]Block, error) {
	if strings.TrimSpace(raw) == "" {
		return []Block{}, nil
	}
	var blocks []Block
	if err := json.Unmarshal([]byte(raw), &blocks); err != nil {
		return nil, err
	}
	if blocks == nil {
		return []Block{}, nil
	}
	return blocks, nil
}

var ErrNotFound = errors.New("not found")

// IsSafeID guards ids that become filesystem path segments. Local ids are
// UUIDv7, but pulled Notion properties are untrusted input.
func IsSafeID(id string) bool {
	if id == "" || len(id) > 100 || id == "." || id == ".." {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// Tag is first-class metadata; it is never part of the Markdown body.
type Tag struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	NormalizedName string `json:"normalized_name"`
}

// Memo is the unit of capture, search, sync and eviction.
type Memo struct {
	ID             string       `json:"id"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
	Revision       int64        `json:"revision"`
	BodyJSON       string       `json:"-"`
	BodyText       string       `json:"-"`
	Snippet        string       `json:"snippet"`
	DeletedAt      *time.Time   `json:"deleted_at,omitempty"`
	KeepLocal      bool         `json:"keep_local"`
	LocalState     LocalState   `json:"local_state"`
	NotionPageID   string       `json:"notion_page_id,omitempty"`
	NotionSyncedAt *time.Time   `json:"notion_synced_at,omitempty"`
	LastHydratedAt *time.Time   `json:"last_hydrated_at,omitempty"`
	BaseRevision   int64        `json:"base_revision"`
	ContentHash    string       `json:"content_hash"`
	SyncError      string       `json:"sync_error,omitempty"`
	BlockStats     BlockStats   `json:"-"`
	Tags           []Tag        `json:"tags"`
	Attachments    []Attachment `json:"attachments"`
}

// Blocks unmarshals the memo body.
func (m Memo) Blocks() ([]Block, error) { return UnmarshalBlocks(m.BodyJSON) }

// AttachmentKind separates image rendering from file chips.
type AttachmentKind string

const (
	AttachmentImage AttachmentKind = "image"
	AttachmentFile  AttachmentKind = "file"
)

// Attachment metadata survives local eviction; bytes do not.
type Attachment struct {
	ID            string         `json:"id"`
	MemoID        string         `json:"memo_id"`
	Kind          AttachmentKind `json:"kind"`
	OriginalName  string         `json:"original_name"`
	LocalPath     string         `json:"-"`
	ThumbPath     string         `json:"-"`
	MIME          string         `json:"mime"`
	Bytes         int64          `json:"bytes"`
	Width         int            `json:"width,omitempty"`
	Height        int            `json:"height,omitempty"`
	SHA256        string         `json:"sha256"`
	NotionFileID  string         `json:"notion_file_id,omitempty"`
	NotionBlockID string         `json:"notion_block_id,omitempty"`
	UploadedAt    *time.Time     `json:"uploaded_at,omitempty"`
	EvictedAt     *time.Time     `json:"evicted_at,omitempty"`
	Present       bool           `json:"present"`
}

// OutboxOp is the coalesced operation for a memo.
type OutboxOp string

const (
	OutboxUpsert OutboxOp = "upsert"
	OutboxDelete OutboxOp = "delete"
)

// OutboxItem is one durable pending Notion write.
type OutboxItem struct {
	MemoID        string    `json:"memo_id"`
	Op            OutboxOp  `json:"op"`
	Revision      int64     `json:"revision"`
	Attempts      int       `json:"attempts"`
	NextAttemptAt time.Time `json:"next_attempt_at"`
	LastError     string    `json:"last_error,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// DeviceState is the per-device sync cursor and maintenance clock.
type DeviceState struct {
	DeviceID       string     `json:"device_id"`
	LastPullAt     *time.Time `json:"last_pull_at,omitempty"`
	LastFullScanAt *time.Time `json:"last_full_scan_at,omitempty"`
	LastTrashSweep *time.Time `json:"last_trash_sweep_at,omitempty"`
}

// SyncStatus is the UI-facing sync state machine.
type SyncStatus string

const (
	SyncIdle    SyncStatus = "idle"
	SyncSyncing SyncStatus = "syncing"
	SyncOffline SyncStatus = "offline"
	SyncPaused  SyncStatus = "paused"
	SyncError   SyncStatus = "error"
)

func sortedUnique(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
