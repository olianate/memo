package notion

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/olianate/memo/internal/domain"
)

// TitlePropertyName is the app-created title property. The name is stored after
// onboarding and discovered by type on read; this constant is only the default.
const TitlePropertyName = "名称"

// AppSchema returns the full app-owned schema including the title property.
func AppSchema(titleProperty string) map[string]PropertySchema {
	if titleProperty == "" {
		titleProperty = TitlePropertyName
	}
	return map[string]PropertySchema{
		titleProperty:     {Title: &struct{}{}},
		PropMemoID:        {RichText: &struct{}{}},
		PropCreated:       {Date: &struct{}{}},
		PropUpdated:       {Date: &struct{}{}},
		PropRevision:      {Number: &NumberSchema{Format: "number"}},
		PropDevice:        {RichText: &struct{}{}},
		PropTags:          {MultiSelect: &MultiSelectSchema{Options: []Option{}}},
		PropStatus:        {Select: &SelectSchema{Options: []Option{{Name: StatusActive}, {Name: StatusDeleted}}}},
		PropSchemaVersion: {Number: &NumberSchema{Format: "number"}},
	}
}

// SchemaConflict is a same-named property with an incompatible type.
type SchemaConflict struct {
	Name         string
	ExistingType string
	RequiredType string
}

// SchemaPlan describes the additive changes needed by an existing database.
// Missing properties are safe to add; conflicts must block the connection
// because changing an existing property would corrupt data.
type SchemaPlan struct {
	Missing   map[string]PropertySchema
	Conflicts []SchemaConflict
}

// PlanSchema compares the app schema with an existing data source schema.
func PlanSchema(existing map[string]string, required map[string]PropertySchema) SchemaPlan {
	plan := SchemaPlan{Missing: map[string]PropertySchema{}}
	names := make([]string, 0, len(required))
	for name := range required {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		schema := required[name]
		want := schemaType(schema)
		if want == "" {
			continue
		}
		have, ok := existing[name]
		if !ok {
			plan.Missing[name] = schema
			continue
		}
		if !strings.EqualFold(have, want) {
			plan.Conflicts = append(plan.Conflicts, SchemaConflict{
				Name: name, ExistingType: have, RequiredType: want,
			})
		}
	}
	return plan
}

// MissingSchema returns the required properties that an existing data source
// does not have yet. It refuses to touch a same-named property with a
// different type, because that would silently corrupt sync writes.
func MissingSchema(existing map[string]string, required map[string]PropertySchema) (map[string]PropertySchema, error) {
	plan := PlanSchema(existing, required)
	if len(plan.Conflicts) > 0 {
		c := plan.Conflicts[0]
		return nil, fmt.Errorf("数据库中的属性「%s」类型是 %s，memo 需要 %s；请改名或调整该属性", c.Name, c.ExistingType, c.RequiredType)
	}
	return plan.Missing, nil
}

// PropertyTypeName returns the Notion type name for a property schema.
func PropertyTypeName(schema PropertySchema) string { return schemaType(schema) }

func schemaType(schema PropertySchema) string {
	switch {
	case schema.Title != nil:
		return "title"
	case schema.RichText != nil:
		return "rich_text"
	case schema.Date != nil:
		return "date"
	case schema.Number != nil:
		return "number"
	case schema.Select != nil:
		return "select"
	case schema.MultiSelect != nil:
		return "multi_select"
	default:
		return ""
	}
}

// PropertiesForMemo projects a memo into Notion page properties.
func PropertiesForMemo(memo domain.Memo, deviceID, titleProperty string) map[string]PropertyValue {
	if titleProperty == "" {
		titleProperty = TitlePropertyName
	}
	created := memo.CreatedAt.UTC().Format(time.RFC3339)
	updated := memo.UpdatedAt.UTC().Format(time.RFC3339)
	status := StatusActive
	if memo.DeletedAt != nil {
		status = StatusDeleted
	}
	revision := float64(memo.Revision)
	schemaVersion := float64(SchemaVersion)
	options := make([]Option, 0, len(memo.Tags))
	for _, tag := range memo.Tags {
		options = append(options, Option{Name: tag.Name})
	}
	title := domain.Snippet(memo.BodyText, 80)
	if title == "" && len(memo.Attachments) > 0 {
		title = memo.Attachments[0].OriginalName
	}
	if title == "" {
		title = "memo"
	}
	return map[string]PropertyValue{
		titleProperty:     {Type: "title", Title: plainRichText(title)},
		PropMemoID:        {Type: "rich_text", RichText: plainRichText(memo.ID)},
		PropCreated:       {Type: "date", Date: &DateValue{Start: created}},
		PropUpdated:       {Type: "date", Date: &DateValue{Start: updated}},
		PropRevision:      {Type: "number", Number: &revision},
		PropDevice:        {Type: "rich_text", RichText: plainRichText(deviceID)},
		PropTags:          {Type: "multi_select", MultiSelect: options},
		PropStatus:        {Type: "select", Select: &Option{Name: status}},
		PropSchemaVersion: {Type: "number", Number: &schemaVersion},
	}
}

func plainRichText(text string) []RichText {
	if text == "" {
		return []RichText{}
	}
	return []RichText{{Type: "text", Text: &TextContent{Content: truncateRunes(text, 2000)}}}
}

// Page helpers.

// MemoIDFromPage reads the stable local UUID from the Memo ID property.
func MemoIDFromPage(p Page) string {
	if v, ok := p.Properties[PropMemoID]; ok {
		return strings.TrimSpace(v.PlainText())
	}
	return ""
}

// TitleFromPage reads the discovered title text.
func TitleFromPage(p Page) string {
	for _, v := range p.Properties {
		if v.Type == "title" {
			return v.PlainText()
		}
	}
	return ""
}

// RevisionFromPage reads the client revision.
func RevisionFromPage(p Page) int64 {
	if v, ok := p.Properties[PropRevision]; ok && v.Number != nil {
		return int64(*v.Number)
	}
	return 0
}

// UpdatedAtFromPage prefers the client Updated date; last_edited_time does not
// move on child-block edits, so it is only a fallback.
func UpdatedAtFromPage(p Page) time.Time {
	if v, ok := p.Properties[PropUpdated]; ok && v.Date != nil {
		if t, err := time.Parse(time.RFC3339, v.Date.Start); err == nil {
			return t
		}
	}
	return p.LastEditedTime
}

// CreatedAtFromPage prefers the client Created date.
func CreatedAtFromPage(p Page) time.Time {
	if v, ok := p.Properties[PropCreated]; ok && v.Date != nil {
		if t, err := time.Parse(time.RFC3339, v.Date.Start); err == nil {
			return t
		}
	}
	return p.CreatedTime
}

// DeviceFromPage reads the last writer.
func DeviceFromPage(p Page) string {
	if v, ok := p.Properties[PropDevice]; ok {
		return v.PlainText()
	}
	return ""
}

// TagsFromPage reads multi-select tag names.
func TagsFromPage(p Page) []string {
	if v, ok := p.Properties[PropTags]; ok {
		out := make([]string, 0, len(v.MultiSelect))
		for _, opt := range v.MultiSelect {
			out = append(out, opt.Name)
		}
		return out
	}
	return nil
}

// DeletedFromPage reports the tombstone status.
func DeletedFromPage(p Page) bool {
	if v, ok := p.Properties[PropStatus]; ok && v.Select != nil {
		return v.Select.Name == StatusDeleted
	}
	return false
}

// SpansToRichText maps inline marks to Notion rich text, splitting at 2,000
// characters per object.
func SpansToRichText(spans []domain.Span) []RichText {
	out := []RichText{}
	for _, span := range spans {
		text := span.Text
		for text != "" {
			chunk := truncateRunes(text, 2000)
			text = strings.TrimPrefix(text, chunk)
			rt := RichText{
				Type: "text",
				Text: &TextContent{Content: chunk},
				Annotations: &Annotations{
					Bold: span.Bold, Italic: span.Italic, Strikethrough: span.Strike, Code: span.Code,
				},
			}
			if span.Link != "" {
				rt.Text.Link = &Link{URL: span.Link}
				rt.Href = span.Link
			}
			out = append(out, rt)
		}
	}
	return out
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

// RichTextToSpans flattens Notion rich text into inline spans.
func RichTextToSpans(texts []RichText) []domain.Span {
	out := make([]domain.Span, 0, len(texts))
	for _, rt := range texts {
		text := rt.PlainText
		if text == "" && rt.Text != nil {
			text = rt.Text.Content
		}
		if text == "" {
			continue
		}
		span := domain.Span{Text: text}
		if rt.Annotations != nil {
			span.Bold = rt.Annotations.Bold
			span.Italic = rt.Annotations.Italic
			span.Strike = rt.Annotations.Strikethrough
			span.Code = rt.Annotations.Code
		}
		if rt.Href != "" {
			span.Link = rt.Href
		}
		out = append(out, span)
	}
	// Merge adjacent spans with identical marks.
	merged := make([]domain.Span, 0, len(out))
	for _, span := range out {
		if len(merged) > 0 {
			last := &merged[len(merged)-1]
			if last.Bold == span.Bold && last.Italic == span.Italic && last.Strike == span.Strike && last.Code == span.Code && last.Link == span.Link {
				last.Text += span.Text
				continue
			}
		}
		merged = append(merged, span)
	}
	return merged
}

// ImportedAttachment is an attachment discovered while reading blocks.
type ImportedAttachment struct {
	BlockID string
	Kind    domain.AttachmentKind
	Name    string
	URL     string
}

const notionFenceLanguage = "notion"

// creatableRawTypes are Notion block types that can be sent back through the
// append API. Read-only or container-only types stay as code fences instead.
var creatableRawTypes = map[string]bool{
	"paragraph":          true,
	"heading_1":          true,
	"heading_2":          true,
	"heading_3":          true,
	"bulleted_list_item": true,
	"numbered_list_item": true,
	"to_do":              true,
	"quote":              true,
	"code":               true,
	"divider":            true,
	"callout":            true,
	"toggle":             true,
	"embed":              true,
	"bookmark":           true,
	"equation":           true,
}

// restoreNotionFence rebuilds a block captured as raw JSON by the pull path.
// Only appendable types without children are restored; everything else falls
// back to a code block so the source is never lost.
func restoreNotionFence(source string) (Block, bool) {
	raw := strings.TrimSpace(source)
	if raw == "" {
		return Block{}, false
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return Block{}, false
	}
	var blockType string
	if err := json.Unmarshal(payload["type"], &blockType); err != nil || !creatableRawTypes[blockType] {
		return Block{}, false
	}
	body, ok := payload[blockType]
	if !ok || len(body) == 0 {
		return Block{}, false
	}
	var object map[string]any
	if err := json.Unmarshal(body, &object); err != nil || object == nil {
		return Block{}, false
	}
	if children, ok := payload["children"]; ok {
		var list []json.RawMessage
		if err := json.Unmarshal(children, &list); err != nil || len(list) > 0 {
			return Block{}, false
		}
	}
	typeJSON, _ := json.Marshal(blockType)
	clean, err := json.Marshal(map[string]json.RawMessage{
		"object":  json.RawMessage(`"block"`),
		"type":    typeJSON,
		blockType: body,
	})
	if err != nil {
		return Block{}, false
	}
	return Block{Object: "block", Type: blockType, Raw: clean}, true
}
func BlocksToNotion(blocks []domain.Block, uploads map[string]string) ([]Block, error) {
	out := make([]Block, 0, len(blocks))
	for _, b := range blocks {
		if b.Type == domain.BlockCode && strings.EqualFold(strings.TrimSpace(b.Language), notionFenceLanguage) {
			if restored, ok := restoreNotionFence(b.PlainText()); ok {
				out = append(out, restored)
				continue
			}
		}
		switch b.Type {
		case domain.BlockParagraph:
			out = append(out, Block{Object: "block", Type: "paragraph", Paragraph: &TextBlock{RichText: SpansToRichText(b.Spans)}})
		case domain.BlockHeading:
			level := b.Level
			if level < 1 {
				level = 1
			}
			if level > 3 {
				level = 3
			}
			tb := &TextBlock{RichText: SpansToRichText(b.Spans)}
			switch level {
			case 1:
				out = append(out, Block{Object: "block", Type: "heading_1", Heading1: tb})
			case 2:
				out = append(out, Block{Object: "block", Type: "heading_2", Heading2: tb})
			case 3:
				out = append(out, Block{Object: "block", Type: "heading_3", Heading3: tb})
			}
		case domain.BlockBullet:
			out = append(out, Block{Object: "block", Type: "bulleted_list_item", BulletedListItem: &TextBlock{RichText: SpansToRichText(b.Spans)}})
		case domain.BlockOrdered:
			out = append(out, Block{Object: "block", Type: "numbered_list_item", NumberedListItem: &TextBlock{RichText: SpansToRichText(b.Spans)}})
		case domain.BlockTodo:
			out = append(out, Block{Object: "block", Type: "to_do", ToDo: &TodoBlock{RichText: SpansToRichText(b.Spans), Checked: b.Checked}})
		case domain.BlockQuote:
			out = append(out, Block{Object: "block", Type: "quote", Quote: &TextBlock{RichText: SpansToRichText(b.Spans)}})
		case domain.BlockCode:
			lang := notionLanguage(b.Language)
			out = append(out, Block{Object: "block", Type: "code", Code: &CodeBlock{RichText: SpansToRichText(b.Spans), Language: lang}})
		case domain.BlockDivider:
			out = append(out, Block{Object: "block", Type: "divider", Divider: &struct{}{}})
		case domain.BlockImage, domain.BlockFile:
			uploadID, ok := uploads[b.AttachmentID]
			if !ok || uploadID == "" {
				return nil, fmt.Errorf("notion: attachment %s has no upload", b.AttachmentID)
			}
			fb := &FileBlock{Type: "file_upload", FileUpload: &FileUploadRef{ID: uploadID}}
			if b.Alt != "" {
				fb.Caption = plainRichText(b.Alt)
			}
			if b.Type == domain.BlockImage {
				out = append(out, Block{Object: "block", Type: "image", Image: fb})
			} else {
				out = append(out, Block{Object: "block", Type: "file", File: fb})
			}
		default:
			return nil, fmt.Errorf("notion: unsupported block type %q", b.Type)
		}
	}
	return out, nil
}

// BlocksFromNotion maps page children to the AST and reports attachments to
// hydrate. Unsupported block types keep their text as a paragraph; blocks with
// no Markdown form become a `notion` fenced code block holding the original
// payload, so no block is ever dropped.
func BlocksFromNotion(blocks []Block) ([]domain.Block, []ImportedAttachment) {
	var out []domain.Block
	var atts []ImportedAttachment
	for _, b := range blocks {
		switch b.Type {
		case "paragraph":
			out = append(out, textDomainBlock(domain.BlockParagraph, b.Paragraph))
		case "heading_1", "heading_2", "heading_3":
			level := 1
			if b.Type == "heading_2" {
				level = 2
			} else if b.Type == "heading_3" {
				level = 3
			}
			block := textDomainBlock(domain.BlockHeading, headingText(b))
			block.Level = level
			out = append(out, block)
		case "bulleted_list_item":
			out = append(out, textDomainBlock(domain.BlockBullet, b.BulletedListItem))
		case "numbered_list_item":
			out = append(out, textDomainBlock(domain.BlockOrdered, b.NumberedListItem))
		case "to_do":
			block := domain.Block{ID: domain.NewID(), Type: domain.BlockTodo}
			if b.ToDo != nil {
				block.Checked = b.ToDo.Checked
				block.Spans = RichTextToSpans(b.ToDo.RichText)
			}
			out = append(out, block)
		case "quote":
			out = append(out, textDomainBlock(domain.BlockQuote, b.Quote))
		case "code":
			block := domain.Block{ID: domain.NewID(), Type: domain.BlockCode}
			if b.Code != nil {
				block.Language = b.Code.Language
				block.Spans = RichTextToSpans(b.Code.RichText)
			}
			out = append(out, block)
		case "divider":
			out = append(out, domain.Block{ID: domain.NewID(), Type: domain.BlockDivider})
		case "image", "file":
			fb := b.Image
			kind := domain.AttachmentImage
			if b.Type == "file" {
				fb = b.File
				kind = domain.AttachmentFile
			}
			if fb == nil {
				out = append(out, unsupportedNotionBlockOnly(b))
				continue
			}
			// External file objects are not app-created attachments. Never
			// download them: a shared page could point at localhost or an
			// internal service (SSRF). Our own writes always use Notion-hosted
			// `file` objects. Keep the original block visible instead of
			// dropping it.
			url := ""
			if fb.File != nil {
				url = fb.File.URL
			}
			if url == "" {
				out = append(out, unsupportedNotionBlockOnly(b))
				continue
			}
			name := captionText(fb.Caption)
			block := domain.Block{ID: domain.NewID(), Type: domain.BlockImage, Alt: name}
			if kind == domain.AttachmentFile {
				block = domain.Block{ID: domain.NewID(), Type: domain.BlockFile, Alt: name}
			}
			out = append(out, block)
			atts = append(atts, ImportedAttachment{BlockID: b.ID, Kind: kind, Name: name, URL: url})
		default:
			out = append(out, unsupportedNotionBlockOnly(b))
		}
		// Nested children are flattened after the parent. Children come from
		// separate list-children calls, not from the raw block payload, so even
		// an unsupported block's children must be kept.
		if len(b.Children) > 0 {
			childBlocks, childAtts := BlocksFromNotion(b.Children)
			out = append(out, childBlocks...)
			atts = append(atts, childAtts...)
		}
	}
	return out, atts
}

func headingText(b Block) *TextBlock {
	if b.Heading1 != nil {
		return b.Heading1
	}
	if b.Heading2 != nil {
		return b.Heading2
	}
	return b.Heading3
}

func textDomainBlock(t domain.BlockType, tb *TextBlock) domain.Block {
	block := domain.Block{ID: domain.NewID(), Type: t}
	if tb != nil {
		block.Spans = RichTextToSpans(tb.RichText)
	}
	return block
}

func captionText(caption []RichText) string {
	var sb strings.Builder
	for _, c := range caption {
		sb.WriteString(c.PlainText)
	}
	return sb.String()
}

func unsupportedNotionBlock(b Block) (domain.Block, bool) {
	raw := b.Raw
	if len(raw) == 0 {
		raw, _ = json.Marshal(b)
	}
	body, blockType := rawBlockBody(b, raw)
	if spans := unsupportedTextSpans(body); len(spans) > 0 {
		return domain.Block{ID: domain.NewID(), Type: domain.BlockParagraph, Spans: spans}, false
	}
	payload := strings.TrimSpace(string(raw))
	if payload == "" {
		payload = fmt.Sprintf(`{"type":%q}`, blockType)
	}
	return domain.Block{
		ID:       domain.NewID(),
		Type:     domain.BlockCode,
		Language: notionFenceLanguage,
		Spans:    []domain.Span{{Text: payload}},
	}, true
}

func unsupportedNotionBlockOnly(b Block) domain.Block {
	block, _ := unsupportedNotionBlock(b)
	return block
}

func rawBlockBody(b Block, raw json.RawMessage) (map[string]json.RawMessage, string) {
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(raw, &outer); err != nil {
		return nil, b.Type
	}
	blockType := b.Type
	if blockType == "" {
		_ = json.Unmarshal(outer["type"], &blockType)
	}
	var body map[string]json.RawMessage
	if payload, ok := outer[blockType]; ok {
		_ = json.Unmarshal(payload, &body)
	}
	if body == nil {
		body = outer
	}
	return body, blockType
}

// unsupportedTextSpans pulls whatever human-readable text an unknown Notion
// block carries. Rich text keeps its inline marks; table cells, URLs, titles
// and equations degrade to plain spans.
func unsupportedTextSpans(body map[string]json.RawMessage) []domain.Span {
	if rawText, ok := body["rich_text"]; ok {
		var rich []RichText
		if json.Unmarshal(rawText, &rich) == nil {
			if spans := RichTextToSpans(rich); len(spans) > 0 {
				return spans
			}
		}
	}
	if rawCells, ok := body["cells"]; ok {
		var cells [][]RichText
		if json.Unmarshal(rawCells, &cells) == nil {
			parts := make([]string, 0, len(cells))
			for _, cell := range cells {
				parts = append(parts, captionText(cell))
			}
			if text := strings.TrimSpace(strings.Join(parts, " | ")); text != "" {
				return []domain.Span{{Text: text}}
			}
		}
	}
	for _, key := range []string{"url", "title", "expression"} {
		rawValue, ok := body[key]
		if !ok {
			continue
		}
		var text string
		if json.Unmarshal(rawValue, &text) == nil && strings.TrimSpace(text) != "" {
			return []domain.Span{{Text: text, Code: key == "expression"}}
		}
	}
	return nil
}

func notionLanguage(lang string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	switch l {
	case "", "text", "plain", "plain text":
		return "plain text"
	case "js":
		return "javascript"
	case "ts":
		return "typescript"
	case "sh", "bash":
		return "shell"
	case "py":
		return "python"
	case "yml":
		return "yaml"
	case "go", "rust", "java", "c", "c++", "cpp", "c#", "csharp", "json", "sql",
		"html", "css", "xml", "markdown", "md", "ruby", "rb", "php", "swift",
		"kotlin", "scala", "r", "lua", "dart", "elixir", "erlang", "haskell",
		"clojure", "groovy", "perl", "powershell", "ps1", "toml", "diff",
		"dockerfile", "graphql", "makefile", "latex", "tex":
		return l
	default:
		return "plain text"
	}
}
