package store

import (
	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/domain/markdown"
)

// derivedBody is everything the store computes from the canonical Markdown on
// write. None of it is a second source of truth.
type derivedBody struct {
	Text    string
	Snippet string
	Hash    string
	Stats   domain.BlockStats
}

func deriveBody(body string, tags []string) derivedBody {
	blocks := markdown.Parse(body)
	text := domain.Text(blocks)
	return derivedBody{
		Text:    text,
		Snippet: domain.Snippet(text, 200),
		Hash:    domain.HashContent(body, tags),
		Stats:   domain.AnalyzeBlocks(blocks),
	}
}
