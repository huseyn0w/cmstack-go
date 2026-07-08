// Package menus implements navigation MENUS (M11-1): named menus assigned to a
// location (header/footer), with ordered, nestable items that reference internal
// content (post/page/category) or a custom URL, plus a per-locale label overlay.
// It is the data + service slice only — the admin builder UI and the public
// header/footer rendering are SEPARATE later slices.
//
// The base menu_items row holds the DEFAULT-locale label; a per-locale label for
// a NON-default locale lives in menu_item_translations and is read via the
// COALESCE overlay. Structural fields (type/ref/url/parent/position) are shared
// across locales and are NOT per-locale.
//
// To avoid cross-module coupling, the service does NOT itself load the referenced
// posts/pages/categories: the admin slice resolves the reference (slug -> url,
// title -> default label) BEFORE calling AddItem/UpdateItem, and the service only
// validates the type, trims the label, and persists what it is given.
package menus

import (
	"time"

	"github.com/google/uuid"
)

// Menu is a named navigation menu. Location is "" when the menu is unassigned; a
// non-empty location (e.g. "header"/"footer") is unique across menus (at most one
// menu per assigned location).
type Menu struct {
	ID        uuid.UUID
	Name      string
	Location  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ItemType is the kind of thing a menu item points at. Internal types (post/page/
// category) carry a RefID plus a resolved URL/label; custom carries a free URL.
type ItemType string

// The supported item types. Internal types reference stored content by id; custom
// is a free-form URL entered by the admin.
const (
	// ItemPost links to a post.
	ItemPost ItemType = "post"
	// ItemPage links to a page.
	ItemPage ItemType = "page"
	// ItemCategory links to a category archive.
	ItemCategory ItemType = "category"
	// ItemCustom is a free-form URL (internal rooted path or external absolute).
	ItemCustom ItemType = "custom"
)

// Valid reports whether t is one of the known item types.
func (t ItemType) Valid() bool {
	switch t {
	case ItemPost, ItemPage, ItemCategory, ItemCustom:
		return true
	default:
		return false
	}
}

// String returns the item type as a plain string for persistence.
func (t ItemType) String() string { return string(t) }

// Item is one entry in a menu. ParentID is nil for a top-level item; Position
// orders siblings. For internal types RefID identifies the referenced content and
// URL is its resolved rooted path; for custom, URL is the entered value. Label is
// the DEFAULT-locale label (overlaid per-locale via ItemTranslation).
type Item struct {
	ID        uuid.UUID
	MenuID    uuid.UUID
	ParentID  *uuid.UUID
	Position  int
	Type      ItemType
	RefID     *uuid.UUID
	URL       string
	Label     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ItemTranslation is a NON-default locale's label overlay for a menu item. Only
// the label is per-locale; every structural field stays on the base item row.
type ItemTranslation struct {
	Locale string
	Label  string
}

// ResolvedItem is the public render shape produced by ResolveForLocation: the
// label already overlaid by the active locale, the URL already localized (or left
// as-is for external links), and children nested by parent ordered by position.
type ResolvedItem struct {
	Label    string
	URL      string
	Children []ResolvedItem
}
