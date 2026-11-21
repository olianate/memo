package domain

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ExtractTagToken finds a live `#tag` token immediately before caret. It
// returns false for headings (`# `), C#, inline code and code blocks, which the
// caller excludes before calling.
func ExtractTagToken(text string, caret int) (token string, start int, end int, ok bool) {
	if caret < 0 || caret > len(text) {
		return "", 0, 0, false
	}
	// Walk back over the tag body.
	start = caret
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:start])
		if r == '#' || unicode.IsSpace(r) {
			break
		}
		if !isTagRune(r) {
			return "", 0, 0, false
		}
		start -= size
	}
	if start == 0 {
		return "", 0, 0, false
	}
	r, size := utf8.DecodeLastRuneInString(text[:start])
	if r != '#' {
		return "", 0, 0, false
	}
	start -= size
	// A tag must be at the start or preceded by whitespace. This rejects C#.
	if start > 0 {
		prev, _ := utf8.DecodeLastRuneInString(text[:start])
		if !unicode.IsSpace(prev) && prev != '(' && prev != '[' && prev != '（' && prev != '【' {
			return "", 0, 0, false
		}
	}
	// A token inside an unmatched backtick is inline code, not a tag.
	if strings.Count(text[:start], "`")%2 == 1 {
		return "", 0, 0, false
	}
	body := text[start+1 : caret]
	if body == "" {
		return "", 0, 0, false
	}
	if strings.TrimSpace(body) != body {
		return "", 0, 0, false
	}
	return body, start, caret, true
}

func isTagRune(r rune) bool {
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return true
	}
	switch r {
	case '_', '-', '·':
		return true
	}
	return false
}

// RemoveTagToken deletes a committed `#tag` token and tidies the whitespace the
// token leaves behind. The token is consumed; it must never remain in the body.
func RemoveTagToken(text string, start, end int) string {
	if start < 0 || end > len(text) || start >= end {
		return text
	}
	before := text[:start]
	after := text[end:]
	// Collapse a double space created in the middle of a line.
	if len(before) > 0 && len(after) > 0 {
		br, _ := utf8.DecodeLastRuneInString(before)
		ar, _ := utf8.DecodeRuneInString(after)
		if unicode.IsSpace(br) && unicode.IsSpace(ar) && ar != '\n' {
			after = strings.TrimPrefix(after, " ")
		}
	}
	return before + after
}

// NormalizeTag is the case-insensitive, NFC-ish comparison key. Vietnamese and
// Chinese display casing is preserved separately in Tag.Name.
func NormalizeTag(name string) string {
	name = strings.TrimPrefix(strings.TrimSpace(name), "#")
	// Notion multi-select options are case-insensitive; lower-casing is enough
	// for the tag vocabulary this app supports.
	return strings.ToLower(name)
}

// CleanTagName strips a leading # and surrounding whitespace.
func CleanTagName(name string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "#"))
}
