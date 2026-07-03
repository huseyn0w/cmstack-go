package settings

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/platform/db/sqlcgen"
)

// compile-time assertion that the pg repo satisfies the domain interface.
var _ Repo = (*RepoPG)(nil)

// RepoPG is the sqlc/pgx-backed settings Repo — the ONLY layer touching
// generated SQL for the site_settings table.
type RepoPG struct{ q *sqlcgen.Queries }

// NewRepoPG constructs a RepoPG over the base querier.
func NewRepoPG(q *sqlcgen.Queries) *RepoPG { return &RepoPG{q: q} }

// Get returns the stored value for key, mapping pgx's no-rows to ErrNotFound.
func (r *RepoPG) Get(ctx context.Context, key string) (string, error) {
	v, err := r.q.GetSetting(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return v, nil
}

// Set upserts value under key (INSERT ... ON CONFLICT DO UPDATE).
func (r *RepoPG) Set(ctx context.Context, key, value string) error {
	return r.q.UpsertSetting(ctx, sqlcgen.UpsertSettingParams{Key: key, Value: value})
}

// All returns every stored key/value pair.
func (r *RepoPG) All(ctx context.Context) (map[string]string, error) {
	rows, err := r.q.ListSettings(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.Key] = row.Value
	}
	return out, nil
}
