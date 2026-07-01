package search

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/huseyn0w/cmstack-go/internal/platform/db/sqlcgen"
)

// compile-time assertion that RepoPG satisfies the domain interface.
var _ Repository = (*RepoPG)(nil)

// RepoPG is the sqlc/pgx-backed search Repository — the ONLY layer touching
// generated SQL for search.
type RepoPG struct{ q *sqlcgen.Queries }

// NewRepoPG constructs a RepoPG over the base querier.
func NewRepoPG(q *sqlcgen.Queries) *RepoPG { return &RepoPG{q: q} }

// FTS runs the weighted websearch_to_tsquery match, ranked + paginated.
func (r *RepoPG) FTS(ctx context.Context, query string, limit, offset int) ([]Hit, error) {
	rows, err := r.q.SearchFTS(ctx, sqlcgen.SearchFTSParams{
		Query:  query,
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(rows))
	for _, row := range rows {
		hits = append(hits, Hit{
			Type:        HitType(row.ResultType),
			ID:          fromPgUUID(row.ID),
			Title:       row.Title,
			Slug:        row.Slug,
			Snippet:     string(row.Snippet),
			PublishedAt: fromTimestamptz(row.PublishedAt),
			Rank:        float64(row.Rank),
		})
	}
	return hits, nil
}

// CountFTS returns the total FTS match count for the query.
func (r *RepoPG) CountFTS(ctx context.Context, query string) (int, error) {
	n, err := r.q.CountSearchFTS(ctx, query)
	return int(n), err
}

// ILIKE runs the parameterized substring fallback. The term is wrapped as
// '%'||term||'%' HERE (as a bound parameter) — never interpolated into SQL.
func (r *RepoPG) ILIKE(ctx context.Context, term string, limit, offset int) ([]Hit, error) {
	rows, err := r.q.SearchILIKE(ctx, sqlcgen.SearchILIKEParams{
		Pattern: likePattern(term),
		Limit:   int32(limit),
		Offset:  int32(offset),
	})
	if err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(rows))
	for _, row := range rows {
		hits = append(hits, Hit{
			Type:        HitType(row.ResultType),
			ID:          fromPgUUID(row.ID),
			Title:       row.Title,
			Slug:        row.Slug,
			Snippet:     row.Snippet,
			PublishedAt: fromTimestamptz(row.PublishedAt),
			Rank:        row.Rank,
		})
	}
	return hits, nil
}

// CountILIKE returns the total substring-match count for the term.
func (r *RepoPG) CountILIKE(ctx context.Context, term string) (int, error) {
	n, err := r.q.CountSearchILIKE(ctx, likePattern(term))
	return int(n), err
}

// likePattern wraps a raw term in the ILIKE wildcards, escaping the LIKE
// metacharacters (% _ \) so a user term like "50%" matches literally rather
// than as a wildcard. Postgres LIKE uses backslash as the default escape char.
func likePattern(term string) string {
	var b []byte
	b = append(b, '%')
	for i := 0; i < len(term); i++ {
		switch term[i] {
		case '\\', '%', '_':
			b = append(b, '\\', term[i])
		default:
			b = append(b, term[i])
		}
	}
	b = append(b, '%')
	return string(b)
}

func fromPgUUID(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return id.Bytes
}

func fromTimestamptz(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}
