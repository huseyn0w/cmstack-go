package posts

import (
	"time"

	"github.com/google/uuid"
)

// Event names persisted as the outbox event_name / used for bus subscriptions.
const (
	// EventRevisionCreated fires SYNCHRONOUSLY (in-tx) every time a revision
	// snapshot is captured. It runs inside the publishing transaction so the
	// snapshot and the update that supersedes it commit atomically.
	EventRevisionCreated = "content.revision.created"
	// EventContentPublished fires ASYNCHRONOUSLY after a post transitions into
	// PUBLISHED. Its async listeners are cache-invalidation + search-reindex
	// seams (M-later), drained from the outbox by the worker.
	EventContentPublished = "content.published"
)

// RevisionCreatedEvent is emitted (sync) when a content revision is snapshotted.
// It carries enough to drive any in-tx bookkeeping without re-reading the row.
type RevisionCreatedEvent struct {
	RevisionID uuid.UUID  `json:"revision_id"`
	EntityType string     `json:"entity_type"`
	EntityID   uuid.UUID  `json:"entity_id"`
	AuthorID   *uuid.UUID `json:"author_id,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Name implements events.Event.
func (RevisionCreatedEvent) Name() string { return EventRevisionCreated }

// ContentPublishedEvent is emitted (async) when a post becomes published. The
// cache-invalidation and search-reindex listeners consume it after commit.
type ContentPublishedEvent struct {
	EntityType  string    `json:"entity_type"`
	PostID      uuid.UUID `json:"post_id"`
	Slug        string    `json:"slug"`
	Title       string    `json:"title"`
	PublishedAt time.Time `json:"published_at"`
}

// Name implements events.Event.
func (ContentPublishedEvent) Name() string { return EventContentPublished }
