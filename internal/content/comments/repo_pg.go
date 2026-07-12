package comments

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/db/sqlcgen"
)

// compile-time assertion that the pg repo satisfies the domain interface.
var _ Repository = (*RepoPG)(nil)

// RepoPG is the sqlc/pgx-backed comment Repository — the ONLY layer touching
// generated SQL for comments.
type RepoPG struct{ q *sqlcgen.Queries }

// NewRepoPG constructs a RepoPG over the base querier.
func NewRepoPG(q *sqlcgen.Queries) *RepoPG { return &RepoPG{q: q} }

// CreateTx inserts a comment within tx.
func (r *RepoPG) CreateTx(ctx context.Context, tx pgx.Tx, in CreateCommentData) (Comment, error) {
	row, err := r.q.WithTx(tx).CreateComment(ctx, sqlcgen.CreateCommentParams{
		PostID:       toPgUUID(in.PostID),
		ParentID:     optUUID(in.ParentID),
		AuthorUserID: optUUID(in.AuthorUserID),
		AuthorName:   in.AuthorName,
		AuthorEmail:  in.AuthorEmail,
		AuthorIp:     in.AuthorIP,
		Body:         in.Body,
		Status:       in.Status.String(),
	})
	return commentFromRow(row), mapErr(err)
}

// GetByID loads any comment by id.
func (r *RepoPG) GetByID(ctx context.Context, id uuid.UUID) (Comment, error) {
	row, err := r.q.GetCommentByID(ctx, toPgUUID(id))
	return commentFromRow(row), mapErr(err)
}

// GetApprovedByID loads an APPROVED comment on postID (the threading parent check).
func (r *RepoPG) GetApprovedByID(ctx context.Context, id, postID uuid.UUID) (Comment, error) {
	row, err := r.q.GetApprovedCommentByID(ctx, sqlcgen.GetApprovedCommentByIDParams{
		ID:     toPgUUID(id),
		PostID: toPgUUID(postID),
	})
	return commentFromRow(row), mapErr(err)
}

// ListApprovedForPost returns a post's APPROVED comments, oldest first.
func (r *RepoPG) ListApprovedForPost(ctx context.Context, postID uuid.UUID) ([]Comment, error) {
	rows, err := r.q.ListApprovedCommentsForPost(ctx, toPgUUID(postID))
	if err != nil {
		return nil, mapErr(err)
	}
	return commentsFromRows(rows), nil
}

// ListForModeration returns a filtered, paginated moderation listing.
func (r *RepoPG) ListForModeration(ctx context.Context, f ModerationFilter) ([]Comment, error) {
	rows, err := r.q.ListCommentsForModeration(ctx, sqlcgen.ListCommentsForModerationParams{
		Limit:  int32(limitOrDefault(f.Limit)),
		Offset: int32(f.Offset),
		Status: statusFilter(f.Status),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return commentsFromRows(rows), nil
}

// CountForModeration returns the total matching the status filter.
func (r *RepoPG) CountForModeration(ctx context.Context, status *Status) (int, error) {
	n, err := r.q.CountCommentsForModeration(ctx, statusFilter(status))
	return int(n), mapErr(err)
}

// CountsByStatus returns the per-status totals for the moderation tab badges.
func (r *RepoPG) CountsByStatus(ctx context.Context) ([]StatusCount, error) {
	rows, err := r.q.CountCommentsByStatus(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]StatusCount, 0, len(rows))
	for _, row := range rows {
		out = append(out, StatusCount{Status: ParseStatus(row.Status), Count: int(row.Total)})
	}
	return out, nil
}

// UpdateStatusTx sets a comment's moderation status within tx.
func (r *RepoPG) UpdateStatusTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, status Status) (Comment, error) {
	row, err := r.q.WithTx(tx).UpdateCommentStatus(ctx, sqlcgen.UpdateCommentStatusParams{
		ID:     toPgUUID(id),
		Status: status.String(),
	})
	return commentFromRow(row), mapErr(err)
}

// UpdateBodyTx writes a self-edited body (stamping edited_at) and the new status.
func (r *RepoPG) UpdateBodyTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, body string, status Status) (Comment, error) {
	row, err := r.q.WithTx(tx).UpdateCommentBody(ctx, sqlcgen.UpdateCommentBodyParams{
		ID:     toPgUUID(id),
		Body:   body,
		Status: status.String(),
	})
	return commentFromRow(row), mapErr(err)
}

// DeleteTx hard-deletes a comment within tx (replies cascade via FK).
func (r *RepoPG) DeleteTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return mapErr(r.q.WithTx(tx).DeleteComment(ctx, toPgUUID(id)))
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

func statusFilter(s *Status) *string {
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
	if id == nil || *id == uuid.Nil {
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

func optFromPgUUID(id pgtype.UUID) *uuid.UUID {
	if !id.Valid {
		return nil
	}
	v := uuid.UUID(id.Bytes)
	return &v
}

func commentFromRow(c sqlcgen.Comment) Comment {
	out := Comment{
		ID:           fromPgUUID(c.ID),
		PostID:       fromPgUUID(c.PostID),
		ParentID:     optFromPgUUID(c.ParentID),
		AuthorUserID: optFromPgUUID(c.AuthorUserID),
		AuthorName:   c.AuthorName,
		AuthorEmail:  c.AuthorEmail,
		AuthorIP:     c.AuthorIp,
		Body:         c.Body,
		Status:       ParseStatus(c.Status),
		CreatedAt:    c.CreatedAt.Time,
		UpdatedAt:    c.UpdatedAt.Time,
	}
	if c.EditedAt.Valid {
		t := c.EditedAt.Time
		out.EditedAt = &t
	}
	return out
}

func commentsFromRows(rows []sqlcgen.Comment) []Comment {
	out := make([]Comment, 0, len(rows))
	for _, row := range rows {
		out = append(out, commentFromRow(row))
	}
	return out
}
