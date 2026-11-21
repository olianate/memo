package services

import (
	"memo/internal/domain"
	"memo/internal/domain/markdown"
)

func markdownParse(source string) []domain.Block     { return markdown.Parse(source) }
func markdownSerialize(blocks []domain.Block) string { return markdown.Serialize(blocks) }
