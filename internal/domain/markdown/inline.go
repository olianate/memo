package markdown

import (
	"strings"

	"memo/internal/domain"
)

// parseInline turns the supported inline Markdown into flat spans. Unmatched
// delimiters stay literal. Nesting (bold italic, links with emphasis) works.
func parseInline(s string) []domain.Span {
	spans := parseInlineStyle(s, domain.Span{})
	return mergeSpans(spans)
}

func addText(spans []domain.Span, text string, style domain.Span) []domain.Span {
	if text == "" {
		return spans
	}
	span := style
	span.Text = text
	return append(spans, span)
}

func parseInlineStyle(s string, style domain.Span) []domain.Span {
	var out []domain.Span
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			out = addText(out, text.String(), style)
			text.Reset()
		}
	}

	for i := 0; i < len(s); {
		c := s[i]

		if c == '\\' && i+1 < len(s) {
			text.WriteByte(s[i+1])
			i += 2
			continue
		}

		if c == '`' {
			if end := strings.IndexByte(s[i+1:], '`'); end >= 0 {
				flush()
				codeStyle := style
				codeStyle.Code = true
				out = addText(out, s[i+1:i+1+end], codeStyle)
				i += end + 2
				continue
			}
		}

		if c == '[' {
			if closeBracket := strings.Index(s[i+1:], "]("); closeBracket >= 0 {
				closeBracket += i + 1
				if urlEnd := strings.IndexByte(s[closeBracket+2:], ')'); urlEnd >= 0 {
					urlEnd += closeBracket + 2
					label := s[i+1 : closeBracket]
					url := s[closeBracket+2 : urlEnd]
					flush()
					linkStyle := style
					linkStyle.Link = url
					out = append(out, parseInlineStyle(label, linkStyle)...)
					i = urlEnd + 1
					continue
				}
			}
		}

		if strings.HasPrefix(s[i:], "**") || strings.HasPrefix(s[i:], "__") {
			delim := s[i : i+2]
			if end := strings.Index(s[i+2:], delim); end >= 0 {
				flush()
				boldStyle := style
				boldStyle.Bold = true
				out = append(out, parseInlineStyle(s[i+2:i+2+end], boldStyle)...)
				i += 2 + end + 2
				continue
			}
		}

		if strings.HasPrefix(s[i:], "~~") {
			if end := strings.Index(s[i+2:], "~~"); end >= 0 {
				flush()
				strikeStyle := style
				strikeStyle.Strike = true
				out = append(out, parseInlineStyle(s[i+2:i+2+end], strikeStyle)...)
				i += 2 + end + 2
				continue
			}
		}

		if c == '*' || c == '_' {
			// Don't treat underscores inside words (snake_case) as emphasis.
			wordBefore := i > 0 && isWordByte(s[i-1])
			if end := findSingleDelimiter(s, i+1, c); end >= 0 {
				wordAfter := end+1 < len(s) && isWordByte(s[end+1])
				if !(c == '_' && (wordBefore || wordAfter)) {
					flush()
					italicStyle := style
					italicStyle.Italic = true
					out = append(out, parseInlineStyle(s[i+1:end], italicStyle)...)
					i = end + 1
					continue
				}
			}
		}

		text.WriteByte(c)
		i++
	}
	flush()
	return out
}

func findSingleDelimiter(s string, from int, delim byte) int {
	for i := from; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == delim {
			return i
		}
	}
	return -1
}

func isWordByte(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= 0x80
}

func mergeSpans(spans []domain.Span) []domain.Span {
	if len(spans) < 2 {
		return spans
	}
	out := make([]domain.Span, 0, len(spans))
	for _, span := range spans {
		if span.Text == "" {
			continue
		}
		if len(out) > 0 {
			last := &out[len(out)-1]
			if last.Bold == span.Bold && last.Italic == span.Italic && last.Strike == span.Strike && last.Code == span.Code && last.Link == span.Link {
				last.Text += span.Text
				continue
			}
		}
		out = append(out, span)
	}
	return out
}
