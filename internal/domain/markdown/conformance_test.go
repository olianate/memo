package markdown

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/olianate/memo/internal/domain"
)

// The suite lives in the frontend so the editor's projection and the sync
// projection are pinned to the same blocks.
const sharedSuitePath = "../../../frontend/src/test/markdown-suite.json"

type suiteBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	Level        int           `json:"level"`
	Checked      bool          `json:"checked"`
	Language     string        `json:"language"`
	Alt          string        `json:"alt"`
	AttachmentID string        `json:"attachment_id"`
	Spans        []domain.Span `json:"spans"`
}

type suiteCase struct {
	Name   string       `json:"name"`
	Body   string       `json:"body"`
	Blocks []suiteBlock `json:"blocks"`
}

func TestSharedMarkdownSuite(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(sharedSuitePath))
	if err != nil {
		t.Fatalf("read shared suite: %v", err)
	}
	var suite struct {
		Cases []suiteCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &suite); err != nil {
		t.Fatalf("decode shared suite: %v", err)
	}
	if len(suite.Cases) == 0 {
		t.Fatal("shared suite has no cases")
	}

	for _, tc := range suite.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			blocks := Parse(tc.Body)
			if len(blocks) != len(tc.Blocks) {
				t.Fatalf("got %d blocks, want %d: %#v", len(blocks), len(tc.Blocks), blocks)
			}
			for i, want := range tc.Blocks {
				got := blocks[i]
				if string(got.Type) != want.Type {
					t.Errorf("block %d type = %q, want %q", i, got.Type, want.Type)
				}
				if got.PlainText() != want.Text {
					t.Errorf("block %d text = %q, want %q", i, got.PlainText(), want.Text)
				}
				if got.Level != want.Level {
					t.Errorf("block %d level = %d, want %d", i, got.Level, want.Level)
				}
				if got.Checked != want.Checked {
					t.Errorf("block %d checked = %v, want %v", i, got.Checked, want.Checked)
				}
				if got.Language != want.Language {
					t.Errorf("block %d language = %q, want %q", i, got.Language, want.Language)
				}
				if got.Alt != want.Alt {
					t.Errorf("block %d alt = %q, want %q", i, got.Alt, want.Alt)
				}
				if got.AttachmentID != want.AttachmentID {
					t.Errorf("block %d attachment id = %q, want %q", i, got.AttachmentID, want.AttachmentID)
				}
				if want.Spans != nil && !reflect.DeepEqual(got.Spans, want.Spans) {
					t.Errorf("block %d spans = %#v, want %#v", i, got.Spans, want.Spans)
				}
			}
		})
	}
}
