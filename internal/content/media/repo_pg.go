package media

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/huseyn0w/cmstack-go/internal/platform/db/sqlcgen"
)

// compile-time assertion that RepoPG satisfies the domain Repository.
var _ Repository = (*RepoPG)(nil)

// RepoPG is the sqlc/pgx-backed media Repository — the ONLY layer touching
// generated SQL for media.
type RepoPG struct{ q *sqlcgen.Queries }

// NewRepoPG constructs a RepoPG over the base querier.
func NewRepoPG(q *sqlcgen.Queries) *RepoPG { return &RepoPG{q: q} }

// CreateTx inserts a media row within tx.
func (r *RepoPG) CreateTx(ctx context.Context, tx pgx.Tx, in CreateMediaData) (Media, error) {
	row, err := r.q.WithTx(tx).CreateMedia(ctx, sqlcgen.CreateMediaParams{
		StorageKey:       in.StorageKey,
		OriginalFilename: in.OriginalFilename,
		Mime:             in.MIME,
		SizeBytes:        in.SizeBytes,
		Width:            optInt32(in.Width),
		Height:           optInt32(in.Height),
		Alt:              in.Alt,
		Title:            in.Title,
		Caption:          in.Caption,
		UploadedBy:       toPgUUID(in.UploadedBy),
	})
	return mediaFromRow(row), mapErr(err)
}

// CreateThumbnailTx inserts (or upserts) a thumbnail variant within tx.
func (r *RepoPG) CreateThumbnailTx(ctx context.Context, tx pgx.Tx, in CreateThumbnailData) (Thumbnail, error) {
	row, err := r.q.WithTx(tx).CreateThumbnail(ctx, sqlcgen.CreateThumbnailParams{
		MediaID:    toPgUUID(in.MediaID),
		Variant:    in.Variant,
		StorageKey: in.StorageKey,
		Width:      int32(in.Width),
		Height:     int32(in.Height),
	})
	return thumbnailFromRow(row), mapErr(err)
}

// GetByID loads a media asset (with its thumbnails) by id.
func (r *RepoPG) GetByID(ctx context.Context, id uuid.UUID) (Media, error) {
	row, err := r.q.GetMediaByID(ctx, toPgUUID(id))
	if err != nil {
		return Media{}, mapErr(err)
	}
	m := mediaFromRow(row)
	thumbs, err := r.ThumbnailsForMedia(ctx, id)
	if err != nil {
		return Media{}, err
	}
	m.Thumbnails = thumbs
	return m, nil
}

// List returns a page of media (newest first), each hydrated with its
// thumbnails via a single batched lookup (no N+1).
func (r *RepoPG) List(ctx context.Context, limit, offset int) ([]Media, error) {
	rows, err := r.q.ListMedia(ctx, sqlcgen.ListMediaParams{
		Limit:  int32(limitOrDefault(limit)),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]Media, 0, len(rows))
	ids := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		m := mediaFromRow(row)
		out = append(out, m)
		ids = append(ids, row.ID)
	}
	if len(ids) == 0 {
		return out, nil
	}

	thumbRows, err := r.q.ListThumbnailsForMediaIDs(ctx, ids)
	if err != nil {
		return nil, mapErr(err)
	}
	byMedia := make(map[uuid.UUID][]Thumbnail, len(out))
	for _, tr := range thumbRows {
		t := thumbnailFromRow(tr)
		byMedia[t.MediaID] = append(byMedia[t.MediaID], t)
	}
	for i := range out {
		out[i].Thumbnails = byMedia[out[i].ID]
	}
	return out, nil
}

// Count returns the total number of media rows.
func (r *RepoPG) Count(ctx context.Context) (int, error) {
	n, err := r.q.CountMedia(ctx)
	return int(n), mapErr(err)
}

// UpdateMetadata writes the editable alt/title/caption fields and returns the
// refreshed row (with thumbnails).
func (r *RepoPG) UpdateMetadata(ctx context.Context, id uuid.UUID, alt, title, caption string) (Media, error) {
	row, err := r.q.UpdateMediaMetadata(ctx, sqlcgen.UpdateMediaMetadataParams{
		ID:      toPgUUID(id),
		Alt:     alt,
		Title:   title,
		Caption: caption,
	})
	if err != nil {
		return Media{}, mapErr(err)
	}
	m := mediaFromRow(row)
	thumbs, err := r.ThumbnailsForMedia(ctx, id)
	if err != nil {
		return Media{}, err
	}
	m.Thumbnails = thumbs
	return m, nil
}

// Delete removes the media row. The media_thumbnails rows are removed by the
// ON DELETE CASCADE FK; the service deletes the STORAGE objects separately
// (it must enumerate variants before the row is gone).
func (r *RepoPG) Delete(ctx context.Context, id uuid.UUID) error {
	return mapErr(r.q.DeleteMedia(ctx, toPgUUID(id)))
}

// ThumbnailsForMedia returns a single asset's variants.
func (r *RepoPG) ThumbnailsForMedia(ctx context.Context, mediaID uuid.UUID) ([]Thumbnail, error) {
	rows, err := r.q.ListThumbnailsForMedia(ctx, toPgUUID(mediaID))
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]Thumbnail, 0, len(rows))
	for _, row := range rows {
		out = append(out, thumbnailFromRow(row))
	}
	return out, nil
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
		return 24
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

func optInt32(v *int) *int32 {
	if v == nil {
		return nil
	}
	out := int32(*v)
	return &out
}

func fromOptInt32(v *int32) *int {
	if v == nil {
		return nil
	}
	out := int(*v)
	return &out
}

func mediaFromRow(m sqlcgen.Medium) Media {
	return Media{
		ID:               fromPgUUID(m.ID),
		StorageKey:       m.StorageKey,
		OriginalFilename: m.OriginalFilename,
		MIME:             m.Mime,
		SizeBytes:        m.SizeBytes,
		Width:            fromOptInt32(m.Width),
		Height:           fromOptInt32(m.Height),
		Alt:              m.Alt,
		Title:            m.Title,
		Caption:          m.Caption,
		UploadedBy:       fromPgUUID(m.UploadedBy),
		CreatedAt:        m.CreatedAt.Time,
		UpdatedAt:        m.UpdatedAt.Time,
	}
}

func thumbnailFromRow(t sqlcgen.MediaThumbnail) Thumbnail {
	return Thumbnail{
		ID:         fromPgUUID(t.ID),
		MediaID:    fromPgUUID(t.MediaID),
		Variant:    t.Variant,
		StorageKey: t.StorageKey,
		Width:      int(t.Width),
		Height:     int(t.Height),
	}
}
