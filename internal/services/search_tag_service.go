package services

import (
	"context"
	"time"

	"github.com/olianate/memo/internal/search"
	"github.com/olianate/memo/internal/stats"
	"github.com/olianate/memo/internal/store"
)

// SearchService wraps the local FTS/LIKE search.
type SearchService struct{ search *search.Service }

// NewSearchService builds the search boundary.
func NewSearchService(s *search.Service) *SearchService { return &SearchService{search: s} }

// Query searches memo text and tags, optionally filtered by tag and date.
func (s *SearchService) Query(query, tag, from, to string) ([]SearchResultDTO, error) {
	opts := search.Options{Query: query, Tag: tag}
	if t, err := time.Parse(time.RFC3339, from); err == nil {
		opts.From = &t
	}
	if t, err := time.Parse(time.RFC3339, to); err == nil {
		opts.To = &t
	}
	results, err := s.search.Search(context.Background(), opts)
	if err != nil {
		return nil, err
	}
	out := make([]SearchResultDTO, 0, len(results))
	for _, r := range results {
		out = append(out, SearchResultDTO{
			Memo:   toMemoDTO(r.Memo, parseBlocks(r.Memo)),
			Before: r.Snippet.Before, Match: r.Snippet.Match, After: r.Snippet.After,
		})
	}
	return out, nil
}

// TagService is the first-class tag boundary. Renames and merges never rewrite
// memo bodies or revisions.
type TagService struct {
	store *store.Store
	emit  func(string, any)
}

// NewTagService builds the tag boundary.
func NewTagService(st *store.Store, emit func(string, any)) *TagService {
	return &TagService{store: st, emit: emit}
}

func (s *TagService) changed() {
	if s.emit != nil {
		s.emit("tags:changed", nil)
		s.emit("memos:changed", nil)
	}
}

// List returns tags with active-memo counts.
func (s *TagService) List() ([]TagDTO, error) {
	tags, err := s.store.ListTags(context.Background())
	if err != nil {
		return nil, err
	}
	out := make([]TagDTO, 0, len(tags))
	for _, t := range tags {
		out = append(out, TagDTO{ID: t.ID, Name: t.Name, Normalized: t.NormalizedName, Count: t.Count})
	}
	return out, nil
}

// Suggest returns tag autocomplete options for a `#` prefix.
func (s *TagService) Suggest(query string) ([]TagDTO, error) {
	tags, err := s.store.SuggestTags(context.Background(), query, 8)
	if err != nil {
		return nil, err
	}
	out := make([]TagDTO, 0, len(tags))
	for _, t := range tags {
		out = append(out, TagDTO{ID: t.ID, Name: t.Name, Normalized: t.NormalizedName, Count: t.Count})
	}
	return out, nil
}

// Rename changes a tag's display name and normalized key.
func (s *TagService) Rename(id, name string) (TagDTO, error) {
	tag, err := s.store.RenameTag(context.Background(), id, name)
	if err != nil {
		return TagDTO{}, err
	}
	s.changed()
	return TagDTO{ID: tag.ID, Name: tag.Name, Normalized: tag.NormalizedName}, nil
}

// Merge moves all memos from source into target and deletes source.
func (s *TagService) Merge(sourceID, targetID string) error {
	if err := s.store.MergeTags(context.Background(), sourceID, targetID); err != nil {
		return err
	}
	s.changed()
	return nil
}

// Delete removes a tag from every memo.
func (s *TagService) Delete(id string) error {
	if err := s.store.DeleteTag(context.Background(), id); err != nil {
		return err
	}
	s.changed()
	return nil
}

// StatsService is the metadata-driven dashboard boundary.
type StatsService struct{ stats *stats.Service }

// NewStatsService builds the dashboard boundary.
func NewStatsService(s *stats.Service) *StatsService { return &StatsService{stats: s} }

// Dashboard computes the selected month's dashboard from local data only.
func (s *StatsService) Dashboard(month string) (*stats.Dashboard, error) {
	return s.stats.Dashboard(context.Background(), month, time.Now())
}
