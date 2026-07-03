package pages

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/platform/db/sqlcgen"
)

// compile-time assertions that the pg repos satisfy the domain interfaces.
var (
	_ Repository                = (*RepoPG)(nil)
	_ kernel.RevisionRepository = (*RevisionRepoPG)(nil)
)

// RepoPG is the sqlc/pgx-backed page Repository — the ONLY layer touching
// generated SQL for pages.
type RepoPG struct{ q *sqlcgen.Queries }

// NewRepoPG constructs a RepoPG over the base querier.
func NewRepoPG(q *sqlcgen.Queries) *RepoPG { return &RepoPG{q: q} }

// CreateTx inserts a page within tx.
func (r *RepoPG) CreateTx(ctx context.Context, tx pgx.Tx, in CreatePageData) (Page, error) {
	row, err := r.q.WithTx(tx).CreatePage(ctx, sqlcgen.CreatePageParams{
		Title:           in.Title,
		Slug:            in.Slug,
		Body:            in.Body,
		Status:          in.Status.String(),
		PublishedAt:     optTime(in.PublishedAt),
		ParentID:        optUUID(in.ParentID),
		Template:        in.Template,
		ReadingTime:     int32(in.ReadingTime),
		MetaTitle:       in.MetaTitle,
		MetaDescription: in.MetaDescription,
		CanonicalUrl:    in.CanonicalURL,
		Noindex:         in.NoIndex,
	})
	return pageFromRow(row), mapErr(err)
}

// UpdateTx updates an active page within tx.
func (r *RepoPG) UpdateTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, in UpdatePageData) (Page, error) {
	row, err := r.q.WithTx(tx).UpdatePage(ctx, sqlcgen.UpdatePageParams{
		ID:              toPgUUID(id),
		Title:           in.Title,
		Slug:            in.Slug,
		Body:            in.Body,
		Status:          in.Status.String(),
		PublishedAt:     optTime(in.PublishedAt),
		ParentID:        optUUID(in.ParentID),
		Template:        in.Template,
		ReadingTime:     int32(in.ReadingTime),
		MetaTitle:       in.MetaTitle,
		MetaDescription: in.MetaDescription,
		CanonicalUrl:    in.CanonicalURL,
		Noindex:         in.NoIndex,
	})
	return pageFromRow(row), mapErr(err)
}

// GetByID loads any page (including trashed) by id.
func (r *RepoPG) GetByID(ctx context.Context, id uuid.UUID) (Page, error) {
	row, err := r.q.GetPageByID(ctx, toPgUUID(id))
	return pageFromRow(row), mapErr(err)
}

// GetActiveByID loads a non-trashed page by id.
func (r *RepoPG) GetActiveByID(ctx context.Context, id uuid.UUID) (Page, error) {
	row, err := r.q.GetActivePageByID(ctx, toPgUUID(id))
	return pageFromRow(row), mapErr(err)
}

// GetPublishedBySlug loads a published, non-trashed page by slug.
func (r *RepoPG) GetPublishedBySlug(ctx context.Context, slug string) (Page, error) {
	row, err := r.q.GetPublishedPageBySlug(ctx, slug)
	return pageFromRow(row), mapErr(err)
}

// SlugTaken reports whether slug is used by a page other than excludeID.
func (r *RepoPG) SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	n, err := r.q.CountPagesBySlug(ctx, sqlcgen.CountPagesBySlugParams{
		Slug: slug,
		ID:   toPgUUID(excludeID),
	})
	return n > 0, mapErr(err)
}

// List returns a filtered, paginated active listing.
func (r *RepoPG) List(ctx context.Context, f ListFilter) ([]Page, error) {
	rows, err := r.q.ListPages(ctx, sqlcgen.ListPagesParams{
		Limit:  int32(limitOrDefault(f.Limit)),
		Offset: int32(f.Offset),
		Status: statusFilter(f.Status),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return pagesFromRows(rows), nil
}

// Count returns the total matching the filter (ignoring pagination).
func (r *RepoPG) Count(ctx context.Context, f ListFilter) (int, error) {
	n, err := r.q.CountPages(ctx, statusFilter(f.Status))
	return int(n), mapErr(err)
}

// ListAllActive returns every non-trashed page (title-ordered).
func (r *RepoPG) ListAllActive(ctx context.Context) ([]Page, error) {
	rows, err := r.q.ListAllActivePages(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	return pagesFromRows(rows), nil
}

// ListChildren returns the non-trashed children of a parent.
func (r *RepoPG) ListChildren(ctx context.Context, parentID uuid.UUID) ([]Page, error) {
	rows, err := r.q.ListChildPages(ctx, toPgUUID(parentID))
	if err != nil {
		return nil, mapErr(err)
	}
	return pagesFromRows(rows), nil
}

// ListTrashed returns a page of trashed pages.
func (r *RepoPG) ListTrashed(ctx context.Context, limit, offset int) ([]Page, error) {
	rows, err := r.q.ListTrashedPages(ctx, sqlcgen.ListTrashedPagesParams{
		Limit:  int32(limitOrDefault(limit)),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return pagesFromRows(rows), nil
}

// CountTrashed returns the trashed total.
func (r *RepoPG) CountTrashed(ctx context.Context) (int, error) {
	n, err := r.q.CountTrashedPages(ctx)
	return int(n), mapErr(err)
}

// ListPublished returns a page of published pages.
func (r *RepoPG) ListPublished(ctx context.Context, limit, offset int) ([]Page, error) {
	rows, err := r.q.ListPublishedPages(ctx, sqlcgen.ListPublishedPagesParams{
		Limit:  int32(limitOrDefault(limit)),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return pagesFromRows(rows), nil
}

// CountPublished returns the published total.
func (r *RepoPG) CountPublished(ctx context.Context) (int, error) {
	n, err := r.q.CountPublishedPages(ctx)
	return int(n), mapErr(err)
}

// TrashTx soft-deletes within tx.
func (r *RepoPG) TrashTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return mapErr(r.q.WithTx(tx).TrashPage(ctx, toPgUUID(id)))
}

// RestoreTx un-trashes within tx.
func (r *RepoPG) RestoreTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return mapErr(r.q.WithTx(tx).RestorePage(ctx, toPgUUID(id)))
}

// PermanentDeleteTx hard-deletes within tx.
func (r *RepoPG) PermanentDeleteTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return mapErr(r.q.WithTx(tx).PermanentDeletePage(ctx, toPgUUID(id)))
}

// RevisionRepoPG is the sqlc-backed kernel.RevisionRepository for pages. It is a
// thin reuse of the shared revisions table (entity_type='page').
type RevisionRepoPG struct{ q *sqlcgen.Queries }

// NewRevisionRepoPG constructs a RevisionRepoPG.
func NewRevisionRepoPG(q *sqlcgen.Queries) *RevisionRepoPG { return &RevisionRepoPG{q: q} }

// CreateTx persists a snapshot within tx.
func (r *RevisionRepoPG) CreateTx(ctx context.Context, tx pgx.Tx, in kernel.CreateRevisionInput) (kernel.Revision, error) {
	row, err := r.q.WithTx(tx).CreateRevision(ctx, sqlcgen.CreateRevisionParams{
		EntityType: in.EntityType,
		EntityID:   toPgUUID(in.EntityID),
		Snapshot:   in.Snapshot,
		AuthorID:   optUUID(in.AuthorID),
	})
	if err != nil {
		return kernel.Revision{}, mapErr(err)
	}
	return revisionFromRow(row), nil
}

// List returns an entity's revisions, newest first.
func (r *RevisionRepoPG) List(ctx context.Context, entityType string, entityID uuid.UUID) ([]kernel.Revision, error) {
	rows, err := r.q.ListRevisions(ctx, sqlcgen.ListRevisionsParams{
		EntityType: entityType,
		EntityID:   toPgUUID(entityID),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]kernel.Revision, 0, len(rows))
	for _, row := range rows {
		out = append(out, revisionFromRow(row))
	}
	return out, nil
}

// Get loads a single revision by id.
func (r *RevisionRepoPG) Get(ctx context.Context, id uuid.UUID) (kernel.Revision, error) {
	row, err := r.q.GetRevision(ctx, toPgUUID(id))
	if err != nil {
		return kernel.Revision{}, mapErr(err)
	}
	return revisionFromRow(row), nil
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

func statusFilter(s *kernel.Status) *string {
	if s == nil {
		return nil
	}
	v := s.String()
	return &v
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

func optTime(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func fromTimestamptz(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}

// --- per-locale content overlay (M7b-2) -------------------------------------

// UpsertTranslationTx inserts or updates a page's translation row for a non-default locale.
func (r *RepoPG) UpsertTranslationTx(ctx context.Context, tx pgx.Tx, pageID uuid.UUID, t Translation) error {
	_, err := r.q.WithTx(tx).UpsertPageTranslation(ctx, sqlcgen.UpsertPageTranslationParams{
		PageID:          toPgUUID(pageID),
		Locale:          t.Locale,
		Title:           t.Title,
		Body:            t.Body,
		MetaTitle:       t.MetaTitle,
		MetaDescription: t.MetaDescription,
	})
	return mapErr(err)
}

// GetTranslation returns one locale's translation row, or ErrNotFound.
func (r *RepoPG) GetTranslation(ctx context.Context, pageID uuid.UUID, locale string) (Translation, error) {
	row, err := r.q.GetPageTranslation(ctx, sqlcgen.GetPageTranslationParams{
		PageID: toPgUUID(pageID),
		Locale: locale,
	})
	if err != nil {
		return Translation{}, mapErr(err)
	}
	return pageTranslationFromRow(row), nil
}

// ListTranslations returns every translation row for a page.
func (r *RepoPG) ListTranslations(ctx context.Context, pageID uuid.UUID) ([]Translation, error) {
	rows, err := r.q.ListPageTranslations(ctx, toPgUUID(pageID))
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]Translation, 0, len(rows))
	for _, row := range rows {
		out = append(out, pageTranslationFromRow(row))
	}
	return out, nil
}

// TranslatedLocales returns the locales that already have a translation row for the page.
func (r *RepoPG) TranslatedLocales(ctx context.Context, pageID uuid.UUID) ([]string, error) {
	locales, err := r.q.ListPageTranslationLocales(ctx, toPgUUID(pageID))
	return locales, mapErr(err)
}

// DeleteTranslationTx removes a locale's translation row within tx.
func (r *RepoPG) DeleteTranslationTx(ctx context.Context, tx pgx.Tx, pageID uuid.UUID, locale string) error {
	return mapErr(r.q.WithTx(tx).DeletePageTranslation(ctx, sqlcgen.DeletePageTranslationParams{
		PageID: toPgUUID(pageID),
		Locale: locale,
	}))
}

// GetActiveInLocaleByID loads an active page overlaid by locale (per-field base fallback).
func (r *RepoPG) GetActiveInLocaleByID(ctx context.Context, id uuid.UUID, locale string) (Page, error) {
	row, err := r.q.GetActivePageInLocaleByID(ctx, sqlcgen.GetActivePageInLocaleByIDParams{
		ID:     toPgUUID(id),
		Locale: locale,
	})
	if err != nil {
		return Page{}, mapErr(err)
	}
	return Page{
		ID:              fromPgUUID(row.ID),
		Title:           row.Title,
		Slug:            row.Slug,
		Body:            row.Body,
		Status:          kernel.Status(row.Status),
		PublishedAt:     fromTimestamptz(row.PublishedAt),
		ParentID:        fromPgUUIDPtr(row.ParentID),
		Template:        row.Template,
		ReadingTime:     int(row.ReadingTime),
		MetaTitle:       row.MetaTitle,
		MetaDescription: row.MetaDescription,
		CanonicalURL:    row.CanonicalUrl,
		NoIndex:         row.Noindex,
		DeletedAt:       fromTimestamptz(row.DeletedAt),
		CreatedAt:       row.CreatedAt.Time,
		UpdatedAt:       row.UpdatedAt.Time,
	}, nil
}

// GetPublishedInLocaleBySlug loads a published page by slug overlaid by locale (base fallback).
func (r *RepoPG) GetPublishedInLocaleBySlug(ctx context.Context, slug, locale string) (Page, error) {
	row, err := r.q.GetPublishedPageInLocaleBySlug(ctx, sqlcgen.GetPublishedPageInLocaleBySlugParams{
		Slug:   slug,
		Locale: locale,
	})
	if err != nil {
		return Page{}, mapErr(err)
	}
	return Page{
		ID:              fromPgUUID(row.ID),
		Title:           row.Title,
		Slug:            row.Slug,
		Body:            row.Body,
		Status:          kernel.Status(row.Status),
		PublishedAt:     fromTimestamptz(row.PublishedAt),
		ParentID:        fromPgUUIDPtr(row.ParentID),
		Template:        row.Template,
		ReadingTime:     int(row.ReadingTime),
		MetaTitle:       row.MetaTitle,
		MetaDescription: row.MetaDescription,
		CanonicalURL:    row.CanonicalUrl,
		NoIndex:         row.Noindex,
		DeletedAt:       fromTimestamptz(row.DeletedAt),
		CreatedAt:       row.CreatedAt.Time,
		UpdatedAt:       row.UpdatedAt.Time,
	}, nil
}

func pageTranslationFromRow(t sqlcgen.PageTranslation) Translation {
	return Translation{
		Locale:          t.Locale,
		Title:           t.Title,
		Body:            t.Body,
		MetaTitle:       t.MetaTitle,
		MetaDescription: t.MetaDescription,
	}
}

func pageFromRow(p sqlcgen.Page) Page {
	return Page{
		ID:              fromPgUUID(p.ID),
		Title:           p.Title,
		Slug:            p.Slug,
		Body:            p.Body,
		Status:          kernel.Status(p.Status),
		PublishedAt:     fromTimestamptz(p.PublishedAt),
		ParentID:        fromPgUUIDPtr(p.ParentID),
		Template:        p.Template,
		ReadingTime:     int(p.ReadingTime),
		MetaTitle:       p.MetaTitle,
		MetaDescription: p.MetaDescription,
		CanonicalURL:    p.CanonicalUrl,
		NoIndex:         p.Noindex,
		DeletedAt:       fromTimestamptz(p.DeletedAt),
		CreatedAt:       p.CreatedAt.Time,
		UpdatedAt:       p.UpdatedAt.Time,
	}
}

func pagesFromRows(rows []sqlcgen.Page) []Page {
	out := make([]Page, 0, len(rows))
	for _, row := range rows {
		out = append(out, pageFromRow(row))
	}
	return out
}

func revisionFromRow(r sqlcgen.Revision) kernel.Revision {
	var author *uuid.UUID
	if r.AuthorID.Valid {
		id := fromPgUUID(r.AuthorID)
		author = &id
	}
	return kernel.Revision{
		ID:         fromPgUUID(r.ID),
		EntityType: r.EntityType,
		EntityID:   fromPgUUID(r.EntityID),
		Snapshot:   r.Snapshot,
		AuthorID:   author,
		CreatedAt:  r.CreatedAt.Time,
	}
}
