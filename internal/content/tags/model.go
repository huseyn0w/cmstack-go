// Package tags implements the Tag taxonomy: a flat, site-wide set of labels
// attached to posts via an M2M join. It mirrors the posts vertical for the M2M
// seam and the kernel for slug/dedupe. All business logic lives in the service;
// data access is only through the repository interface; admin actions are gated
// by the `tag` permission subject.
package tags

import (
	"time"

	"github.com/google/uuid"
)

// Tag is the domain representation of a taxonomy tag. Name holds the
// DEFAULT-locale (en) content; per-locale variants live in the Translation
// overlay (M7b-3).
type Tag struct {
	ID        uuid.UUID
	Name      string
	Slug      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Translation is the per-locale CONTENT overlay for a tag (M7b-3). It carries
// only the translatable name for a NON-default locale; the base tag row holds the
// default-locale (en) name and the shared slug.
type Translation struct {
	Locale string
	Name   string
}
