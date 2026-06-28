package services

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

// RepoPG is the sqlc/pgx-backed service Repository — the ONLY layer touching
// generated SQL for services and their FAQs.
type RepoPG struct{ q *sqlcgen.Queries }

// NewRepoPG constructs a RepoPG over the base querier.
func NewRepoPG(q *sqlcgen.Queries) *RepoPG { return &RepoPG{q: q} }

// CreateTx inserts a service within tx.
func (r *RepoPG) CreateTx(ctx context.Context, tx pgx.Tx, in CreateServiceData) (Service, error) {
	row, err := r.q.WithTx(tx).CreateService(ctx, sqlcgen.CreateServiceParams{
		Title:       in.Title,
		Slug:        in.Slug,
		Summary:     in.Summary,
		Body:        in.Body,
		Price:       in.Price,
		AreaServed:  in.AreaServed,
		Status:      in.Status.String(),
		PublishedAt: optTime(in.PublishedAt),
		ReadingTime: int32(in.ReadingTime),
	})
	return serviceFromRow(row), mapErr(err)
}

// UpdateTx updates an active service within tx.
func (r *RepoPG) UpdateTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, in UpdateServiceData) (Service, error) {
	row, err := r.q.WithTx(tx).UpdateService(ctx, sqlcgen.UpdateServiceParams{
		ID:          toPgUUID(id),
		Title:       in.Title,
		Slug:        in.Slug,
		Summary:     in.Summary,
		Body:        in.Body,
		Price:       in.Price,
		AreaServed:  in.AreaServed,
		Status:      in.Status.String(),
		PublishedAt: optTime(in.PublishedAt),
		ReadingTime: int32(in.ReadingTime),
	})
	return serviceFromRow(row), mapErr(err)
}

// GetByID loads any service (including trashed) by id.
func (r *RepoPG) GetByID(ctx context.Context, id uuid.UUID) (Service, error) {
	row, err := r.q.GetServiceByID(ctx, toPgUUID(id))
	return serviceFromRow(row), mapErr(err)
}

// GetActiveByID loads a non-trashed service by id.
func (r *RepoPG) GetActiveByID(ctx context.Context, id uuid.UUID) (Service, error) {
	row, err := r.q.GetActiveServiceByID(ctx, toPgUUID(id))
	return serviceFromRow(row), mapErr(err)
}

// GetPublishedBySlug loads a published, non-trashed service by slug.
func (r *RepoPG) GetPublishedBySlug(ctx context.Context, slug string) (Service, error) {
	row, err := r.q.GetPublishedServiceBySlug(ctx, slug)
	return serviceFromRow(row), mapErr(err)
}

// SlugTaken reports whether slug is used by a service other than excludeID.
func (r *RepoPG) SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	n, err := r.q.CountServicesBySlug(ctx, sqlcgen.CountServicesBySlugParams{
		Slug: slug,
		ID:   toPgUUID(excludeID),
	})
	return n > 0, mapErr(err)
}

// List returns a filtered, paginated active listing.
func (r *RepoPG) List(ctx context.Context, f ListFilter) ([]Service, error) {
	rows, err := r.q.ListServices(ctx, sqlcgen.ListServicesParams{
		Limit:  int32(limitOrDefault(f.Limit)),
		Offset: int32(f.Offset),
		Status: statusFilter(f.Status),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return servicesFromRows(rows), nil
}

// Count returns the total matching the filter (ignoring pagination).
func (r *RepoPG) Count(ctx context.Context, f ListFilter) (int, error) {
	n, err := r.q.CountServices(ctx, statusFilter(f.Status))
	return int(n), mapErr(err)
}

// ListTrashed returns a page of trashed services.
func (r *RepoPG) ListTrashed(ctx context.Context, limit, offset int) ([]Service, error) {
	rows, err := r.q.ListTrashedServices(ctx, sqlcgen.ListTrashedServicesParams{
		Limit:  int32(limitOrDefault(limit)),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return servicesFromRows(rows), nil
}

// CountTrashed returns the trashed total.
func (r *RepoPG) CountTrashed(ctx context.Context) (int, error) {
	n, err := r.q.CountTrashedServices(ctx)
	return int(n), mapErr(err)
}

// ListPublished returns a page of published services.
func (r *RepoPG) ListPublished(ctx context.Context, limit, offset int) ([]Service, error) {
	rows, err := r.q.ListPublishedServices(ctx, sqlcgen.ListPublishedServicesParams{
		Limit:  int32(limitOrDefault(limit)),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return servicesFromRows(rows), nil
}

// CountPublished returns the published total.
func (r *RepoPG) CountPublished(ctx context.Context) (int, error) {
	n, err := r.q.CountPublishedServices(ctx)
	return int(n), mapErr(err)
}

// TrashTx soft-deletes within tx.
func (r *RepoPG) TrashTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return mapErr(r.q.WithTx(tx).TrashService(ctx, toPgUUID(id)))
}

// RestoreTx un-trashes within tx.
func (r *RepoPG) RestoreTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return mapErr(r.q.WithTx(tx).RestoreService(ctx, toPgUUID(id)))
}

// PermanentDeleteTx hard-deletes within tx (FAQs cascade).
func (r *RepoPG) PermanentDeleteTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return mapErr(r.q.WithTx(tx).PermanentDeleteService(ctx, toPgUUID(id)))
}

// ListFAQs returns a service's FAQs ordered by position.
func (r *RepoPG) ListFAQs(ctx context.Context, serviceID uuid.UUID) ([]FAQ, error) {
	rows, err := r.q.ListServiceFAQs(ctx, toPgUUID(serviceID))
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]FAQ, 0, len(rows))
	for _, row := range rows {
		out = append(out, faqFromRow(row))
	}
	return out, nil
}

// ReplaceFAQsTx deletes the service's existing FAQs and inserts the supplied
// list in order, within tx. This is the simple, race-free strategy: the whole
// FAQ block is rewritten atomically with the service update.
func (r *RepoPG) ReplaceFAQsTx(ctx context.Context, tx pgx.Tx, serviceID uuid.UUID, faqs []FAQData) error {
	q := r.q.WithTx(tx)
	if err := q.DeleteServiceFAQs(ctx, toPgUUID(serviceID)); err != nil {
		return mapErr(err)
	}
	for _, f := range faqs {
		if _, err := q.CreateServiceFAQ(ctx, sqlcgen.CreateServiceFAQParams{
			ServiceID: toPgUUID(serviceID),
			Question:  f.Question,
			Answer:    f.Answer,
			Position:  int32(f.Position),
		}); err != nil {
			return mapErr(err)
		}
	}
	return nil
}

// RevisionRepoPG is the sqlc-backed kernel.RevisionRepository for services
// (entity_type='service').
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

func serviceFromRow(s sqlcgen.Service) Service {
	return Service{
		ID:          fromPgUUID(s.ID),
		Title:       s.Title,
		Slug:        s.Slug,
		Summary:     s.Summary,
		Body:        s.Body,
		Price:       s.Price,
		AreaServed:  s.AreaServed,
		Status:      kernel.Status(s.Status),
		PublishedAt: fromTimestamptz(s.PublishedAt),
		ReadingTime: int(s.ReadingTime),
		DeletedAt:   fromTimestamptz(s.DeletedAt),
		CreatedAt:   s.CreatedAt.Time,
		UpdatedAt:   s.UpdatedAt.Time,
	}
}

func servicesFromRows(rows []sqlcgen.Service) []Service {
	out := make([]Service, 0, len(rows))
	for _, row := range rows {
		out = append(out, serviceFromRow(row))
	}
	return out
}

func faqFromRow(f sqlcgen.ServiceFaq) FAQ {
	return FAQ{
		ID:        fromPgUUID(f.ID),
		ServiceID: fromPgUUID(f.ServiceID),
		Question:  f.Question,
		Answer:    f.Answer,
		Position:  int(f.Position),
	}
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
