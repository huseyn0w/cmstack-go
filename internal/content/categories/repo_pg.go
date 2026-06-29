package categories

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/huseyn0w/cmstack-go/internal/platform/db/sqlcgen"
)

// compile-time assertion that the pg repo satisfies the domain interface.
var _ Repository = (*RepoPG)(nil)

// RepoPG is the sqlc/pgx-backed category Repository — the ONLY layer touching
// generated SQL for categories.
type RepoPG struct{ q *sqlcgen.Queries }

// NewRepoPG constructs a RepoPG over the base querier.
func NewRepoPG(q *sqlcgen.Queries) *RepoPG { return &RepoPG{q: q} }

// CreateTx inserts a category within tx.
func (r *RepoPG) CreateTx(ctx context.Context, tx pgx.Tx, in CreateCategoryData) (Category, error) {
	row, err := r.q.WithTx(tx).CreateCategory(ctx, sqlcgen.CreateCategoryParams{
		Name:        in.Name,
		Slug:        in.Slug,
		Description: in.Description,
		ParentID:    optUUID(in.ParentID),
	})
	return categoryFromRow(row), mapErr(err)
}

// UpdateTx updates a category within tx.
func (r *RepoPG) UpdateTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, in UpdateCategoryData) (Category, error) {
	row, err := r.q.WithTx(tx).UpdateCategory(ctx, sqlcgen.UpdateCategoryParams{
		ID:          toPgUUID(id),
		Name:        in.Name,
		Slug:        in.Slug,
		Description: in.Description,
		ParentID:    optUUID(in.ParentID),
	})
	return categoryFromRow(row), mapErr(err)
}

// DeleteTx hard-deletes a category within tx. The post_categories rows cascade
// (FK ON DELETE CASCADE); children are detached (parent_id ON DELETE SET NULL).
func (r *RepoPG) DeleteTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return mapErr(r.q.WithTx(tx).DeleteCategory(ctx, toPgUUID(id)))
}

// GetByID loads a category by id.
func (r *RepoPG) GetByID(ctx context.Context, id uuid.UUID) (Category, error) {
	row, err := r.q.GetCategoryByID(ctx, toPgUUID(id))
	return categoryFromRow(row), mapErr(err)
}

// GetBySlug loads a category by slug.
func (r *RepoPG) GetBySlug(ctx context.Context, slug string) (Category, error) {
	row, err := r.q.GetCategoryBySlug(ctx, slug)
	return categoryFromRow(row), mapErr(err)
}

// SlugTaken reports whether slug is used by a category other than excludeID.
func (r *RepoPG) SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	n, err := r.q.CountCategoriesBySlug(ctx, sqlcgen.CountCategoriesBySlugParams{
		Slug: slug,
		ID:   toPgUUID(excludeID),
	})
	return n > 0, mapErr(err)
}

// ListAll returns every category (name-ordered).
func (r *RepoPG) ListAll(ctx context.Context) ([]Category, error) {
	rows, err := r.q.ListAllCategories(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	return categoriesFromRows(rows), nil
}

// ListChildren returns the children of a parent.
func (r *RepoPG) ListChildren(ctx context.Context, parentID uuid.UUID) ([]Category, error) {
	rows, err := r.q.ListChildCategories(ctx, optUUID(&parentID))
	if err != nil {
		return nil, mapErr(err)
	}
	return categoriesFromRows(rows), nil
}

// List returns a page of categories.
func (r *RepoPG) List(ctx context.Context, limit, offset int) ([]Category, error) {
	rows, err := r.q.ListCategories(ctx, sqlcgen.ListCategoriesParams{
		Limit:  int32(limitOrDefault(limit)),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return categoriesFromRows(rows), nil
}

// Count returns the total number of categories.
func (r *RepoPG) Count(ctx context.Context) (int, error) {
	n, err := r.q.CountCategories(ctx)
	return int(n), mapErr(err)
}

// AttachTx idempotently links a category to a post within tx.
func (r *RepoPG) AttachTx(ctx context.Context, tx pgx.Tx, postID, categoryID uuid.UUID) error {
	return mapErr(r.q.WithTx(tx).AttachPostCategory(ctx, sqlcgen.AttachPostCategoryParams{
		PostID:     toPgUUID(postID),
		CategoryID: toPgUUID(categoryID),
	}))
}

// DetachAllTx removes every category association for a post within tx.
func (r *RepoPG) DetachAllTx(ctx context.Context, tx pgx.Tx, postID uuid.UUID) error {
	return mapErr(r.q.WithTx(tx).DetachAllPostCategories(ctx, toPgUUID(postID)))
}

// ListForPost returns the categories attached to a post.
func (r *RepoPG) ListForPost(ctx context.Context, postID uuid.UUID) ([]Category, error) {
	rows, err := r.q.ListCategoriesForPost(ctx, toPgUUID(postID))
	if err != nil {
		return nil, mapErr(err)
	}
	return categoriesFromRows(rows), nil
}

// IDsForPost returns just the attached category ids.
func (r *RepoPG) IDsForPost(ctx context.Context, postID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.q.ListCategoriesForPost(ctx, toPgUUID(postID))
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromPgUUID(row.ID))
	}
	return out, nil
}

// ListPublishedPostIDsInCategory returns published post ids in a category.
func (r *RepoPG) ListPublishedPostIDsInCategory(ctx context.Context, categoryID uuid.UUID, limit, offset int) ([]uuid.UUID, error) {
	rows, err := r.q.ListPublishedPostsInCategory(ctx, sqlcgen.ListPublishedPostsInCategoryParams{
		CategoryID: toPgUUID(categoryID),
		Limit:      int32(limitOrDefault(limit)),
		Offset:     int32(offset),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]uuid.UUID, 0, len(rows))
	for _, p := range rows {
		out = append(out, fromPgUUID(p.ID))
	}
	return out, nil
}

// CountPublishedPostsInCategory returns the published post total in a category.
func (r *RepoPG) CountPublishedPostsInCategory(ctx context.Context, categoryID uuid.UUID) (int, error) {
	n, err := r.q.CountPublishedPostsInCategory(ctx, toPgUUID(categoryID))
	return int(n), mapErr(err)
}

// --- conversions -------------------------------------------------------------

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func limitOrDefault(limit int) int {
	if limit <= 0 {
		return 20
	}
	return limit
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

func categoryFromRow(c sqlcgen.Category) Category {
	return Category{
		ID:          fromPgUUID(c.ID),
		Name:        c.Name,
		Slug:        c.Slug,
		Description: c.Description,
		ParentID:    fromPgUUIDPtr(c.ParentID),
		CreatedAt:   c.CreatedAt.Time,
		UpdatedAt:   c.UpdatedAt.Time,
	}
}

func categoriesFromRows(rows []sqlcgen.Category) []Category {
	out := make([]Category, 0, len(rows))
	for _, row := range rows {
		out = append(out, categoryFromRow(row))
	}
	return out
}
