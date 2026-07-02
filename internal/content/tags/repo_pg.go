package tags

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

// RepoPG is the sqlc/pgx-backed tag Repository — the ONLY layer touching
// generated SQL for tags.
type RepoPG struct{ q *sqlcgen.Queries }

// NewRepoPG constructs a RepoPG over the base querier.
func NewRepoPG(q *sqlcgen.Queries) *RepoPG { return &RepoPG{q: q} }

// CreateTx inserts a tag within tx.
func (r *RepoPG) CreateTx(ctx context.Context, tx pgx.Tx, in CreateTagData) (Tag, error) {
	row, err := r.q.WithTx(tx).CreateTag(ctx, sqlcgen.CreateTagParams{
		Name: in.Name,
		Slug: in.Slug,
	})
	return tagFromRow(row), mapErr(err)
}

// UpdateTx updates a tag within tx.
func (r *RepoPG) UpdateTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, in UpdateTagData) (Tag, error) {
	row, err := r.q.WithTx(tx).UpdateTag(ctx, sqlcgen.UpdateTagParams{
		ID:   toPgUUID(id),
		Name: in.Name,
		Slug: in.Slug,
	})
	return tagFromRow(row), mapErr(err)
}

// DeleteTx hard-deletes a tag within tx. The post_tags rows cascade.
func (r *RepoPG) DeleteTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return mapErr(r.q.WithTx(tx).DeleteTag(ctx, toPgUUID(id)))
}

// GetByID loads a tag by id.
func (r *RepoPG) GetByID(ctx context.Context, id uuid.UUID) (Tag, error) {
	row, err := r.q.GetTagByID(ctx, toPgUUID(id))
	return tagFromRow(row), mapErr(err)
}

// GetBySlug loads a tag by slug.
func (r *RepoPG) GetBySlug(ctx context.Context, slug string) (Tag, error) {
	row, err := r.q.GetTagBySlug(ctx, slug)
	return tagFromRow(row), mapErr(err)
}

// SlugTaken reports whether slug is used by a tag other than excludeID.
func (r *RepoPG) SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	n, err := r.q.CountTagsBySlug(ctx, sqlcgen.CountTagsBySlugParams{
		Slug: slug,
		ID:   toPgUUID(excludeID),
	})
	return n > 0, mapErr(err)
}

// ListAll returns every tag (name-ordered).
func (r *RepoPG) ListAll(ctx context.Context) ([]Tag, error) {
	rows, err := r.q.ListAllTags(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	return tagsFromRows(rows), nil
}

// List returns a page of tags.
func (r *RepoPG) List(ctx context.Context, limit, offset int) ([]Tag, error) {
	rows, err := r.q.ListTags(ctx, sqlcgen.ListTagsParams{
		Limit:  int32(limitOrDefault(limit)),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return tagsFromRows(rows), nil
}

// Count returns the total number of tags.
func (r *RepoPG) Count(ctx context.Context) (int, error) {
	n, err := r.q.CountTags(ctx)
	return int(n), mapErr(err)
}

// AttachTx idempotently links a tag to a post within tx.
func (r *RepoPG) AttachTx(ctx context.Context, tx pgx.Tx, postID, tagID uuid.UUID) error {
	return mapErr(r.q.WithTx(tx).AttachPostTag(ctx, sqlcgen.AttachPostTagParams{
		PostID: toPgUUID(postID),
		TagID:  toPgUUID(tagID),
	}))
}

// DetachAllTx removes every tag association for a post within tx.
func (r *RepoPG) DetachAllTx(ctx context.Context, tx pgx.Tx, postID uuid.UUID) error {
	return mapErr(r.q.WithTx(tx).DetachAllPostTags(ctx, toPgUUID(postID)))
}

// ListForPost returns the tags attached to a post.
func (r *RepoPG) ListForPost(ctx context.Context, postID uuid.UUID) ([]Tag, error) {
	rows, err := r.q.ListTagsForPost(ctx, toPgUUID(postID))
	if err != nil {
		return nil, mapErr(err)
	}
	return tagsFromRows(rows), nil
}

// IDsForPost returns just the attached tag ids.
func (r *RepoPG) IDsForPost(ctx context.Context, postID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.q.ListTagsForPost(ctx, toPgUUID(postID))
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromPgUUID(row.ID))
	}
	return out, nil
}

// ListPublishedPostIDsInTag returns published post ids in a tag.
func (r *RepoPG) ListPublishedPostIDsInTag(ctx context.Context, tagID uuid.UUID, limit, offset int) ([]uuid.UUID, error) {
	rows, err := r.q.ListPublishedPostsInTag(ctx, sqlcgen.ListPublishedPostsInTagParams{
		TagID:  toPgUUID(tagID),
		Limit:  int32(limitOrDefault(limit)),
		Offset: int32(offset),
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

// CountPublishedPostsInTag returns the published post total in a tag.
func (r *RepoPG) CountPublishedPostsInTag(ctx context.Context, tagID uuid.UUID) (int, error) {
	n, err := r.q.CountPublishedPostsInTag(ctx, toPgUUID(tagID))
	return int(n), mapErr(err)
}

// --- per-locale content overlay (M7b-3) -------------------------------------

// UpsertTranslationTx inserts or updates a NON-default locale's translation row.
func (r *RepoPG) UpsertTranslationTx(ctx context.Context, tx pgx.Tx, tagID uuid.UUID, t Translation) error {
	_, err := r.q.WithTx(tx).UpsertTagTranslation(ctx, sqlcgen.UpsertTagTranslationParams{
		TagID:  toPgUUID(tagID),
		Locale: t.Locale,
		Name:   t.Name,
	})
	return mapErr(err)
}

// GetTranslation returns one locale's translation row.
func (r *RepoPG) GetTranslation(ctx context.Context, tagID uuid.UUID, locale string) (Translation, error) {
	row, err := r.q.GetTagTranslation(ctx, sqlcgen.GetTagTranslationParams{
		TagID:  toPgUUID(tagID),
		Locale: locale,
	})
	if err != nil {
		return Translation{}, mapErr(err)
	}
	return tagTranslationFromRow(row), nil
}

// ListTranslations returns every translation row for a tag.
func (r *RepoPG) ListTranslations(ctx context.Context, tagID uuid.UUID) ([]Translation, error) {
	rows, err := r.q.ListTagTranslations(ctx, toPgUUID(tagID))
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]Translation, 0, len(rows))
	for _, row := range rows {
		out = append(out, tagTranslationFromRow(row))
	}
	return out, nil
}

// TranslatedLocales returns the locales that already have a translation row.
func (r *RepoPG) TranslatedLocales(ctx context.Context, tagID uuid.UUID) ([]string, error) {
	locales, err := r.q.ListTagTranslationLocales(ctx, toPgUUID(tagID))
	return locales, mapErr(err)
}

// DeleteTranslationTx removes a locale's translation row within tx.
func (r *RepoPG) DeleteTranslationTx(ctx context.Context, tx pgx.Tx, tagID uuid.UUID, locale string) error {
	return mapErr(r.q.WithTx(tx).DeleteTagTranslation(ctx, sqlcgen.DeleteTagTranslationParams{
		TagID:  toPgUUID(tagID),
		Locale: locale,
	}))
}

// GetInLocaleByID loads a tag overlaid by locale (base fallback for name).
func (r *RepoPG) GetInLocaleByID(ctx context.Context, id uuid.UUID, locale string) (Tag, error) {
	row, err := r.q.GetTagInLocaleByID(ctx, sqlcgen.GetTagInLocaleByIDParams{
		ID:     toPgUUID(id),
		Locale: locale,
	})
	return tagFromRow(row), mapErr(err)
}

// GetPublishedInLocaleBySlug loads a tag by slug overlaid by locale.
func (r *RepoPG) GetPublishedInLocaleBySlug(ctx context.Context, slug, locale string) (Tag, error) {
	row, err := r.q.GetPublishedTagInLocaleBySlug(ctx, sqlcgen.GetPublishedTagInLocaleBySlugParams{
		Slug:   slug,
		Locale: locale,
	})
	return tagFromRow(row), mapErr(err)
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

func fromPgUUID(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return id.Bytes
}

func tagFromRow(t sqlcgen.Tag) Tag {
	return Tag{
		ID:        fromPgUUID(t.ID),
		Name:      t.Name,
		Slug:      t.Slug,
		CreatedAt: t.CreatedAt.Time,
		UpdatedAt: t.UpdatedAt.Time,
	}
}

func tagTranslationFromRow(t sqlcgen.TagTranslation) Translation {
	return Translation{
		Locale: t.Locale,
		Name:   t.Name,
	}
}

func tagsFromRows(rows []sqlcgen.Tag) []Tag {
	out := make([]Tag, 0, len(rows))
	for _, row := range rows {
		out = append(out, tagFromRow(row))
	}
	return out
}
