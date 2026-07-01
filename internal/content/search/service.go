package search

import (
	"context"
	"strings"
)

// DefaultPerPage is the search results page size when the caller passes <= 0.
const DefaultPerPage = 10

// maxPerPage caps the page size so a hostile ?perPage can't request an unbounded
// scan.
const maxPerPage = 50

// Service holds the search business logic: query validation, the FTS-then-ILIKE
// strategy, public URL building, and pagination. It is public-facing and takes
// no authorizer — search only ever reads published, non-trashed content (the
// repo queries enforce that).
type Service struct {
	repo Repository
}

// NewService constructs the search service over its repository.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Search runs a paginated public search for query on page (1-based) with perPage
// results. Strategy: trim the query; an empty query returns an empty Result (no
// error). Otherwise run FTS first; if FTS matches nothing, fall back to the
// ILIKE substring scan (django parity — catches substrings/typos the tsquery
// misses). URLs are resolved per type. Pagination is limit/offset.
func (s *Service) Search(ctx context.Context, query string, page, perPage int) (Result, error) {
	query = strings.TrimSpace(query)
	if perPage <= 0 {
		perPage = DefaultPerPage
	}
	if perPage > maxPerPage {
		perPage = maxPerPage
	}
	if page < 1 {
		page = 1
	}

	res := Result{Query: query, Page: page, PerPage: perPage}
	if query == "" {
		res.Hits = []Hit{}
		return res, nil
	}

	offset := (page - 1) * perPage

	// FTS first.
	total, err := s.repo.CountFTS(ctx, query)
	if err != nil {
		return Result{}, err
	}
	if total > 0 {
		hits, err := s.repo.FTS(ctx, query, perPage, offset)
		if err != nil {
			return Result{}, err
		}
		res.Total = total
		res.Hits = s.decorate(hits)
		return res, nil
	}

	// Fallback: ILIKE substring scan.
	total, err = s.repo.CountILIKE(ctx, query)
	if err != nil {
		return Result{}, err
	}
	res.Fallback = true
	res.Total = total
	if total == 0 {
		res.Hits = []Hit{}
		return res, nil
	}
	hits, err := s.repo.ILIKE(ctx, query, perPage, offset)
	if err != nil {
		return Result{}, err
	}
	res.Hits = s.decorate(hits)
	return res, nil
}

// decorate resolves each hit's public URL from its type + slug.
func (s *Service) decorate(hits []Hit) []Hit {
	for i := range hits {
		hits[i].URL = publicURL(hits[i].Type, hits[i].Slug)
	}
	return hits
}

// publicURL builds the public path for a hit. Post -> /blog/{slug}; page ->
// /p/{slug}; service -> /services/{slug} (matches the router's public routes).
func publicURL(t HitType, slug string) string {
	switch t {
	case HitPost:
		return "/blog/" + slug
	case HitPage:
		return "/p/" + slug
	case HitService:
		return "/services/" + slug
	default:
		return "/"
	}
}
