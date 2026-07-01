// Package search implements M6 site search: a public, cross-type full-text
// search over published posts, pages, and services. All logic lives in the
// service; data access is only through the Repository (the ONLY sqlc/pgx layer);
// handlers are thin HTTP boundaries. Search is public-facing — there is no auth.
package search

import (
	"time"

	"github.com/google/uuid"
)

// HitType is the discriminant for a search hit's content type. It drives the
// public URL prefix and the result-card eyebrow/badge.
type HitType string

// HitPost, HitPage, and HitService are the content-type discriminants for a
// search hit.
const (
	HitPost    HitType = "post"
	HitPage    HitType = "page"
	HitService HitType = "service"
)

// String returns the raw type string (post|page|service).
func (t HitType) String() string { return string(t) }

// Hit is one unified search result across the content types. Snippet is an
// already-highlighted (FTS) or plain (fallback) excerpt; URL is the public path
// the service resolved from Type + Slug. Rank is the relevance score used for
// ordering (higher is more relevant).
type Hit struct {
	Type        HitType
	ID          uuid.UUID
	Title       string
	Slug        string
	Snippet     string
	URL         string
	PublishedAt *time.Time
	Rank        float64
}

// Result is a paginated search response. Query echoes the trimmed term back to
// the view (for the input value + the empty-state message); Fallback reports
// whether the ILIKE path served the hits (FTS found nothing).
type Result struct {
	Query    string
	Hits     []Hit
	Total    int
	Page     int
	PerPage  int
	Fallback bool
}
