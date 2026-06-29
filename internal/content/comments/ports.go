package comments

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/platform/events"
)

// ErrNotFound is the sentinel the repository returns when a comment (or the
// referenced post) is absent. The service maps it to domain outcomes; handlers
// turn it into a 404.
var ErrNotFound = errors.New("comments: not found")

// CreateCommentData is the fully-prepared row the repo inserts. The service has
// already sanitized the body, resolved the author identity, and defaulted the
// status to PENDING.
type CreateCommentData struct {
	PostID       uuid.UUID
	ParentID     *uuid.UUID
	AuthorUserID *uuid.UUID
	AuthorName   string
	AuthorEmail  string
	AuthorIP     string
	Body         string
	Status       Status
}

// ModerationFilter narrows the admin moderation listing. A nil Status means "all
// statuses".
type ModerationFilter struct {
	Status *Status
	Limit  int
	Offset int
}

// StatusCount pairs a status with its row count (the moderation tab badges).
type StatusCount struct {
	Status Status
	Count  int
}

// Repository is the data-access contract for comments — the ONLY layer permitted
// to touch sqlc/pgx for comments. Transactional writes accept a pgx.Tx so a
// write and its in-tx side effects (outbox enqueue) commit atomically.
type Repository interface {
	CreateTx(ctx context.Context, tx pgx.Tx, in CreateCommentData) (Comment, error)

	GetByID(ctx context.Context, id uuid.UUID) (Comment, error)
	// GetApprovedByID loads an APPROVED comment on postID (the threading parent
	// check). Returns ErrNotFound when absent / not approved / wrong post.
	GetApprovedByID(ctx context.Context, id, postID uuid.UUID) (Comment, error)

	// ListApprovedForPost returns a post's APPROVED comments, oldest first (the
	// public thread source).
	ListApprovedForPost(ctx context.Context, postID uuid.UUID) ([]Comment, error)

	ListForModeration(ctx context.Context, f ModerationFilter) ([]Comment, error)
	CountForModeration(ctx context.Context, status *Status) (int, error)
	// CountsByStatus returns the per-status totals for the moderation tab badges.
	CountsByStatus(ctx context.Context) ([]StatusCount, error)

	UpdateStatusTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, status Status) (Comment, error)
	// UpdateBodyTx writes a self-edited body, sets edited_at, and re-opens
	// moderation by setting the new status (PENDING).
	UpdateBodyTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, body string, status Status) (Comment, error)
	DeleteTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
}

// PostLookup resolves the published post a comment targets. The post service
// satisfies it via a small adapter; the comment service depends only on this
// narrow contract.
type PostLookup interface {
	// PublishedBySlug returns the (id, title) of a published post by slug, or
	// ErrNotFound when no published post matches.
	PublishedBySlug(ctx context.Context, slug string) (PostRef, error)
	// AuthorEmail returns the email of the post's author for the notification
	// recipient. An empty string + nil error means "no author email available".
	AuthorEmail(ctx context.Context, postID uuid.UUID) (string, error)
}

// PostRef is the minimal published-post projection the comment service needs.
type PostRef struct {
	ID    uuid.UUID
	Slug  string
	Title string
}

// Authorizer answers (action, subject) permission questions. accounts.Authorizer
// satisfies it. Moderation routes gate on (update|delete, comment).
type Authorizer interface {
	Can(ctx context.Context, userID uuid.UUID, action, subject string) bool
}

// SpamChecker verifies an optional anti-spam token (reCAPTCHA v3). The platform
// recaptcha.Verifier satisfies it; when no secret is configured Verify returns
// (true, nil) so guest submission works without keys (graceful no-op).
type SpamChecker interface {
	Verify(ctx context.Context, token string) (bool, error)
}

// Publisher publishes a domain event inside a transaction. *events.Bus satisfies
// it.
type Publisher interface {
	Publish(ctx context.Context, tx pgx.Tx, event events.Event) error
}

// RateLimiter answers whether an action for a key (the client IP) may proceed,
// consuming a token. *ratelimit.Limiter satisfies it.
type RateLimiter interface {
	Allow(key string) bool
}
