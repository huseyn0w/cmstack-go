package posts

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/platform/events"
)

// ErrNotFound is the sentinel the repository returns when a post is absent. The
// service maps it to domain outcomes; handlers turn it into a 404.
var ErrNotFound = errors.New("posts: not found")

// ListFilter narrows an admin post listing. A nil pointer means "no constraint".
type ListFilter struct {
	Status         *kernel.Status
	AuthorID       *uuid.UUID
	IncludeTrashed bool
	Limit          int
	Offset         int
	// TODO(M3): full-text query `q` once search lands.
}

// CreatePostData is the fully-prepared row the repo inserts. The service has
// already sanitized the body, computed reading time, deduped the slug, and
// resolved the publish/schedule timestamps.
type CreatePostData struct {
	Title       string
	Slug        string
	Excerpt     string
	Body        string
	Status      kernel.Status
	PublishedAt *time.Time
	ScheduledAt *time.Time
	AuthorID    uuid.UUID
	ReadingTime int
}

// UpdatePostData is the fully-prepared row the repo writes on update. Like
// CreatePostData, every field is already validated/derived by the service.
type UpdatePostData struct {
	Title       string
	Slug        string
	Excerpt     string
	Body        string
	Status      kernel.Status
	PublishedAt *time.Time
	ScheduledAt *time.Time
	ReadingTime int
}

// Repository is the data-access contract for posts. It is the ONLY layer
// permitted to touch sqlc/pgx for posts; the service depends solely on this
// interface. Transactional writes accept a pgx.Tx so a write and its in-tx side
// effects (revision snapshot, outbox enqueue) commit atomically.
type Repository interface {
	CreateTx(ctx context.Context, tx pgx.Tx, in CreatePostData) (Post, error)
	UpdateTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, in UpdatePostData) (Post, error)

	GetByID(ctx context.Context, id uuid.UUID) (Post, error)
	GetActiveByID(ctx context.Context, id uuid.UUID) (Post, error)
	GetPublishedBySlug(ctx context.Context, slug string) (Post, error)

	// SlugTaken reports whether slug belongs to a post OTHER than excludeID.
	SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error)

	List(ctx context.Context, f ListFilter) ([]Post, error)
	Count(ctx context.Context, f ListFilter) (int, error)
	ListTrashed(ctx context.Context, limit, offset int) ([]Post, error)
	CountTrashed(ctx context.Context) (int, error)
	ListPublished(ctx context.Context, limit, offset int) ([]Post, error)
	CountPublished(ctx context.Context) (int, error)
	ListPublishedByAuthor(ctx context.Context, authorID uuid.UUID) ([]Post, error)

	TrashTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
	RestoreTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
	PermanentDeleteTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error

	ListDueScheduledIDs(ctx context.Context, now time.Time) ([]uuid.UUID, error)

	// LikeTx inserts a like (idempotent) and returns whether a row was added.
	LikeTx(ctx context.Context, tx pgx.Tx, postID, userID uuid.UUID) (added bool, err error)
	// UnlikeTx removes a like and returns whether a row was removed.
	UnlikeTx(ctx context.Context, tx pgx.Tx, postID, userID uuid.UUID) (removed bool, err error)
	// SyncLikeCountTx recomputes posts.like_count from post_likes within tx.
	SyncLikeCountTx(ctx context.Context, tx pgx.Tx, postID uuid.UUID) error
	HasLiked(ctx context.Context, postID, userID uuid.UUID) (bool, error)
}

// Authorizer answers (action, subject) permission questions for a user. The
// accounts.Authorizer satisfies it. Ownership is enforced ON TOP of this by the
// service (the grants are coarse; the service is the gate).
type Authorizer interface {
	Can(ctx context.Context, userID uuid.UUID, action, subject string) bool
}

// Publisher publishes a domain event inside a transaction. *events.Bus
// satisfies it.
type Publisher interface {
	Publish(ctx context.Context, tx pgx.Tx, event events.Event) error
}

// Clock returns the current time; injected so publish/schedule timestamps and
// the due-scan are deterministic in tests.
type Clock func() time.Time
