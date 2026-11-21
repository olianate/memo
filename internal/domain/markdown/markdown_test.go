package markdown

import (
	"testing"

	"memo/internal/domain"
)

func types(blocks []domain.Block) []domain.BlockType {
	out := make([]domain.BlockType, len(blocks))
	for i, b := range blocks {
		out[i] = b.Type
	}
	return out
}

func TestParseBlockTypes(t *testing.T) {
	src := "# Heading\n\nparagraph\n\n- bullet\n\n1. ordered\n\n- [x] done\n\n> quote\n\n```go\nfmt.Println()\n```\n\n---\n\n![shot](attachment://a1)\n\n[report.pdf](attachment://a2)"
	blocks := Parse(src)
	want := []domain.BlockType{
		domain.BlockHeading, domain.BlockParagraph, domain.BlockBullet, domain.BlockOrdered,
		domain.BlockTodo, domain.BlockQuote, domain.BlockCode, domain.BlockDivider,
		domain.BlockImage, domain.BlockFile,
	}
	got := types(blocks)
	if len(got) != len(want) {
		t.Fatalf("block count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("block %d = %q, want %q", i, got[i], want[i])
		}
	}
	if blocks[0].Level != 1 {
		t.Errorf("heading level = %d, want 1", blocks[0].Level)
	}
	if !blocks[4].Checked {
		t.Error("todo should be checked")
	}
	if blocks[2].PlainText() != "bullet" {
		t.Errorf("bullet text = %q", blocks[2].PlainText())
	}
	if blocks[6].Language != "go" {
		t.Errorf("code language = %q", blocks[6].Language)
	}
	if blocks[8].AttachmentID != "a1" || blocks[8].Alt != "shot" {
		t.Errorf("image block = %+v", blocks[8])
	}
}

func TestInlineMarks(t *testing.T) {
	blocks := Parse("**bold** and *italic* and ~~gone~~ and `code` and [link](https://example.com)")
	spans := blocks[0].Spans
	want := []struct {
		text   string
		bold   bool
		italic bool
		strike bool
		code   bool
		link   string
	}{
		{"bold", true, false, false, false, ""},
		{" and ", false, false, false, false, ""},
		{"italic", false, true, false, false, ""},
		{" and ", false, false, false, false, ""},
		{"gone", false, false, true, false, ""},
		{" and ", false, false, false, false, ""},
		{"code", false, false, false, true, ""},
		{" and ", false, false, false, false, ""},
		{"link", false, false, false, false, "https://example.com"},
	}
	if len(spans) != len(want) {
		t.Fatalf("span count = %d, want %d: %+v", len(spans), len(want), spans)
	}
	for i, w := range want {
		got := spans[i]
		if got.Text != w.text || got.Bold != w.bold || got.Italic != w.italic || got.Strike != w.strike || got.Code != w.code || got.Link != w.link {
			t.Errorf("span %d = %+v, want %+v", i, got, w)
		}
	}
}

func TestInlineNestingAndLiterals(t *testing.T) {
	blocks := Parse("**bold with *italic* inside** and snake_case_name and C# and 2 * 3")
	text := domain.Text(blocks)
	if text != "bold with italic inside and snake_case_name and C# and 2 * 3" {
		t.Fatalf("plain text = %q", text)
	}
	spans := blocks[0].Spans
	if !spans[0].Bold {
		t.Error("outer bold mark missing")
	}
	foundItalic := false
	for _, s := range spans {
		if s.Text == "italic" && s.Italic && s.Bold {
			foundItalic = true
		}
	}
	if !foundItalic {
		t.Errorf("nested italic span missing: %+v", spans)
	}
}

func TestHeadingVsTag(t *testing.T) {
	if blocks := Parse("# Heading"); blocks[0].Type != domain.BlockHeading {
		t.Fatalf("`# Heading` should be a heading, got %q", blocks[0].Type)
	}
	if blocks := Parse("#tag"); blocks[0].Type != domain.BlockParagraph || blocks[0].PlainText() != "#tag" {
		t.Fatalf("`#tag` should stay a paragraph, got %+v", blocks[0])
	}
}

func TestUnsupportedSyntaxStaysLiteral(t *testing.T) {
	src := "| a | b |\n\n<div>hi</div>\n\n$$\nx\n$$"
	blocks := Parse(src)
	for _, b := range blocks {
		if b.Type != domain.BlockParagraph {
			t.Errorf("unsupported syntax became %q", b.Type)
		}
	}
	if !contains(domain.Text(blocks), "| a | b |") {
		t.Errorf("table text lost: %q", domain.Text(blocks))
	}
}

func TestSerializeRoundTrip(t *testing.T) {
	src := "# Title\n\nA **bold** line with *italic*, ~~strike~~, `code` and [a link](https://example.com).\n\n- one\n- two\n\n1. first\n\n- [ ] open\n- [x] done\n\n> quoted\n\n```js\nconst x = 1\n```\n\n---\n\n![pic](attachment://img)\n\n[file](attachment://doc)"
	blocks := Parse(src)
	again := Parse(Serialize(blocks))
	if len(again) != len(blocks) {
		t.Fatalf("round trip block count = %d, want %d", len(again), len(blocks))
	}
	for i := range blocks {
		if again[i].Type != blocks[i].Type {
			t.Errorf("block %d type = %q, want %q", i, again[i].Type, blocks[i].Type)
		}
		if again[i].PlainText() != blocks[i].PlainText() {
			t.Errorf("block %d text = %q, want %q", i, again[i].PlainText(), blocks[i].PlainText())
		}
		if again[i].Checked != blocks[i].Checked || again[i].AttachmentID != blocks[i].AttachmentID || again[i].Level != blocks[i].Level {
			t.Errorf("block %d metadata changed: %+v -> %+v", i, blocks[i], again[i])
		}
	}
}

func TestExtractedTextStripsMarkdown(t *testing.T) {
	blocks := Parse("# A **bold** title\n\n- item with `code`")
	text := domain.Text(blocks)
	if text != "A bold title\nitem with code" {
		t.Fatalf("text = %q", text)
	}
	if domain.Snippet("  lots   of\n\nspace  ", 100) != "lots of space" {
		t.Fatalf("snippet = %q", domain.Snippet("  lots   of\n\nspace  ", 100))
	}
}

func TestParseImageWithEmptyAlt(t *testing.T) {
	blocks := Parse("![](attachment://abc)")
	if len(blocks) != 1 || blocks[0].Type != domain.BlockImage || blocks[0].AttachmentID != "abc" {
		t.Fatalf("blocks = %+v", blocks)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
