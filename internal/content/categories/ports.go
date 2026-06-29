package categories

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrNotFound is the sentinel the repository returns when a category is absent.
var ErrNotFound = errors.New("categories: not found")

// CreateCategoryData is the fully-prepared row the repo inserts. The service has
// already sanitized the description, deduped the slug, and verified the parent.
type CreateCategoryData struct {
	Name        string
	Slug        string
	Description string
	ParentID    *uuid.UUID
}

// UpdateCategoryData is the fully-prepared row the repo writes on update.
type UpdateCategoryData struct {
	Name        string
	Slug        string
	Description string
	ParentID    *uuid.UUID
}

// Repository is the data-access contract for categories. It is the ONLY layer
// permitted to touch sqlc/pgx for categories. The M2M attach/detach methods
// accept a pgx.Tx so the post write and its category associations commit
// atomically (the post service drives them inside its own transaction).
type Repository interface {
	CreateTx(ctx context.Context, tx pgx.Tx, in CreateCategoryData) (Category, error)
	UpdateTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, in UpdateCategoryData) (Category, error)
	DeleteTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error

	GetByID(ctx context.Context, id uuid.UUID) (Category, error)
	GetBySlug(ctx context.Context, slug string) (Category, error)

	// SlugTaken reports whether slug belongs to a category OTHER than excludeID.
	SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error)

	// ListAll returns every category (name-ordered) — the set behind the tree and
	// the parent picker. The taxonomy is small relative to posts.
	ListAll(ctx context.Context) ([]Category, error)
	ListChildren(ctx context.Context, parentID uuid.UUID) ([]Category, error)
	List(ctx context.Context, limit, offset int) ([]Category, error)
	Count(ctx context.Context) (int, error)

	// --- M2M (posts) ---------------------------------------------------------

	// AttachTx idempotently links a category to a post within tx.
	AttachTx(ctx context.Context, tx pgx.Tx, postID, categoryID uuid.UUID) error
	// DetachAllTx removes every category association for a post within tx (used to
	// replace the full set on a post update).
	DetachAllTx(ctx context.Context, tx pgx.Tx, postID uuid.UUID) error
	// ListForPost returns the categories attached to a post (name-ordered).
	ListForPost(ctx context.Context, postID uuid.UUID) ([]Category, error)
	// IDsForPost returns just the attached category ids (for editor pre-selection).
	IDsForPost(ctx context.Context, postID uuid.UUID) ([]uuid.UUID, error)
	// ListPublishedPostIDsInCategory returns published post ids in a category,
	// paginated, plus the total.
	ListPublishedPostIDsInCategory(ctx context.Context, categoryID uuid.UUID, limit, offset int) ([]uuid.UUID, error)
	CountPublishedPostsInCategory(ctx context.Context, categoryID uuid.UUID) (int, error)
}

// Authorizer answers (action, subject) permission questions for a user.
type Authorizer interface {
	Can(ctx context.Context, userID uuid.UUID, action, subject string) bool
}
