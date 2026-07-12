package apitoken

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/db/sqlcgen"
)

// compile-time assertion that the pg repo satisfies the domain port.
var _ Repo = (*RepoPG)(nil)

// RepoPG is the sqlc/pgx-backed api_tokens Repo — the ONLY layer touching
// generated SQL for the api_tokens table.
type RepoPG struct{ q *sqlcgen.Queries }

// NewRepoPG constructs a RepoPG over the base querier.
func NewRepoPG(q *sqlcgen.Queries) *RepoPG { return &RepoPG{q: q} }

// Create inserts a new token row and returns the stored metadata.
func (r *RepoPG) Create(ctx context.Context, in CreateParams) (Token, error) {
	row, err := r.q.CreateAPIToken(ctx, sqlcgen.CreateAPITokenParams{
		UserID:    toPgUUID(in.UserID),
		Name:      in.Name,
		TokenHash: in.TokenHash,
		LastFour:  in.LastFour,
		ExpiresAt: toPgTimestamp(in.ExpiresAt),
	})
	if err != nil {
		return Token{}, err
	}
	return mapToken(row), nil
}

// GetByHash returns the valid token for hash, mapping pgx's no-rows to
// ErrNotFound.
func (r *RepoPG) GetByHash(ctx context.Context, hash string) (Token, error) {
	row, err := r.q.GetAPITokenByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Token{}, ErrNotFound
		}
		return Token{}, err
	}
	return mapToken(row), nil
}

// Touch stamps last_used_at = now() for the token id.
func (r *RepoPG) Touch(ctx context.Context, id uuid.UUID) error {
	return r.q.TouchAPIToken(ctx, toPgUUID(id))
}

// ListByUser returns every token owned by userID, newest first.
func (r *RepoPG) ListByUser(ctx context.Context, userID uuid.UUID) ([]Token, error) {
	rows, err := r.q.ListAPITokensByUser(ctx, toPgUUID(userID))
	if err != nil {
		return nil, err
	}
	out := make([]Token, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapToken(row))
	}
	return out, nil
}

// Revoke marks the token revoked, scoped to its owner.
func (r *RepoPG) Revoke(ctx context.Context, id, userID uuid.UUID) error {
	return r.q.RevokeAPIToken(ctx, sqlcgen.RevokeAPITokenParams{
		ID:     toPgUUID(id),
		UserID: toPgUUID(userID),
	})
}

// mapToken converts a generated row into the domain Token.
func mapToken(row sqlcgen.ApiToken) Token {
	return Token{
		ID:         fromPgUUID(row.ID),
		UserID:     fromPgUUID(row.UserID),
		Name:       row.Name,
		LastFour:   row.LastFour,
		ExpiresAt:  fromPgTimestamp(row.ExpiresAt),
		LastUsedAt: fromPgTimestamp(row.LastUsedAt),
		RevokedAt:  fromPgTimestamp(row.RevokedAt),
		CreatedAt:  row.CreatedAt.Time,
	}
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

func toPgTimestamp(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func fromPgTimestamp(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	out := t.Time
	return &out
}
