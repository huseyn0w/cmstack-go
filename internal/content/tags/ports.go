package tags

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrNotFound is the sentinel the repository returns when a tag is absent.
var ErrNotFound = errors.New("tags: not found")

// CreateTagData is the fully-prepared row the repo inserts.
type CreateTagData struct {
	Name string
	Slug string
}

// UpdateTagData is the fully-prepared row the repo writes on update.
type UpdateTagData struct {
	Name string
	Slug string
}

// Repository is the data-access contract for tags. It is the ONLY layer
// permitted to touch sqlc/pgx for tags. The M2M attach/detach methods accept a
// pgx.Tx so the post write and its tag associations commit atomically.
type Repository interface {
	CreateTx(ctx context.Context, tx pgx.Tx, in CreateTagData) (Tag, error)
	UpdateTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, in UpdateTagData) (Tag, error)
	DeleteTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error

	GetByID(ctx context.Context, id uuid.UUID) (Tag, error)
	GetBySlug(ctx context.Context, slug string) (Tag, error)

	// SlugTaken reports whether slug belongs to a tag OTHER than excludeID.
	SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error)

	ListAll(ctx context.Context) ([]Tag, error)
	List(ctx context.Context, limit, offset int) ([]Tag, error)
	Count(ctx context.Context) (int, error)

	// --- M2M (posts) ---------------------------------------------------------

	AttachTx(ctx context.Context, tx pgx.Tx, postID, tagID uuid.UUID) error
	DetachAllTx(ctx context.Context, tx pgx.Tx, postID uuid.UUID) error
	ListForPost(ctx context.Context, postID uuid.UUID) ([]Tag, error)
	IDsForPost(ctx context.Context, postID uuid.UUID) ([]uuid.UUID, error)
	ListPublishedPostIDsInTag(ctx context.Context, tagID uuid.UUID, limit, offset int) ([]uuid.UUID, error)
	CountPublishedPostsInTag(ctx context.Context, tagID uuid.UUID) (int, error)
}

// Authorizer answers (action, subject) permission questions for a user.
type Authorizer interface {
	Can(ctx context.Context, userID uuid.UUID, action, subject string) bool
}
