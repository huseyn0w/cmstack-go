package menus

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/db"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/db/sqlcgen"
)

// compile-time assertion that the pg repo satisfies the domain interface.
var _ Repository = (*RepoPG)(nil)

// RepoPG is the sqlc/pgx-backed menus Repository — the ONLY layer touching
// generated SQL for menus. It holds the pool for the one multi-statement write
// (SetPositions) that must run in a single transaction.
type RepoPG struct {
	pool db.Beginner
	q    *sqlcgen.Queries
}

// NewRepoPG constructs a RepoPG over the pool and the base querier.
func NewRepoPG(pool db.Beginner, q *sqlcgen.Queries) *RepoPG {
	return &RepoPG{pool: pool, q: q}
}

// --- menus -------------------------------------------------------------------

// CreateMenu inserts a menu.
func (r *RepoPG) CreateMenu(ctx context.Context, name, location string) (Menu, error) {
	row, err := r.q.CreateMenu(ctx, sqlcgen.CreateMenuParams{Name: name, Location: location})
	if err != nil {
		return Menu{}, mapMenuErr(err)
	}
	return menuFromRow(row), nil
}

// GetMenu loads a menu by id.
func (r *RepoPG) GetMenu(ctx context.Context, id uuid.UUID) (Menu, error) {
	row, err := r.q.GetMenu(ctx, toPgUUID(id))
	if err != nil {
		return Menu{}, mapMenuErr(err)
	}
	return menuFromRow(row), nil
}

// ListMenus returns every menu (name-ordered).
func (r *RepoPG) ListMenus(ctx context.Context) ([]Menu, error) {
	rows, err := r.q.ListMenus(ctx)
	if err != nil {
		return nil, mapMenuErr(err)
	}
	out := make([]Menu, 0, len(rows))
	for _, row := range rows {
		out = append(out, menuFromRow(row))
	}
	return out, nil
}

// UpdateMenu writes name+location, mapping a location collision to ErrLocationTaken.
func (r *RepoPG) UpdateMenu(ctx context.Context, id uuid.UUID, name, location string) (Menu, error) {
	row, err := r.q.UpdateMenu(ctx, sqlcgen.UpdateMenuParams{
		ID:       toPgUUID(id),
		Name:     name,
		Location: location,
	})
	if err != nil {
		return Menu{}, mapMenuErr(err)
	}
	return menuFromRow(row), nil
}

// DeleteMenu removes a menu (items + translations cascade).
func (r *RepoPG) DeleteMenu(ctx context.Context, id uuid.UUID) error {
	return mapMenuErr(r.q.DeleteMenu(ctx, toPgUUID(id)))
}

// MenuByLocation returns the single menu assigned to location.
func (r *RepoPG) MenuByLocation(ctx context.Context, location string) (Menu, error) {
	row, err := r.q.GetMenuByLocation(ctx, location)
	if err != nil {
		return Menu{}, mapMenuErr(err)
	}
	return menuFromRow(row), nil
}

// --- items -------------------------------------------------------------------

// AddItem inserts a menu item.
func (r *RepoPG) AddItem(ctx context.Context, in CreateItemData) (Item, error) {
	row, err := r.q.CreateMenuItem(ctx, sqlcgen.CreateMenuItemParams{
		MenuID:   toPgUUID(in.MenuID),
		ParentID: optUUID(in.ParentID),
		Position: int32(in.Position),
		Type:     in.Type.String(),
		RefID:    optUUID(in.RefID),
		Url:      in.URL,
		Label:    in.Label,
	})
	if err != nil {
		return Item{}, mapMenuErr(err)
	}
	return itemFromRow(row), nil
}

// UpdateItem writes an item's structural fields + label (position unchanged).
func (r *RepoPG) UpdateItem(ctx context.Context, id uuid.UUID, in UpdateItemData) (Item, error) {
	row, err := r.q.UpdateMenuItem(ctx, sqlcgen.UpdateMenuItemParams{
		ID:       toPgUUID(id),
		ParentID: optUUID(in.ParentID),
		Type:     in.Type.String(),
		RefID:    optUUID(in.RefID),
		Url:      in.URL,
		Label:    in.Label,
	})
	if err != nil {
		return Item{}, mapMenuErr(err)
	}
	return itemFromRow(row), nil
}

// DeleteItem removes an item (children + translations cascade).
func (r *RepoPG) DeleteItem(ctx context.Context, id uuid.UUID) error {
	return mapMenuErr(r.q.DeleteMenuItem(ctx, toPgUUID(id)))
}

// ListItems returns a menu's items ordered by position (base labels).
func (r *RepoPG) ListItems(ctx context.Context, menuID uuid.UUID) ([]Item, error) {
	rows, err := r.q.ListMenuItems(ctx, toPgUUID(menuID))
	if err != nil {
		return nil, mapMenuErr(err)
	}
	out := make([]Item, 0, len(rows))
	for _, row := range rows {
		out = append(out, itemFromRow(row))
	}
	return out, nil
}

// SetPositions assigns position = index for each id in orderedIDs in one tx.
func (r *RepoPG) SetPositions(ctx context.Context, _ uuid.UUID, orderedIDs []uuid.UUID) error {
	return db.RunInTx(ctx, r.pool, func(ctx context.Context, tx pgx.Tx) error {
		qtx := r.q.WithTx(tx)
		for i, id := range orderedIDs {
			if err := qtx.SetMenuItemPosition(ctx, sqlcgen.SetMenuItemPositionParams{
				ID:       toPgUUID(id),
				Position: int32(i),
			}); err != nil {
				return mapMenuErr(err)
			}
		}
		return nil
	})
}

// ListItemsInLocale returns a menu's items ordered by position with each label
// overlaid by locale (base fallback); all other fields structural.
func (r *RepoPG) ListItemsInLocale(ctx context.Context, menuID uuid.UUID, locale string) ([]Item, error) {
	rows, err := r.q.ListMenuItemsInLocale(ctx, sqlcgen.ListMenuItemsInLocaleParams{
		MenuID: toPgUUID(menuID),
		Locale: locale,
	})
	if err != nil {
		return nil, mapMenuErr(err)
	}
	out := make([]Item, 0, len(rows))
	for _, row := range rows {
		out = append(out, itemFromRow(row))
	}
	return out, nil
}

// --- per-locale label overlay ------------------------------------------------

// UpsertItemTranslation inserts or updates an item's per-locale label.
func (r *RepoPG) UpsertItemTranslation(ctx context.Context, itemID uuid.UUID, locale, label string) error {
	return mapMenuErr(r.q.UpsertMenuItemTranslation(ctx, sqlcgen.UpsertMenuItemTranslationParams{
		ItemID: toPgUUID(itemID),
		Locale: locale,
		Label:  label,
	}))
}

// ListItemTranslations returns every per-locale label for an item.
func (r *RepoPG) ListItemTranslations(ctx context.Context, itemID uuid.UUID) ([]ItemTranslation, error) {
	rows, err := r.q.ListMenuItemTranslations(ctx, toPgUUID(itemID))
	if err != nil {
		return nil, mapMenuErr(err)
	}
	out := make([]ItemTranslation, 0, len(rows))
	for _, row := range rows {
		out = append(out, ItemTranslation{Locale: row.Locale, Label: row.Label})
	}
	return out, nil
}

// ItemTranslatedLocales returns the locales that already have a label for an item.
func (r *RepoPG) ItemTranslatedLocales(ctx context.Context, itemID uuid.UUID) ([]string, error) {
	locales, err := r.q.ListMenuItemTranslationLocales(ctx, toPgUUID(itemID))
	return locales, mapMenuErr(err)
}

// --- conversions -------------------------------------------------------------

// mapMenuErr normalizes pgx/pg errors to the module's sentinels: no-rows ->
// ErrNotFound, unique-violation on the location index -> ErrLocationTaken.
func mapMenuErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrLocationTaken
	}
	return err
}

func menuFromRow(m sqlcgen.Menu) Menu {
	return Menu{
		ID:        fromPgUUID(m.ID),
		Name:      m.Name,
		Location:  m.Location,
		CreatedAt: m.CreatedAt.Time,
		UpdatedAt: m.UpdatedAt.Time,
	}
}

func itemFromRow(i sqlcgen.MenuItem) Item {
	return Item{
		ID:        fromPgUUID(i.ID),
		MenuID:    fromPgUUID(i.MenuID),
		ParentID:  fromPgUUIDPtr(i.ParentID),
		Position:  int(i.Position),
		Type:      ItemType(i.Type),
		RefID:     fromPgUUIDPtr(i.RefID),
		URL:       i.Url,
		Label:     i.Label,
		CreatedAt: i.CreatedAt.Time,
		UpdatedAt: i.UpdatedAt.Time,
	}
}

func toPgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

func optUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return toPgUUID(*id)
}

func fromPgUUID(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return id.Bytes
}

func fromPgUUIDPtr(id pgtype.UUID) *uuid.UUID {
	if !id.Valid {
		return nil
	}
	v := uuid.UUID(id.Bytes)
	return &v
}
