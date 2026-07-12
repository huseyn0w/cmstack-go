package posts

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/events"
)

// ErrNotFound is the sentinel the repository returns when a post is absent. The
// service maps it to domain outcomes; handlers turn it into a 404.
var ErrNotFound = errors.New("posts: not found")

// ListFilter narrows an admin post listing. A nil pointer means "no constraint";
// an empty CategorySlug/TagSlug/Q means "no constraint on that axis" too. Set
// filters combine as an intersection (AND).
type ListFilter struct {
	Status         *kernel.Status
	AuthorID       *uuid.UUID
	IncludeTrashed bool
	Limit          int
	Offset         int
	// CategorySlug narrows to posts assigned the category with this slug.
	CategorySlug string
	// TagSlug narrows to posts assigned the tag with this slug.
	TagSlug string
	// Q is a free-text filter matched against the post title/excerpt (ILIKE).
	Q string
}

// CreatePostData is the fully-prepared row the repo inserts. The service has
// already sanitized the body, computed reading time, deduped the slug, and
// resolved the publish/schedule timestamps.
type CreatePostData struct {
	Title           string
	Slug            string
	Excerpt         string
	Body            string
	Status          kernel.Status
	PublishedAt     *time.Time
	ScheduledAt     *time.Time
	AuthorID        uuid.UUID
	ReadingTime     int
	MetaTitle       string
	MetaDescription string
	CanonicalURL    string
	NoIndex         bool
}

// UpdatePostData is the fully-prepared row the repo writes on update. Like
// CreatePostData, every field is already validated/derived by the service.
type UpdatePostData struct {
	Title           string
	Slug            string
	Excerpt         string
	Body            string
	Status          kernel.Status
	PublishedAt     *time.Time
	ScheduledAt     *time.Time
	ReadingTime     int
	MetaTitle       string
	MetaDescription string
	CanonicalURL    string
	NoIndex         bool
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

	// SitemapItems returns a lightweight enumeration of every published,
	// non-trashed post (slug/title/description/updated_at only) for the M8
	// crawler routes. It loads no body/heavy fields.
	SitemapItems(ctx context.Context) ([]kernel.SitemapItem, error)
	ListPublishedByAuthor(ctx context.Context, authorID uuid.UUID) ([]Post, error)

	// ListPublishedFiltered returns published posts narrowed by optional,
	// combinable category/tag slug filters (M3). An empty slug means "no
	// constraint on that axis"; both set is an intersection. Drafts/trashed are
	// always excluded.
	ListPublishedFiltered(ctx context.Context, categorySlug, tagSlug string, limit, offset int) ([]Post, error)
	CountPublishedFiltered(ctx context.Context, categorySlug, tagSlug string) (int, error)

	// ListRelatedPublished returns up to limit published posts sharing >=1
	// category or tag with postID (excluding self), most-related first (M3).
	ListRelatedPublished(ctx context.Context, postID uuid.UUID, limit int) ([]Post, error)

	// GetPublishedByIDs loads the published, non-trashed posts among ids,
	// preserving the given id order (the order the taxonomy archive computed).
	GetPublishedByIDs(ctx context.Context, ids []uuid.UUID) ([]Post, error)

	TrashTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
	RestoreTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
	PermanentDeleteTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error

	ListDueScheduledIDs(ctx context.Context, now time.Time) ([]uuid.UUID, error)

	// --- per-locale content overlay (M7b-1) ---------------------------------

	// UpsertTranslationTx inserts or updates the translation row for a NON-default
	// locale within tx (en content lives on the base post row). Body is sanitized
	// by the service before it reaches here.
	UpsertTranslationTx(ctx context.Context, tx pgx.Tx, postID uuid.UUID, t Translation) error
	// GetTranslation returns one locale's translation row, or ErrNotFound.
	GetTranslation(ctx context.Context, postID uuid.UUID, locale string) (Translation, error)
	// ListTranslations returns every translation row for a post (all locales).
	ListTranslations(ctx context.Context, postID uuid.UUID) ([]Translation, error)
	// TranslatedLocales returns the set of locales that already have a translation
	// row for the post (drives the editor's "has translation" tab markers).
	TranslatedLocales(ctx context.Context, postID uuid.UUID) ([]string, error)
	// DeleteTranslationTx removes a locale's translation row within tx.
	DeleteTranslationTx(ctx context.Context, tx pgx.Tx, postID uuid.UUID, locale string) error
	// GetActiveInLocaleByID loads an active post by id with title/excerpt/body
	// overlaid by locale's translation (empty/absent field -> base fallback).
	GetActiveInLocaleByID(ctx context.Context, id uuid.UUID, locale string) (Post, error)
	// GetPublishedInLocaleBySlug loads a published post by slug overlaid by locale.
	GetPublishedInLocaleBySlug(ctx context.Context, slug, locale string) (Post, error)
	// ListPublishedInLocale returns a page of published posts overlaid by locale.
	ListPublishedInLocale(ctx context.Context, locale string, limit, offset int) ([]Post, error)

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

// TaxonomyAssigner replaces a post's full set of category/tag associations
// within an EXISTING transaction (the post write tx). *categories.Service and
// *tags.Service satisfy the two single-axis methods via the adapter in the web
// wiring; the post service drives both inside one tx so the post row and its
// taxonomy commit atomically (M3 seam). A nil assigner means "taxonomy is not
// wired" (e.g. reduced-deps tests) and post writes proceed without it.
type TaxonomyAssigner interface {
	// AssignCategoriesTx replaces the post's categories with categoryIDs.
	AssignCategoriesTx(ctx context.Context, tx pgx.Tx, postID uuid.UUID, categoryIDs []uuid.UUID) error
	// AssignTagsTx replaces the post's tags with tagIDs.
	AssignTagsTx(ctx context.Context, tx pgx.Tx, postID uuid.UUID, tagIDs []uuid.UUID) error
}

// Publisher publishes a domain event inside a transaction. *events.Bus
// satisfies it.
type Publisher interface {
	Publish(ctx context.Context, tx pgx.Tx, event events.Event) error
}

// Clock returns the current time; injected so publish/schedule timestamps and
// the due-scan are deterministic in tests.
type Clock func() time.Time
