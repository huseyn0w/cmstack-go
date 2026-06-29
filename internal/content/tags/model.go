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

// Tag is the domain representation of a taxonomy tag. Name is single-locale now;
// a per-locale variant is an M7 seam (see migration 00007).
//
// TODO(M7 i18n): per-locale Name via tag_translations.
type Tag struct {
	ID        uuid.UUID
	Name      string
	Slug      string
	CreatedAt time.Time
	UpdatedAt time.Time
}
