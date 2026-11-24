package notion

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/domain/markdown"
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
	if len(back) != 1 {
		t.Fatalf("external image dropped: %+v", back)
	}
	if back[0].Type != domain.BlockCode || back[0].Language != notionFenceLanguage {
		t.Fatalf("external image should stay visible as a notion fence: %+v", back[0])
	}
	if !strings.Contains(back[0].PlainText(), "127.0.0.1") {
		t.Fatalf("fence lost the original payload: %q", back[0].PlainText())
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
func TestNotionFenceRestoresRawBlock(t *testing.T) {
	raw := `{"object":"block","id":"abc","type":"callout","has_children":false,"callout":{"rich_text":[{"type":"text","text":{"content":"note"}}],"icon":{"type":"emoji","emoji":"💡"}}}`
	blocks := []domain.Block{{
		Type:     domain.BlockCode,
		Language: "notion",
		Spans:    []domain.Span{{Text: raw}},
	}}
	out, err := BlocksToNotion(blocks, nil)
	if err != nil {
		t.Fatalf("BlocksToNotion: %v", err)
	}
	if len(out) != 1 || out[0].Type != "callout" {
		t.Fatalf("out = %+v, want one callout", out)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(out[0].Raw, &payload); err != nil {
		t.Fatalf("raw payload: %v", err)
	}
	if _, ok := payload["id"]; ok {
		t.Error("restored payload kept the read-only id")
	}
	if _, ok := payload["has_children"]; ok {
		t.Error("restored payload kept has_children")
	}
	if payload["object"] != "block" || payload["type"] != "callout" {
		t.Errorf("payload = %+v", payload)
	}
	if _, ok := payload["callout"]; !ok {
		t.Error("restored payload lost the callout body")
	}
}

func TestNotionFenceFallsBackToCodeBlock(t *testing.T) {
	cases := []string{
		"not json",
		`{"type":"child_page","child_page":{"title":"x"}}`,
		`{"type":"paragraph","paragraph":{"rich_text":[]},"children":[{"type":"paragraph"}]}`,
	}
	for _, raw := range cases {
		blocks := []domain.Block{{
			Type:     domain.BlockCode,
			Language: "notion",
			Spans:    []domain.Span{{Text: raw}},
		}}
		out, err := BlocksToNotion(blocks, nil)
		if err != nil {
			t.Fatalf("BlocksToNotion(%q): %v", raw, err)
		}
		if len(out) != 1 || out[0].Type != "code" {
			t.Fatalf("out = %+v, want code fallback for %q", out, raw)
		}
		if out[0].Code == nil || out[0].Code.Language != "plain text" {
			t.Errorf("fallback language = %+v, want plain text", out[0].Code)
		}
		if len(out[0].Code.RichText) != 1 || out[0].Code.RichText[0].Text == nil || out[0].Code.RichText[0].Text.Content != raw {
			t.Errorf("fallback content lost for %q", raw)
		}
	}
}
func TestBlocksFromNotionKeepsUnsupportedBlocks(t *testing.T) {
	const src = `[
		{"object":"block","id":"c1","type":"callout","callout":{"rich_text":[{"type":"text","plain_text":"note","text":{"content":"note"}}],"icon":{"type":"emoji","emoji":"*"}}},
		{"object":"block","id":"k1","type":"table_row","table_row":{"cells":[[{"type":"text","plain_text":"a"}],[{"type":"text","plain_text":"b"}]]}},
		{"object":"block","id":"e1","type":"embed","embed":{"url":"https://example.com/embed"}},
		{"object":"block","id":"p1","type":"child_page","child_page":{"title":"Child page"}},
		{"object":"block","id":"l1","type":"column_list","column_list":{},"has_children":true}
	]`
	var blocks []Block
	if err := json.Unmarshal([]byte(src), &blocks); err != nil {
		t.Fatal(err)
	}
	if len(blocks[0].Raw) == 0 {
		t.Fatal("raw payload was not captured on read")
	}
	out, atts := BlocksFromNotion(blocks)
	if len(atts) != 0 {
		t.Fatalf("unexpected attachments: %+v", atts)
	}
	if len(out) != len(blocks) {
		t.Fatalf("blocks = %d, want %d (no block may be dropped): %+v", len(out), len(blocks), out)
	}
	if out[0].Type != domain.BlockParagraph || out[0].PlainText() != "note" {
		t.Errorf("callout = %+v, want paragraph note", out[0])
	}
	if out[1].Type != domain.BlockParagraph || out[1].PlainText() != "a | b" {
		t.Errorf("table row = %+v, want paragraph 'a | b'", out[1])
	}
	if out[2].Type != domain.BlockParagraph || out[2].PlainText() != "https://example.com/embed" {
		t.Errorf("embed = %+v, want its URL as a paragraph", out[2])
	}
	if out[3].Type != domain.BlockParagraph || out[3].PlainText() != "Child page" {
		t.Errorf("child page = %+v, want its title as a paragraph", out[3])
	}
	if out[4].Type != domain.BlockCode || out[4].Language != notionFenceLanguage {
		t.Fatalf("column list = %+v, want a notion fence", out[4])
	}
	if !strings.Contains(out[4].PlainText(), `"column_list"`) {
		t.Errorf("fence payload lost the original block: %q", out[4].PlainText())
	}
}

func TestNotionBlocksSerializeToMarkdown(t *testing.T) {
	blocks := []Block{
		{Type: "paragraph", Paragraph: &TextBlock{RichText: []RichText{
			{Type: "text", PlainText: "bold", Annotations: &Annotations{Bold: true}},
			{Type: "text", PlainText: " and "},
			{Type: "text", PlainText: "link", Href: "https://example.com"},
		}}},
		{Type: "heading_2", Heading2: &TextBlock{RichText: []RichText{{Type: "text", PlainText: "h2"}}}},
		{Type: "bulleted_list_item", BulletedListItem: &TextBlock{RichText: []RichText{{Type: "text", PlainText: "bullet"}}}},
		{Type: "to_do", ToDo: &TodoBlock{Checked: true, RichText: []RichText{{Type: "text", PlainText: "todo"}}}},
		{Type: "quote", Quote: &TextBlock{RichText: []RichText{{Type: "text", PlainText: "quote"}}}},
		{Type: "code", Code: &CodeBlock{Language: "go", RichText: []RichText{{Type: "text", PlainText: "fmt.Println()"}}}},
		{Type: "divider"},
	}
	out, _ := BlocksFromNotion(blocks)
	md := markdown.Serialize(out)
	for _, want := range []string{
		"**bold**", "[link](https://example.com)", "## h2", "- bullet",
		"- [x] todo", "> quote", "```go", "fmt.Println()", "---",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown %q is missing %q", md, want)
		}
	}
}

func TestBlocksFromNotionSupportedMatrix(t *testing.T) {
	const src = `[
		{"type":"paragraph","id":"1","paragraph":{"rich_text":[{"type":"text","plain_text":"para"}]}},
		{"type":"heading_1","id":"2","heading_1":{"rich_text":[{"type":"text","plain_text":"h1"}]}},
		{"type":"heading_2","id":"3","heading_2":{"rich_text":[{"type":"text","plain_text":"h2"}]}},
		{"type":"heading_3","id":"4","heading_3":{"rich_text":[{"type":"text","plain_text":"h3"}]}},
		{"type":"bulleted_list_item","id":"5","bulleted_list_item":{"rich_text":[{"type":"text","plain_text":"bullet"}]}},
		{"type":"numbered_list_item","id":"6","numbered_list_item":{"rich_text":[{"type":"text","plain_text":"ordered"}]}},
		{"type":"to_do","id":"7","to_do":{"checked":true,"rich_text":[{"type":"text","plain_text":"todo"}]}},
		{"type":"quote","id":"8","quote":{"rich_text":[{"type":"text","plain_text":"quote"}]}},
		{"type":"code","id":"9","code":{"language":"go","rich_text":[{"type":"text","plain_text":"fmt.Println()"}]}},
		{"type":"divider","id":"10","divider":{}}
	]`
	var blocks []Block
	if err := json.Unmarshal([]byte(src), &blocks); err != nil {
		t.Fatal(err)
	}
	out, _ := BlocksFromNotion(blocks)
	if len(out) != len(blocks) {
		t.Fatalf("blocks = %d, want %d", len(out), len(blocks))
	}
	want := []domain.BlockType{
		domain.BlockParagraph, domain.BlockHeading, domain.BlockHeading, domain.BlockHeading,
		domain.BlockBullet, domain.BlockOrdered, domain.BlockTodo, domain.BlockQuote,
		domain.BlockCode, domain.BlockDivider,
	}
	for i, typ := range want {
		if out[i].Type != typ {
			t.Errorf("block %d type = %q, want %q", i, out[i].Type, typ)
		}
	}
	if out[1].Level != 1 || out[2].Level != 2 || out[3].Level != 3 {
		t.Errorf("heading levels = %d/%d/%d", out[1].Level, out[2].Level, out[3].Level)
	}
	if !out[6].Checked || out[8].Language != "go" {
		t.Errorf("todo/code metadata lost: %+v", out)
	}
}

func TestHeadingLevelsAboveThreeClampToNotionH3(t *testing.T) {
	for level := 4; level <= 6; level++ {
		out, err := BlocksToNotion([]domain.Block{{
			Type:  domain.BlockHeading,
			Level: level,
			Spans: spans("deep"),
		}}, nil)
		if err != nil {
			t.Fatalf("level %d: %v", level, err)
		}
		if len(out) != 1 || out[0].Type != "heading_3" || out[0].Heading3 == nil {
			t.Errorf("level %d -> %+v, want heading_3", level, out)
		}
	}
}
