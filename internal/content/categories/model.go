// Package categories implements the Category taxonomy: a site-wide, hierarchical
// tree (self-referential parent) attached to posts via an M2M join. It mirrors
// the pages vertical for the hierarchy + cycle-prevention and the posts vertical
// for the M2M seam. All business logic lives in the service; data access is only
// through the repository interface; admin actions are gated by the `category`
// permission subject.
package categories

import (
	"time"

	"github.com/google/uuid"
)

// Category is the domain representation of a taxonomy category. ParentID is nil
// for a root category. Name/Description hold the DEFAULT-locale (en) content;
// per-locale variants live in the Translation overlay (M7b-3).
type Category struct {
	ID          uuid.UUID
	Name        string
	Slug        string
	Description string // sanitized rich text (kernel.SanitizeRichText on write)
	ParentID    *uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Translation is the per-locale CONTENT overlay for a category (M7b-3). It
// carries only the translatable text fields for a NON-default locale; the base
// category row holds the default-locale (en) content plus every structural field
// (slug, parent), which are shared across locales.
type Translation struct {
	Locale      string
	Name        string
	Description string // sanitized rich text (kernel.SanitizeRichText on every save)
}

// TreeNode is a category plus its rendered hierarchy depth, used by the admin
// indented list and the parent picker. Depth 0 is a root.
type TreeNode struct {
	Category Category
	Depth    int
}
