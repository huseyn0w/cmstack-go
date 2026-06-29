// Package media implements the Media/Files content type: the M4 vertical slice
// over the storage abstraction. The service holds all logic (validation,
// thumbnail/dimension generation, storage orchestration); data access is only
// through the repository; storage access is only through the Storage interface;
// side effects are only emitted as events. Handlers are thin HTTP boundaries.
package media

import (
	"time"

	"github.com/google/uuid"
)

// Media is the domain representation of an uploaded asset. Width/Height are nil
// for non-raster assets (PDF). Thumbnails are the generated raster variants
// (empty for documents).
type Media struct {
	ID               uuid.UUID
	StorageKey       string
	OriginalFilename string
	MIME             string
	SizeBytes        int64
	Width            *int
	Height           *int
	Alt              string
	Title            string
	Caption          string
	UploadedBy       uuid.UUID
	CreatedAt        time.Time
	UpdatedAt        time.Time

	Thumbnails []Thumbnail
}

// Thumbnail is one generated variant of a Media original.
type Thumbnail struct {
	ID         uuid.UUID
	MediaID    uuid.UUID
	Variant    string
	StorageKey string
	Width      int
	Height     int
}

// IsImage reports whether the asset is a raster image (carries dimensions and
// may have thumbnails). It is true exactly when a width was probed.
func (m Media) IsImage() bool { return m.Width != nil && m.Height != nil }

// ThumbnailKey returns the storage key of the named variant, or "" if absent.
func (m Media) ThumbnailKey(variant string) string {
	for _, t := range m.Thumbnails {
		if t.Variant == variant {
			return t.StorageKey
		}
	}
	return ""
}

// DisplayTitle is the human label for the asset: the title when set, else the
// original filename, else a short id fragment.
func (m Media) DisplayTitle() string {
	if m.Title != "" {
		return m.Title
	}
	if m.OriginalFilename != "" {
		return m.OriginalFilename
	}
	return m.ID.String()
}
