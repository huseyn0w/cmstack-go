package services

import (
	"time"

	"github.com/google/uuid"
)

// Event names persisted as the outbox event_name / used for bus subscriptions.
// Same content-level events as posts/pages: revision-created is sync (in-tx),
// content-published is async (drained from the outbox).
const (
	// EventRevisionCreated fires SYNCHRONOUSLY (in-tx) on each revision snapshot.
	EventRevisionCreated = "content.revision.created"
	// EventContentPublished fires ASYNCHRONOUSLY after a service is published.
	EventContentPublished = "content.published"
)

// RevisionCreatedEvent is emitted (sync) when a service revision is snapshotted.
type RevisionCreatedEvent struct {
	RevisionID uuid.UUID  `json:"revision_id"`
	EntityType string     `json:"entity_type"`
	EntityID   uuid.UUID  `json:"entity_id"`
	AuthorID   *uuid.UUID `json:"author_id,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Name implements events.Event.
func (RevisionCreatedEvent) Name() string { return EventRevisionCreated }

// ContentPublishedEvent is emitted (async) when a service becomes published.
type ContentPublishedEvent struct {
	EntityType  string    `json:"entity_type"`
	ServiceID   uuid.UUID `json:"service_id"`
	Slug        string    `json:"slug"`
	Title       string    `json:"title"`
	PublishedAt time.Time `json:"published_at"`
}

// Name implements events.Event.
func (ContentPublishedEvent) Name() string { return EventContentPublished }
