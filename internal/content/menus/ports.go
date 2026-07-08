package menus

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Sentinel errors the repository returns; the service maps them to domain
// outcomes and handlers turn them into HTTP status codes.
var (
	// ErrNotFound is returned when a menu or item is absent.
	ErrNotFound = errors.New("menus: not found")
	// ErrLocationTaken is returned when assigning a location that another menu
	// already occupies (the partial-unique index rejects the write, SQLSTATE
	// 23505).
	ErrLocationTaken = errors.New("menus: location already assigned to another menu")
)

// CreateItemData is the fully-prepared item row the repo inserts. The service has
// already validated the type, trimmed the label, and (for internal types) been
// handed the resolved URL/ref by the caller.
type CreateItemData struct {
	MenuID   uuid.UUID
	ParentID *uuid.UUID
	Position int
	Type     ItemType
	RefID    *uuid.UUID
	URL      string
	Label    string
}

// UpdateItemData is the fully-prepared item row the repo writes on update
// (position is managed separately via SetPositions/Reorder).
type UpdateItemData struct {
	ParentID *uuid.UUID
	Type     ItemType
	RefID    *uuid.UUID
	URL      string
	Label    string
}

// Repository is the data-access contract for menus. It is the ONLY layer
// permitted to touch sqlc/pgx for menus.
type Repository interface {
	// --- menus -------------------------------------------------------------
	CreateMenu(ctx context.Context, name, location string) (Menu, error)
	GetMenu(ctx context.Context, id uuid.UUID) (Menu, error)
	ListMenus(ctx context.Context) ([]Menu, error)
	// UpdateMenu writes name+location; a location collision maps to ErrLocationTaken.
	UpdateMenu(ctx context.Context, id uuid.UUID, name, location string) (Menu, error)
	DeleteMenu(ctx context.Context, id uuid.UUID) error
	// MenuByLocation returns the single menu assigned to location (or ErrNotFound).
	MenuByLocation(ctx context.Context, location string) (Menu, error)

	// --- items -------------------------------------------------------------
	AddItem(ctx context.Context, in CreateItemData) (Item, error)
	UpdateItem(ctx context.Context, id uuid.UUID, in UpdateItemData) (Item, error)
	DeleteItem(ctx context.Context, id uuid.UUID) error
	// ListItems returns a menu's items ordered by position (base labels).
	ListItems(ctx context.Context, menuID uuid.UUID) ([]Item, error)
	// SetPositions assigns position = index for each id in orderedIDs, in one tx.
	SetPositions(ctx context.Context, menuID uuid.UUID, orderedIDs []uuid.UUID) error
	// ListItemsInLocale returns a menu's items ordered by position with each
	// item's LABEL overlaid by locale (base fallback). All other fields structural.
	ListItemsInLocale(ctx context.Context, menuID uuid.UUID, locale string) ([]Item, error)

	// --- per-locale label overlay ------------------------------------------
	UpsertItemTranslation(ctx context.Context, itemID uuid.UUID, locale, label string) error
	ListItemTranslations(ctx context.Context, itemID uuid.UUID) ([]ItemTranslation, error)
	ItemTranslatedLocales(ctx context.Context, itemID uuid.UUID) ([]string, error)
}

// Authorizer answers (action, subject) permission questions for a user.
type Authorizer interface {
	Can(ctx context.Context, userID uuid.UUID, action, subject string) bool
}

// Beginner is the minimal transaction starter the service needs for the
// multi-statement reorder. *pgxpool.Pool satisfies it (via platform/db.Beginner).
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}
