// Package markdown parses and serializes the supported Markdown subset to and
// from the canonical block AST. Unsupported syntax stays literal text.
package markdown

import (
	"strings"

	"memo/internal/domain"
)

// Parse converts Markdown source into blocks. Block IDs are freshly generated;
// Markdown carries no IDs.
func Parse(src string) []domain.Block {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	lines := strings.Split(src, "\n")
	blocks := make([]domain.Block, 0, len(lines))

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "```") {
			lang := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			var code []string
			i++
			for ; i < len(lines); i++ {
				if strings.TrimSpace(lines[i]) == "```" {
					break
				}
				code = append(code, lines[i])
			}
			blocks = append(blocks, domain.Block{
				ID:       domain.NewID(),
				Type:     domain.BlockCode,
				Language: lang,
				Spans:    []domain.Span{{Text: strings.Join(code, "\n")}},
			})
			continue
		}

		if isDivider(trimmed) {
			blocks = append(blocks, domain.Block{ID: domain.NewID(), Type: domain.BlockDivider})
			continue
		}

		if level, text, ok := heading(trimmed); ok {
			blocks = append(blocks, domain.Block{
				ID:    domain.NewID(),
				Type:  domain.BlockHeading,
				Level: level,
				Spans: parseInline(text),
			})
			continue
		}

		if strings.HasPrefix(trimmed, "> ") || trimmed == ">" {
			text := strings.TrimPrefix(strings.TrimPrefix(trimmed, "> "), ">")
			blocks = append(blocks, domain.Block{
				ID:    domain.NewID(),
				Type:  domain.BlockQuote,
				Spans: parseInline(text),
			})
			continue
		}

		if kind, checked, text, ok := listItem(trimmed); ok {
			block := domain.Block{ID: domain.NewID(), Spans: parseInline(text)}
			switch kind {
			case domain.BlockTodo:
				block.Type = domain.BlockTodo
				block.Checked = checked
			case domain.BlockOrdered:
				block.Type = domain.BlockOrdered
			default:
				block.Type = domain.BlockBullet
			}
			blocks = append(blocks, block)
			continue
		}

		if block, ok := attachmentBlock(trimmed); ok {
			blocks = append(blocks, block)
			continue
		}

		blocks = append(blocks, domain.Block{
			ID:    domain.NewID(),
			Type:  domain.BlockParagraph,
			Spans: parseInline(trimmed),
		})
	}

	return blocks
}

func isDivider(s string) bool {
	if len(s) < 3 {
		return false
	}
	for _, marker := range []string{"-", "*", "_"} {
		if strings.Trim(s, marker) == "" && len(s) >= 3 {
			return true
		}
	}
	return false
}

func heading(s string) (level int, text string, ok bool) {
	n := 0
	for n < len(s) && s[n] == '#' {
		n++
	}
	if n < 1 || n > 3 || n >= len(s) || s[n] != ' ' {
		return 0, "", false
	}
	return n, strings.TrimSpace(s[n+1:]), true
}

func listItem(s string) (kind domain.BlockType, checked bool, text string, ok bool) {
	if strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "* ") {
		rest := s[2:]
		lower := strings.ToLower(rest)
		if strings.HasPrefix(lower, "[ ] ") {
			return domain.BlockTodo, false, rest[4:], true
		}
		if strings.HasPrefix(lower, "[x] ") {
			return domain.BlockTodo, true, rest[4:], true
		}
		return domain.BlockBullet, false, rest, true
	}
	// Ordered list: digits, dot or paren, space.
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i > 0 && i+1 < len(s) && (s[i] == '.' || s[i] == ')') && s[i+1] == ' ' {
		return domain.BlockOrdered, false, s[i+2:], true
	}
	return "", false, "", false
}

// attachmentBlock recognizes the two raw-mode attachment references:
//
//	![alt](attachment://id)
//	[name](attachment://id)
func attachmentBlock(s string) (domain.Block, bool) {
	const scheme = "attachment://"
	if strings.HasPrefix(s, "![") {
		closeBracket := strings.Index(s, "](")
		if closeBracket < 1 || !strings.HasSuffix(s, ")") {
			return domain.Block{}, false
		}
		url := s[closeBracket+2 : len(s)-1]
		if !strings.HasPrefix(url, scheme) {
			return domain.Block{}, false
		}
		return domain.Block{
			ID:           domain.NewID(),
			Type:         domain.BlockImage,
			Alt:          s[2:closeBracket],
			AttachmentID: strings.TrimPrefix(url, scheme),
		}, true
	}
	if strings.HasPrefix(s, "[") {
		closeBracket := strings.Index(s, "](")
		if closeBracket < 1 || !strings.HasSuffix(s, ")") {
			return domain.Block{}, false
		}
		url := s[closeBracket+2 : len(s)-1]
		if !strings.HasPrefix(url, scheme) {
			return domain.Block{}, false
		}
		return domain.Block{
			ID:           domain.NewID(),
			Type:         domain.BlockFile,
			Alt:          s[1:closeBracket],
			AttachmentID: strings.TrimPrefix(url, scheme),
		}, true
	}
	return domain.Block{}, false
}

// Serialize projects blocks to the supported Markdown subset.
func Serialize(blocks []domain.Block) string {
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case domain.BlockHeading:
			level := b.Level
			if level < 1 {
				level = 1
			}
			if level > 3 {
				level = 3
			}
			parts = append(parts, strings.Repeat("#", level)+" "+serializeSpans(b.Spans))
		case domain.BlockBullet:
			parts = append(parts, "- "+serializeSpans(b.Spans))
		case domain.BlockOrdered:
			parts = append(parts, "1. "+serializeSpans(b.Spans))
		case domain.BlockTodo:
			box := "- [ ] "
			if b.Checked {
				box = "- [x] "
			}
			parts = append(parts, box+serializeSpans(b.Spans))
		case domain.BlockQuote:
			parts = append(parts, "> "+serializeSpans(b.Spans))
		case domain.BlockCode:
			lang := strings.TrimSpace(b.Language)
			var sb strings.Builder
			sb.WriteString("```")
			sb.WriteString(lang)
			sb.WriteString("\n")
			sb.WriteString(blockText(b))
			sb.WriteString("\n```")
			parts = append(parts, sb.String())
		case domain.BlockDivider:
			parts = append(parts, "---")
		case domain.BlockImage:
			parts = append(parts, "!["+b.Alt+"](attachment://"+b.AttachmentID+")")
		case domain.BlockFile:
			parts = append(parts, "["+b.Alt+"](attachment://"+b.AttachmentID+")")
		default:
			parts = append(parts, serializeSpans(b.Spans))
		}
	}
	return strings.Join(parts, "\n\n")
}

func blockText(b domain.Block) string {
	var sb strings.Builder
	for _, span := range b.Spans {
		sb.WriteString(span.Text)
	}
	return sb.String()
}

func serializeSpans(spans []domain.Span) string {
	var sb strings.Builder
	for _, span := range spans {
		text := escapeInline(span.Text)
		if span.Code {
			text = "`" + span.Text + "`"
		}
		if span.Bold {
			text = "**" + text + "**"
		}
		if span.Italic {
			text = "*" + text + "*"
		}
		if span.Strike {
			text = "~~" + text + "~~"
		}
		if span.Link != "" {
			text = "[" + text + "](" + span.Link + ")"
		}
		sb.WriteString(text)
	}
	return sb.String()
}

func escapeInline(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch r {
		case '\\', '*', '_', '`', '~', '[', ']':
			sb.WriteRune('\\')
		}
		sb.WriteRune(r)
	}
	return sb.String()
}
