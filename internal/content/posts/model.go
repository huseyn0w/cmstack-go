// Package posts implements the Post content type: the full vertical slice over
// the shared content kernel (status, slug, sanitizer, reading time, revisions).
// All business logic lives in the service; data access is only through the
// repository interfaces; side effects are only emitted as events. Handlers are
// thin HTTP boundaries.
package posts

import (
	"time"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
)

// Post is the domain representation of a post. publishedAt is stamped ONCE on
// first publish and preserved across re-publish; scheduledAt is set while a
// DRAFT is awaiting an automatic publish.
type Post struct {
	ID          uuid.UUID
	Title       string
	Slug        string
	Excerpt     string
	Body        string // sanitized HTML (kernel.SanitizeRichText on every save)
	Status      kernel.Status
	PublishedAt *time.Time
	ScheduledAt *time.Time
	AuthorID    uuid.UUID
	ReadingTime int // whole minutes (kernel.ReadingTimeMinutes)
	LikeCount   int
	DeletedAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time

	// TODO(M8 SEO fields): MetaTitle, MetaDescription, CanonicalURL, NoIndex.
	// TODO(M3 categories/tags M2M): Categories, Tags.
}

// Published reports whether the post is visible on the public site.
func (p Post) Published() bool {
	return p.Status == kernel.StatusPublished && p.DeletedAt == nil
}

// Trashed reports whether the post is soft-deleted.
func (p Post) Trashed() bool { return p.DeletedAt != nil }

// Scheduled reports whether the post is a draft awaiting an automatic publish.
func (p Post) Scheduled() bool {
	return p.Status == kernel.StatusDraft && p.ScheduledAt != nil
}

// snapshot is the immutable scalar capture stored as a revision before each
// update. It intentionally captures TEXT fields only — taxonomy (M3) is not part
// of the snapshot, so restoring a revision restores text, not associations
// (mirrors the ts behavior).
type snapshot struct {
	Title   string `json:"title"`
	Slug    string `json:"slug"`
	Excerpt string `json:"excerpt"`
	Body    string `json:"body"`
	Status  string `json:"status"`
}
