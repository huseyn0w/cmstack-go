package search

import "context"

// Repository is the data-access contract for search. It is the ONLY layer
// permitted to touch sqlc/pgx for search. It exposes both strategies (FTS and
// the ILIKE fallback) as separate methods so the service owns the strategy
// choice; the repo never interpolates the query string into SQL — the term is a
// bound parameter in every method.
type Repository interface {
	// FTS runs websearch_to_tsquery matching across published, non-trashed
	// posts/pages/services, ranked by ts_rank (+ type/recency tie-break),
	// paginated by limit/offset.
	FTS(ctx context.Context, query string, limit, offset int) ([]Hit, error)
	// CountFTS returns the total FTS match count for the query.
	CountFTS(ctx context.Context, query string) (int, error)
	// ILIKE runs the parameterized substring fallback across the same scope.
	// term is the raw search term; the repo wraps it as '%'||term||'%'.
	ILIKE(ctx context.Context, term string, limit, offset int) ([]Hit, error)
	// CountILIKE returns the total substring-match count for the term.
	CountILIKE(ctx context.Context, term string) (int, error)
}
