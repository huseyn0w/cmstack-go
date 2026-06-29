package media

import (
	"time"

	"github.com/google/uuid"
)

// EventMediaUploaded fires ASYNCHRONOUSLY after a media asset is successfully
// uploaded and persisted. Its listeners are cache/search/CDN-warm seams drained
// from the outbox by the worker (mirrors content.published).
const EventMediaUploaded = "media.uploaded"

// UploadedEvent is emitted (async) when a media asset is uploaded. It carries
// enough for a downstream listener to act without re-reading the row.
type UploadedEvent struct {
	MediaID    uuid.UUID `json:"media_id"`
	StorageKey string    `json:"storage_key"`
	MIME       string    `json:"mime"`
	SizeBytes  int64     `json:"size_bytes"`
	UploadedBy uuid.UUID `json:"uploaded_by"`
	UploadedAt time.Time `json:"uploaded_at"`
}

// Name implements events.Event.
func (UploadedEvent) Name() string { return EventMediaUploaded }
