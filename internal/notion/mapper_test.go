package notion

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"memo/internal/domain"
)

func spans(text string, marks ...string) []domain.Span {
	span := domain.Span{Text: text}
	for _, mark := range marks {
		switch mark {
		case "bold":
			span.Bold = true
		case "italic":
			span.Italic = true
		case "strike":
			span.Strike = true
		case "code":
			span.Code = true
		}
	}
	return []domain.Span{span}
}

func TestBlockMappingRoundTrip(t *testing.T) {
	blocks := []domain.Block{
		{ID: "1", Type: domain.BlockHeading, Level: 2, Spans: spans("heading")},
		{ID: "2", Type: domain.BlockParagraph, Spans: []domain.Span{{Text: "bold", Bold: true}, {Text: " and "}, {Text: "italic", Italic: true}}},
		{ID: "3", Type: domain.BlockBullet, Spans: spans("bullet")},
		{ID: "4", Type: domain.BlockOrdered, Spans: spans("ordered")},
		{ID: "5", Type: domain.BlockTodo, Checked: true, Spans: spans("todo")},
		{ID: "6", Type: domain.BlockQuote, Spans: spans("quote")},
		{ID: "7", Type: domain.BlockCode, Language: "go", Spans: spans("fmt.Println()")},
		{ID: "8", Type: domain.BlockDivider},
	}
	mapped, err := BlocksToNotion(blocks, nil)
	if err != nil {
		t.Fatal(err)
	}
	back, atts := BlocksFromNotion(mapped)
	if len(atts) != 0 {
		t.Fatalf("unexpected attachments: %+v", atts)
	}
	if len(back) != len(blocks) {
		t.Fatalf("blocks = %d, want %d", len(back), len(blocks))
	}
	for i := range blocks {
		if back[i].Type != blocks[i].Type {
			t.Errorf("block %d type = %q, want %q", i, back[i].Type, blocks[i].Type)
		}
		if back[i].PlainText() != blocks[i].PlainText() {
			t.Errorf("block %d text = %q, want %q", i, back[i].PlainText(), blocks[i].PlainText())
		}
		if back[i].Checked != blocks[i].Checked || back[i].Level != blocks[i].Level {
			t.Errorf("block %d metadata = %+v, want %+v", i, back[i], blocks[i])
		}
	}
	if !back[1].Spans[0].Bold || back[1].Spans[2].Italic == false {
		t.Fatalf("inline marks lost: %+v", back[1].Spans)
	}
	if back[6].Language != "go" {
		t.Fatalf("language = %q", back[6].Language)
	}
}

func TestLinkAndStrikeMapping(t *testing.T) {
	blocks := []domain.Block{{ID: "1", Type: domain.BlockParagraph, Spans: []domain.Span{
		{Text: "link", Link: "https://example.com"},
		{Text: " and "},
		{Text: "gone", Strike: true},
		{Text: " and "},
		{Text: "code", Code: true},
	}}}
	mapped, err := BlocksToNotion(blocks, nil)
	if err != nil {
		t.Fatal(err)
	}
	back, _ := BlocksFromNotion(mapped)
	got := back[0].Spans
	if got[0].Link != "https://example.com" || !got[2].Strike || !got[4].Code {
		t.Fatalf("spans = %+v", got)
	}
}

func TestAttachmentMappingRequiresUpload(t *testing.T) {
	blocks := []domain.Block{
		{ID: "1", Type: domain.BlockImage, AttachmentID: "a1", Alt: "shot"},
		{ID: "2", Type: domain.BlockFile, AttachmentID: "a2", Alt: "report.pdf"},
	}
	if _, err := BlocksToNotion(blocks, nil); err == nil {
		t.Fatal("expected an error for a missing upload")
	}
	mapped, err := BlocksToNotion(blocks, map[string]string{"a1": "up1", "a2": "up2"})
	if err != nil {
		t.Fatal(err)
	}
	if mapped[0].Image.FileUpload.ID != "up1" || mapped[1].File.FileUpload.ID != "up2" {
		t.Fatalf("mapped = %+v", mapped)
	}
	read := []Block{
		{ID: "b1", Type: "image", Image: &FileBlock{Type: "file", File: &FileObject{URL: "https://signed/1"}, Caption: []RichText{{Type: "text", PlainText: "shot"}}}},
		{ID: "b2", Type: "file", File: &FileBlock{Type: "file", File: &FileObject{URL: "https://signed/2"}, Caption: []RichText{{Type: "text", PlainText: "report.pdf"}}}},
	}
	back, atts := BlocksFromNotion(read)
	if len(atts) != 2 || atts[0].URL != "https://signed/1" || atts[1].Name != "report.pdf" {
		t.Fatalf("atts = %+v", atts)
	}
	if back[0].Type != domain.BlockImage || back[1].Type != domain.BlockFile {
		t.Fatalf("blocks = %+v", back)
	}
}

func TestExternalFileObjectsAreNotImported(t *testing.T) {
	blocks := []Block{{ID: "b1", Type: "image", Image: &FileBlock{
		Type:     "external",
		External: &FileObject{URL: "http://127.0.0.1:8080/secret"},
	}}}
	back, atts := BlocksFromNotion(blocks)
	if len(atts) != 0 {
		t.Fatalf("external URL imported: %+v", atts)
	}
	if len(back) != 0 {
		t.Fatalf("external image became a block: %+v", back)
	}
}

func TestRichTextSplitAt2000(t *testing.T) {
	rt := SpansToRichText([]domain.Span{{Text: strings.Repeat("字", 2500), Bold: true}})
	if len(rt) != 2 {
		t.Fatalf("chunks = %d", len(rt))
	}
	if len([]rune(rt[0].Text.Content)) != 2000 || !rt[0].Annotations.Bold {
		t.Fatalf("first chunk = %+v", rt[0])
	}
	if len([]rune(rt[1].Text.Content)) != 500 {
		t.Fatalf("second chunk = %d", len([]rune(rt[1].Text.Content)))
	}
}

func TestPropertyJSONRoundTrip(t *testing.T) {
	value := PropertyValue{Type: "multi_select", MultiSelect: []Option{{Name: "work"}, {Name: "life"}}}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var parsed PropertyValue
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Type != "multi_select" || len(parsed.MultiSelect) != 2 {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestPagePropertyHelpers(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	page := Page{
		ID:             "page1",
		LastEditedTime: now.Add(-time.Hour),
		Properties: map[string]PropertyValue{
			"名称":         {Type: "title", Title: []RichText{{PlainText: "title"}}},
			PropMemoID:   {Type: "rich_text", RichText: []RichText{{PlainText: "memo-1"}}},
			PropRevision: {Type: "number", Number: floatPtr(7)},
			PropUpdated:  {Type: "date", Date: &DateValue{Start: now.Format(time.RFC3339)}},
			PropCreated:  {Type: "date", Date: &DateValue{Start: now.Add(-48 * time.Hour).Format(time.RFC3339)}},
			PropDevice:   {Type: "rich_text", RichText: []RichText{{PlainText: "dev-a"}}},
			PropTags:     {Type: "multi_select", MultiSelect: []Option{{Name: "work"}}},
			PropStatus:   {Type: "select", Select: &Option{Name: StatusDeleted}},
		},
	}
	if MemoIDFromPage(page) != "memo-1" || RevisionFromPage(page) != 7 || !DeletedFromPage(page) {
		t.Fatalf("page helpers failed")
	}
	if DeviceFromPage(page) != "dev-a" || len(TagsFromPage(page)) != 1 || TagsFromPage(page)[0] != "work" {
		t.Fatalf("tags/device failed")
	}
	if !UpdatedAtFromPage(page).Equal(now) || TitleFromPage(page) != "title" {
		t.Fatalf("title/updated failed")
	}
	memo := domain.Memo{ID: "m1", BodyText: "hello", Revision: 2, CreatedAt: now, UpdatedAt: now,
		Tags: []domain.Tag{{Name: "work"}}}
	props := PropertiesForMemo(memo, "dev-a", "名称")
	if props[PropMemoID].PlainText() != "m1" || props[PropTags].MultiSelect[0].Name != "work" {
		t.Fatalf("props = %+v", props)
	}
}

func floatPtr(v float64) *float64 { return &v }
