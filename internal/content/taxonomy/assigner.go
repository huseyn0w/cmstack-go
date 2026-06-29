// Package taxonomy wires the category and tag services together behind the
// posts.TaxonomyAssigner seam, so the post service can persist a post's full
// category + tag sets inside its own write transaction (M3). It owns no logic of
// its own — it just delegates each axis to the matching service's AssignTx.
package taxonomy

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// categoryAssigner is the narrow slice of *categories.Service this adapter needs.
type categoryAssigner interface {
	AssignTx(ctx context.Context, tx pgx.Tx, postID uuid.UUID, categoryIDs []uuid.UUID) error
}

// tagAssigner is the narrow slice of *tags.Service this adapter needs.
type tagAssigner interface {
	AssignTx(ctx context.Context, tx pgx.Tx, postID uuid.UUID, tagIDs []uuid.UUID) error
}

// Assigner satisfies posts.TaxonomyAssigner by delegating to the category and
// tag services. Each AssignTx runs inside the post service's transaction, so the
// post row and its associations commit atomically.
type Assigner struct {
	categories categoryAssigner
	tags       tagAssigner
}

// NewAssigner constructs the combined assigner from the two services.
func NewAssigner(categories categoryAssigner, tags tagAssigner) *Assigner {
	return &Assigner{categories: categories, tags: tags}
}

// AssignCategoriesTx replaces the post's category set within tx.
func (a *Assigner) AssignCategoriesTx(ctx context.Context, tx pgx.Tx, postID uuid.UUID, categoryIDs []uuid.UUID) error {
	return a.categories.AssignTx(ctx, tx, postID, categoryIDs)
}

// AssignTagsTx replaces the post's tag set within tx.
func (a *Assigner) AssignTagsTx(ctx context.Context, tx pgx.Tx, postID uuid.UUID, tagIDs []uuid.UUID) error {
	return a.tags.AssignTx(ctx, tx, postID, tagIDs)
}
