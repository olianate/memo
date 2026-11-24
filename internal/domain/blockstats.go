package domain

import (
	"encoding/json"
	"unicode"
	"unicode/utf8"
)

// BlockStats is a retained summary of body composition. It is written at save
// time so the dashboard keeps working after the body itself is evicted.
type BlockStats struct {
	Words      int `json:"words"`
	Characters int `json:"characters"`
	Paragraph  int `json:"paragraph"`
	Heading    int `json:"heading"`
	List       int `json:"list"`
	Todo       int `json:"todo"`
	TodoDone   int `json:"todo_done"`
	Quote      int `json:"quote"`
	Code       int `json:"code"`
	Attachment int `json:"attachment"`
}

// AnalyzeBlocks summarizes a body for the statistics screen.
func AnalyzeBlocks(blocks []Block) BlockStats {
	var s BlockStats
	for _, b := range blocks {
		text := b.PlainText()
		s.Words += CountWords(text)
		s.Characters += utf8.RuneCountInString(text)
		switch b.Type {
		case BlockParagraph:
			s.Paragraph++
		case BlockHeading:
			s.Heading++
		case BlockBullet, BlockOrdered:
			s.List++
		case BlockTodo:
			s.Todo++
			if b.Checked {
				s.TodoDone++
			}
		case BlockQuote:
			s.Quote++
		case BlockCode:
			s.Code++
		case BlockImage, BlockFile:
			s.Attachment++
		}
	}
	return s
}

// CountWords counts CJK characters individually and Latin-script runs as words.
func CountWords(text string) int {
	count := 0
	inWord := false
	for _, r := range text {
		switch {
		case isCJK(r):
			count++
			inWord = false
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if !inWord {
				count++
				inWord = true
			}
		default:
			inWord = false
		}
	}
	return count
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}

// MarshalBlockStats serializes the summary for the retained metadata column.
func MarshalBlockStats(s BlockStats) string {
	data, _ := json.Marshal(s)
	return string(data)
}

// UnmarshalBlockStats parses a retained summary; malformed means zero.
func UnmarshalBlockStats(raw string) BlockStats {
	var s BlockStats
	_ = json.Unmarshal([]byte(raw), &s)
	return s
}
