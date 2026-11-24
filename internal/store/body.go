package store

import (
	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/domain/markdown"
)

// Derived is everything the store computes from the canonical Markdown on
// write. None of it is a second source of truth.
type Derived struct {
	Text    string
	Snippet string
	Hash    string
	Stats   domain.BlockStats
}

// Derive projects a body. blocks must be markdown.Parse(body); callers that
// have already parsed it pass it in so one edit parses the body once.
func Derive(body string, tags []string, blocks []domain.Block) Derived {
	text := domain.Text(blocks)
	return Derived{
		Text:    text,
		Snippet: domain.Snippet(text, 200),
		Hash:    domain.HashContent(body, tags),
		Stats:   domain.AnalyzeBlocks(blocks),
	}
}

func deriveBody(body string, tags []string) Derived {
	return Derive(body, tags, markdown.Parse(body))
}
