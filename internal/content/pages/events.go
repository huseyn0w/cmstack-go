package pages

import (
	"time"

	"github.com/google/uuid"
)

// Event names persisted as the outbox event_name / used for bus subscriptions.
// They are the SAME content-level events as posts use: revision-created is sync
// (in-tx) and content-published is async (drained from the outbox). Reusing the
// names means one publish listener serves every content type.
const (
	// EventRevisionCreated fires SYNCHRONOUSLY (in-tx) every time a revision
	// snapshot is captured.
	EventRevisionCreated = "content.revision.created"
	// EventContentPublished fires ASYNCHRONOUSLY after a page transitions into
	// PUBLISHED.
	EventContentPublished = "content.published"
)

// RevisionCreatedEvent is emitted (sync) when a page revision is snapshotted.
type RevisionCreatedEvent struct {
	RevisionID uuid.UUID  `json:"revision_id"`
	EntityType string     `json:"entity_type"`
	EntityID   uuid.UUID  `json:"entity_id"`
	AuthorID   *uuid.UUID `json:"author_id,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Name implements events.Event.
func (RevisionCreatedEvent) Name() string { return EventRevisionCreated }

// ContentPublishedEvent is emitted (async) when a page becomes published. The
// shared content publish listener (cache invalidation + search reindex seams)
// consumes it after commit.
type ContentPublishedEvent struct {
	EntityType  string    `json:"entity_type"`
	PageID      uuid.UUID `json:"page_id"`
	Slug        string    `json:"slug"`
	Title       string    `json:"title"`
	PublishedAt time.Time `json:"published_at"`
}

// Name implements events.Event.
func (ContentPublishedEvent) Name() string { return EventContentPublished }
