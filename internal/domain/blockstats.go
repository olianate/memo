package domain

import "encoding/json"

// BlockStats is a retained summary of body composition. It is written at save
// time so the dashboard keeps working after the body itself is evicted.
type BlockStats struct {
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
