// Package pages implements the Page content type: a standalone, optionally
// HIERARCHICAL page (parent self-reference) with a named template selector,
// revisions, and soft-delete — the full vertical slice over the shared content
// kernel (status, slug, sanitizer, reading time, revisions). All business logic
// lives in the service; data access is only through the repository interfaces;
// side effects are only emitted as events. Handlers are thin HTTP boundaries.
//
// Unlike posts, pages have NO per-author ownership in the canon: any Editor or
// Administrator with the page permission manages every page (mirrors django).
package pages

import (
	"time"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
)

// Page is the domain representation of a page. publishedAt is stamped ONCE on
// first publish and preserved across re-publish. ParentID is nil for a top-level
// page; Template is one of the server-side allow-list (see template.go).
type Page struct {
	ID          uuid.UUID
	Title       string
	Slug        string
	Body        string // sanitized HTML (kernel.SanitizeRichText on every save)
	Status      kernel.Status
	PublishedAt *time.Time
	ParentID    *uuid.UUID
	Template    string
	ReadingTime int // whole minutes (kernel.ReadingTimeMinutes)
	DeletedAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time

	// TODO(M8 SEO fields): MetaTitle, MetaDescription, CanonicalURL, NoIndex.
}

// Translation is the per-locale CONTENT overlay for a page (M7b-2). It carries
// only the translatable text fields for a NON-default locale; the base page row
// holds the default-locale (en) content plus every structural field (slug,
// status, parent, template, schedule), which are shared across locales.
//
// TODO(M8): MetaTitle/MetaDescription join here when SEO fields translate.
type Translation struct {
	Locale string
	Title  string
	Body   string // sanitized HTML (kernel.SanitizeRichText on every save)
}

// Published reports whether the page is visible on the public site.
func (p Page) Published() bool {
	return p.Status == kernel.StatusPublished && p.DeletedAt == nil
}

// Trashed reports whether the page is soft-deleted.
func (p Page) Trashed() bool { return p.DeletedAt != nil }

// snapshot is the immutable scalar capture stored as a revision before each
// update. It captures the TEXT fields plus the structural parent/template so a
// restore reinstates the prior hierarchy position and layout.
type snapshot struct {
	Title    string `json:"title"`
	Slug     string `json:"slug"`
	Body     string `json:"body"`
	Status   string `json:"status"`
	Template string `json:"template"`
	ParentID string `json:"parent_id"` // "" when top-level
}
