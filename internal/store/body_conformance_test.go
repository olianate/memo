package store

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/olianate/memo/internal/domain/markdown"
)

type bodySuite struct {
	Cases []bodyCase `json:"cases"`
}

type bodyCase struct {
	Name        string `json:"name"`
	Body        string `json:"body"`
	BodyText    string `json:"bodyText"`
	Snippet     string `json:"snippet"`
	Words       int    `json:"words"`
	Characters  int    `json:"characters"`
	Composition struct {
		Paragraph  int `json:"paragraph"`
		Heading    int `json:"heading"`
		List       int `json:"list"`
		Todo       int `json:"todo"`
		Quote      int `json:"quote"`
		Code       int `json:"code"`
		Attachment int `json:"attachment"`
	} `json:"composition"`
	TodoTotal int `json:"todoTotal"`
	TodoDone  int `json:"todoDone"`
}

func TestBodyProjectionConformance(t *testing.T) {
	raw, err := os.ReadFile("../../frontend/src/test/body-suite.json")
	if err != nil {
		t.Fatalf("read shared body suite: %v", err)
	}
	var suite bodySuite
	if err := json.Unmarshal(raw, &suite); err != nil {
		t.Fatalf("parse shared body suite: %v", err)
	}
	for _, tc := range suite.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			derived := Derive(tc.Body, nil, markdown.Parse(tc.Body))
			if derived.Text != tc.BodyText {
				t.Errorf("text = %q, want %q", derived.Text, tc.BodyText)
			}
			if derived.Snippet != tc.Snippet {
				t.Errorf("snippet = %q, want %q", derived.Snippet, tc.Snippet)
			}
			stats := derived.Stats
			if stats.Words != tc.Words {
				t.Errorf("words = %d, want %d", stats.Words, tc.Words)
			}
			if stats.Characters != tc.Characters {
				t.Errorf("characters = %d, want %d", stats.Characters, tc.Characters)
			}
			if stats.Paragraph != tc.Composition.Paragraph ||
				stats.Heading != tc.Composition.Heading ||
				stats.List != tc.Composition.List ||
				stats.Todo != tc.Composition.Todo ||
				stats.Quote != tc.Composition.Quote ||
				stats.Code != tc.Composition.Code ||
				stats.Attachment != tc.Composition.Attachment {
				t.Errorf("composition = %+v, want %+v", stats, tc.Composition)
			}
			if stats.TodoDone != tc.TodoDone {
				t.Errorf("todo done = %d, want %d", stats.TodoDone, tc.TodoDone)
			}
			if stats.Todo != tc.TodoTotal {
				t.Errorf("todo total = %d, want %d", stats.Todo, tc.TodoTotal)
			}
		})
	}
}
