// Package services implements the Service content type: the GEO-optimized service
// page — the format answer engines quote. It pairs a definitional summary with a
// rich body, citable facts (price, area served) and an ORDERED FAQ list, plus
// revisions and soft-delete — the full vertical slice over the shared content
// kernel. All business logic lives in the service; data access is only through
// the repository interfaces; side effects are only emitted as events.
//
// Like pages, services have NO per-author ownership in the canon: the permission
// grant (Editor/Administrator) alone gates every action (mirrors django).
package services

import (
	"time"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
)

// Service is the domain representation of a service page. publishedAt is stamped
// ONCE on first publish and preserved across re-publish. FAQs is the ordered Q&A
// list (loaded by the read paths; empty on the bare row).
type Service struct {
	ID          uuid.UUID
	Title       string
	Slug        string
	Summary     string // plain definitional sentence(s)
	Body        string // sanitized HTML (kernel.SanitizeRichText on every save)
	Price       string // freeform, e.g. "From $499"
	AreaServed  string // freeform, e.g. "Berlin and surrounding areas"
	Status      kernel.Status
	PublishedAt *time.Time
	ReadingTime int
	DeletedAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time

	FAQs []FAQ

	// TODO(M8 SEO fields): MetaTitle, MetaDescription, CanonicalURL, NoIndex.
}

// Translation is the per-locale CONTENT overlay for a service (M7b-2). It carries
// only the translatable text fields for a NON-default locale; the base service
// row holds the default-locale (en) content plus every structural/citable field
// (slug, status, price, area_served, schedule), which are shared across locales.
//
// TODO(M8): MetaTitle/MetaDescription join here when SEO fields translate.
type Translation struct {
	Locale  string
	Title   string
	Summary string
	Body    string // sanitized HTML (kernel.SanitizeRichText on every save)
}

// FAQ is one ordered question/answer pair belonging to a service.
type FAQ struct {
	ID        uuid.UUID
	ServiceID uuid.UUID
	Question  string
	Answer    string // sanitized HTML (rich text allowed in answers)
	Position  int
}

// Published reports whether the service is visible on the public site.
func (s Service) Published() bool {
	return s.Status == kernel.StatusPublished && s.DeletedAt == nil
}

// Trashed reports whether the service is soft-deleted.
func (s Service) Trashed() bool { return s.DeletedAt != nil }

// snapshot is the immutable scalar capture stored as a revision before each
// update. It captures the service's text fields (FAQs are structural and are not
// part of the snapshot, mirroring the posts/pages text-only revision policy).
type snapshot struct {
	Title      string `json:"title"`
	Slug       string `json:"slug"`
	Summary    string `json:"summary"`
	Body       string `json:"body"`
	Price      string `json:"price"`
	AreaServed string `json:"area_served"`
	Status     string `json:"status"`
}
