// Package notion is the Notion REST gateway contract plus its HTTP adapter.
// The app treats Notion as an archive: the local SQLite store is authoritative
// for reads and writes, and every Notion call goes through this boundary.
package notion

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// API version pinned by the spec. The 2026-03-11 API splits databases into
// data sources and rejects the old `archived` field.
const APIVersion = "2026-03-11"

// SchemaVersion is written to the app-owned `Schema Version` property.
const SchemaVersion = 1

// Property names are fixed by the app. The title property is user-defined in
// Notion, so its name is discovered and stored separately.
const (
	PropMemoID        = "Memo ID"
	PropCreated       = "Created"
	PropUpdated       = "Updated"
	PropRevision      = "Revision"
	PropDevice        = "Device"
	PropTags          = "Tags"
	PropStatus        = "Status"
	PropSchemaVersion = "Schema Version"
)

// Status values for the tombstone property.
const (
	StatusActive  = "active"
	StatusDeleted = "deleted"
)

// Gateway is the port the sync service depends on. Tests use a fake.
type Gateway interface {
	Me(ctx context.Context) (*User, error)
	SearchParentPages(ctx context.Context, query string) (*ParentPageSearch, error)
	SearchTrash(ctx context.Context) ([]Page, error)
	CreateMemoDatabase(ctx context.Context, parentPageID, title string, properties map[string]PropertySchema) (*DataSource, error)
	ResolveDataSource(ctx context.Context, id string) (*DataSource, error)
	RetrieveDataSource(ctx context.Context, dataSourceID string) (*DataSource, error)
	UpdateDataSourceSchema(ctx context.Context, dataSourceID string, properties map[string]PropertySchema) error
	QueryDataSource(ctx context.Context, dataSourceID string, req QueryRequest) (*QueryResponse, error)
	CreatePage(ctx context.Context, dataSourceID string, properties map[string]PropertyValue, children []Block) (*Page, error)
	UpdatePage(ctx context.Context, pageID string, properties map[string]PropertyValue) error
	GetPage(ctx context.Context, pageID string) (*Page, error)
	ListChildren(ctx context.Context, blockID, cursor string, pageSize int) (*ChildrenResponse, error)
	AppendChildren(ctx context.Context, blockID string, children []Block) (*ChildrenResponse, error)
	DeleteBlock(ctx context.Context, blockID string) error
	UploadFile(ctx context.Context, filename, contentType string, data []byte) (string, error)
	Download(ctx context.Context, url string) ([]byte, error)
}

// User is the result of users/me, including the workspace file ceiling.
type User struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	WorkspaceName string `json:"workspace_name"`
	MaxFileUpload int64  `json:"max_file_upload_size_in_bytes"`
	BotType       string `json:"bot_type"`
}

// Option is a select/multi-select option.
type Option struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
}

// DateValue is a Notion date property.
type DateValue struct {
	Start string `json:"start"`
	End   string `json:"end,omitempty"`
}

// PropertyValue is the write/read shape of one page property.
type PropertyValue struct {
	Type        string     `json:"-"`
	Title       []RichText `json:"-"`
	RichText    []RichText `json:"-"`
	Date        *DateValue `json:"-"`
	Number      *float64   `json:"-"`
	Select      *Option    `json:"-"`
	MultiSelect []Option   `json:"-"`
}

// MarshalJSON emits the Notion property shape selected by Type.
func (p PropertyValue) MarshalJSON() ([]byte, error) {
	switch p.Type {
	case "title":
		return json.Marshal(map[string]any{"type": p.Type, "title": p.Title})
	case "rich_text":
		return json.Marshal(map[string]any{"type": p.Type, "rich_text": p.RichText})
	case "date":
		return json.Marshal(map[string]any{"type": p.Type, "date": p.Date})
	case "number":
		return json.Marshal(map[string]any{"type": p.Type, "number": p.Number})
	case "select":
		return json.Marshal(map[string]any{"type": p.Type, "select": p.Select})
	case "multi_select":
		return json.Marshal(map[string]any{"type": p.Type, "multi_select": p.MultiSelect})
	default:
		return nil, fmt.Errorf("notion: unsupported property type %q", p.Type)
	}
}

// UnmarshalJSON reads any Notion property for the supported types.
func (p *PropertyValue) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type        string     `json:"type"`
		Title       []RichText `json:"title"`
		RichText    []RichText `json:"rich_text"`
		Date        *DateValue `json:"date"`
		Number      *float64   `json:"number"`
		Select      *Option    `json:"select"`
		MultiSelect []Option   `json:"multi_select"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	p.Type = raw.Type
	p.Title = raw.Title
	p.RichText = raw.RichText
	p.Date = raw.Date
	p.Number = raw.Number
	p.Select = raw.Select
	p.MultiSelect = raw.MultiSelect
	return nil
}

// PlainText returns the property's text content.
func (p PropertyValue) PlainText() string {
	texts := p.Title
	if len(texts) == 0 {
		texts = p.RichText
	}
	out := ""
	for _, t := range texts {
		if t.PlainText != "" {
			out += t.PlainText
			continue
		}
		if t.Text != nil {
			out += t.Text.Content
		}
	}
	return out
}

// Parent identifies where a page lives. A page whose parent is a data source
// is a database row, not a valid parent for the archive database.
type Parent struct {
	Type         string `json:"type"`
	PageID       string `json:"page_id,omitempty"`
	DatabaseID   string `json:"database_id,omitempty"`
	DataSourceID string `json:"data_source_id,omitempty"`
}

// Page is a Notion row (one memo).
type Page struct {
	ID             string                   `json:"id"`
	CreatedTime    time.Time                `json:"created_time"`
	LastEditedTime time.Time                `json:"last_edited_time"`
	InTrash        bool                     `json:"in_trash"`
	Archived       bool                     `json:"archived"`
	URL            string                   `json:"url"`
	Parent         Parent                   `json:"parent"`
	Properties     map[string]PropertyValue `json:"properties"`
}

// ParentPageSearch is the result of looking for archive locations.
// Notion only exposes objects that were directly shared with the integration.
// DataSources are the preferred archive target; pages are only used to create
// a database when no shared database exists.
type ParentPageSearch struct {
	Pages       []Page
	DataSources []DataSource
}

// DataSource is the table that owns a memo database's schema.
type DataSource struct {
	ID            string                    `json:"id"`
	DatabaseID    string                    `json:"database_id"`
	URL           string                    `json:"url,omitempty"`
	Title         string                    `json:"title"`
	Properties    map[string]PropertySchema `json:"properties"`
	PropertyTypes map[string]string         `json:"-"`
	Options       map[string][]Option       `json:"-"`
}

// UnmarshalJSON normalizes the title array and property types.
func (d *DataSource) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID     string     `json:"id"`
		URL    string     `json:"url"`
		Title  []RichText `json:"title"`
		Parent struct {
			DatabaseID string `json:"database_id"`
		} `json:"parent"`
		Properties map[string]struct {
			Type        string `json:"type"`
			MultiSelect struct {
				Options []Option `json:"options"`
			} `json:"multi_select"`
			Select struct {
				Options []Option `json:"options"`
			} `json:"select"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	d.ID = raw.ID
	d.DatabaseID = raw.Parent.DatabaseID
	d.URL = raw.URL
	for _, t := range raw.Title {
		d.Title += t.PlainText
	}
	d.PropertyTypes = map[string]string{}
	d.Options = map[string][]Option{}
	for name, p := range raw.Properties {
		d.PropertyTypes[name] = p.Type
		if len(p.MultiSelect.Options) > 0 {
			d.Options[name] = p.MultiSelect.Options
		}
		if len(p.Select.Options) > 0 {
			d.Options[name] = p.Select.Options
		}
	}
	return nil
}

// TitleProperty returns the discovered title property name. Notion lets the
// user rename it (the probe database used 名称), so it is never hard-coded.
func (d DataSource) TitleProperty() string {
	for name, t := range d.PropertyTypes {
		if t == "title" {
			return name
		}
	}
	return ""
}

// PropertySchema describes one app-created property.
type PropertySchema struct {
	Title       *struct{}          `json:"title,omitempty"`
	RichText    *struct{}          `json:"rich_text,omitempty"`
	Date        *struct{}          `json:"date,omitempty"`
	Number      *NumberSchema      `json:"number,omitempty"`
	Select      *SelectSchema      `json:"select,omitempty"`
	MultiSelect *MultiSelectSchema `json:"multi_select,omitempty"`
}

// NumberSchema selects a Notion number format.
type NumberSchema struct {
	Format string `json:"format,omitempty"`
}

// SelectSchema carries select options.
type SelectSchema struct {
	Options []Option `json:"options,omitempty"`
}

// MultiSelectSchema carries multi-select options.
type MultiSelectSchema struct {
	Options []Option `json:"options,omitempty"`
}

// RichText is one Notion rich-text object.
type RichText struct {
	Type        string       `json:"type"`
	Text        *TextContent `json:"text,omitempty"`
	Annotations *Annotations `json:"annotations,omitempty"`
	PlainText   string       `json:"plain_text,omitempty"`
	Href        string       `json:"href,omitempty"`
}

// TextContent is the text payload with an optional link.
type TextContent struct {
	Content string `json:"content"`
	Link    *Link  `json:"link,omitempty"`
}

// Link is a rich-text link target.
type Link struct {
	URL string `json:"url"`
}

// Annotations are inline marks.
type Annotations struct {
	Bold          bool `json:"bold"`
	Italic        bool `json:"italic"`
	Strikethrough bool `json:"strikethrough"`
	Underline     bool `json:"underline"`
	Code          bool `json:"code"`
}

// Block is a Notion block, read or write shape.
type Block struct {
	Object      string `json:"object,omitempty"`
	ID          string `json:"id,omitempty"`
	Type        string `json:"type"`
	HasChildren bool   `json:"has_children,omitempty"`

	Paragraph        *TextBlock `json:"paragraph,omitempty"`
	Heading1         *TextBlock `json:"heading_1,omitempty"`
	Heading2         *TextBlock `json:"heading_2,omitempty"`
	Heading3         *TextBlock `json:"heading_3,omitempty"`
	BulletedListItem *TextBlock `json:"bulleted_list_item,omitempty"`
	NumberedListItem *TextBlock `json:"numbered_list_item,omitempty"`
	ToDo             *TodoBlock `json:"to_do,omitempty"`
	Quote            *TextBlock `json:"quote,omitempty"`
	Code             *CodeBlock `json:"code,omitempty"`
	Divider          *struct{}  `json:"divider,omitempty"`
	Image            *FileBlock `json:"image,omitempty"`
	File             *FileBlock `json:"file,omitempty"`

	Children []Block `json:"children,omitempty"`

	// Raw carries an untouched block payload captured from Notion. It is used
	// only to restore blocks that the typed model cannot represent.
	Raw json.RawMessage `json:"-"`
}

// MarshalJSON returns the captured payload verbatim when present, so a block
// read from Notion can be written back without losing its shape.
func (b *Block) UnmarshalJSON(data []byte) error {
	type plain Block
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*b = Block(decoded)
	b.Raw = append(json.RawMessage(nil), data...)
	return nil
}

func (b Block) MarshalJSON() ([]byte, error) {
	if len(b.Raw) > 0 {
		return b.Raw, nil
	}
	type plain Block
	return json.Marshal(plain(b))
}

// TextBlock is the shared rich-text block payload.
type TextBlock struct {
	RichText []RichText `json:"rich_text"`
}

// TodoBlock adds the checkbox.
type TodoBlock struct {
	RichText []RichText `json:"rich_text"`
	Checked  bool       `json:"checked"`
}

// CodeBlock adds a language.
type CodeBlock struct {
	RichText []RichText `json:"rich_text"`
	Language string     `json:"language"`
}

// FileBlock is an image/file block. On write, FileUpload references an upload;
// on read, File carries a signed URL.
type FileBlock struct {
	Type       string         `json:"type"`
	File       *FileObject    `json:"file,omitempty"`
	External   *FileObject    `json:"external,omitempty"`
	FileUpload *FileUploadRef `json:"file_upload,omitempty"`
	Caption    []RichText     `json:"caption,omitempty"`
}

// FileObject holds a URL and optional expiry.
type FileObject struct {
	URL        string     `json:"url"`
	ExpiryTime *time.Time `json:"expiry_time,omitempty"`
}

// FileUploadRef references a pending Notion file upload.
type FileUploadRef struct {
	ID string `json:"id"`
}

// ChildrenResponse is one page of block children.
type ChildrenResponse struct {
	Results    []Block `json:"results"`
	HasMore    bool    `json:"has_more"`
	NextCursor string  `json:"next_cursor"`
}

// QueryRequest is a data-source query body.
type QueryRequest struct {
	Filter      map[string]any `json:"filter,omitempty"`
	Sorts       []Sort         `json:"sorts,omitempty"`
	PageSize    int            `json:"page_size,omitempty"`
	StartCursor string         `json:"start_cursor,omitempty"`
}

// Sort orders a query by a property.
type Sort struct {
	Property  string `json:"property,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Direction string `json:"direction"`
}

// QueryResponse is one page of data-source rows.
type QueryResponse struct {
	Results       []Page        `json:"results"`
	HasMore       bool          `json:"has_more"`
	NextCursor    string        `json:"next_cursor"`
	RequestStatus RequestStatus `json:"request_status"`
}

// RequestStatus reports truncation; has_more=false is not enough at 10k rows.
type RequestStatus struct {
	Type             string `json:"type"`
	IncompleteReason string `json:"incomplete_reason"`
}

// Incomplete reports whether the query hit the result cap.
func (r QueryResponse) Incomplete() bool {
	return r.RequestStatus.Type == "incomplete" && r.RequestStatus.IncompleteReason == "query_result_limit_reached"
}

// APIError is a parsed Notion error response, including 503 commit metadata.
type APIError struct {
	Status              int
	Code                string
	Message             string
	RetryAfter          time.Duration
	CommittedResourceID string
	CommittedChildIDs   []string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("notion: %d %s: %s", e.Status, e.Code, e.Message)
}

// Retryable reports whether the error is a transient rate/service error.
func (e *APIError) Retryable() bool {
	return e != nil && (e.Status == 429 || e.Status == 529 || e.Status >= 500)
}

// IsRateLimited reports a 429/529.
func (e *APIError) IsRateLimited() bool {
	return e != nil && (e.Status == 429 || e.Status == 529)
}

// CommitLanded reports a 503 that actually applied the write.
func (e *APIError) CommitLanded() bool {
	return e != nil && e.Status == 503 && e.CommittedResourceID != ""
}

// StatusError builds an API error with no transport metadata.
func StatusError(status int, code, message string) *APIError {
	return &APIError{Status: status, Code: code, Message: message}
}
