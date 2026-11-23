package services

import (
	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/domain/markdown"
)

func markdownParse(source string) []domain.Block     { return markdown.Parse(source) }
func markdownSerialize(blocks []domain.Block) string { return markdown.Serialize(blocks) }
