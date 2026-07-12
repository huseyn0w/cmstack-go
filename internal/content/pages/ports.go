package pages

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/events"
)

// ErrNotFound is the sentinel the repository returns when a page is absent. The
// service maps it to domain outcomes; handlers turn it into a 404.
var ErrNotFound = errors.New("pages: not found")

// ListFilter narrows an admin page listing. A nil status means "no constraint".
type ListFilter struct {
	Status *kernel.Status
	Limit  int
	Offset int
}

// CreatePageData is the fully-prepared row the repo inserts. The service has
// already sanitized the body, computed reading time, deduped the slug, validated
// the template, checked the parent, and resolved the publish timestamp.
type CreatePageData struct {
	Title           string
	Slug            string
	Body            string
	Status          kernel.Status
	PublishedAt     *time.Time
	ParentID        *uuid.UUID
	Template        string
	ReadingTime     int
	MetaTitle       string
	MetaDescription string
	CanonicalURL    string
	NoIndex         bool
}

// UpdatePageData is the fully-prepared row the repo writes on update.
type UpdatePageData struct {
	Title           string
	Slug            string
	Body            string
	Status          kernel.Status
	PublishedAt     *time.Time
	ParentID        *uuid.UUID
	Template        string
	ReadingTime     int
	MetaTitle       string
	MetaDescription string
	CanonicalURL    string
	NoIndex         bool
}

// Repository is the data-access contract for pages. It is the ONLY layer
// permitted to touch sqlc/pgx for pages. Transactional writes accept a pgx.Tx so
// the write and its in-tx side effects (revision snapshot, outbox enqueue) commit
// atomically.
type Repository interface {
	CreateTx(ctx context.Context, tx pgx.Tx, in CreatePageData) (Page, error)
	UpdateTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, in UpdatePageData) (Page, error)

	GetByID(ctx context.Context, id uuid.UUID) (Page, error)
	GetActiveByID(ctx context.Context, id uuid.UUID) (Page, error)
	GetPublishedBySlug(ctx context.Context, slug string) (Page, error)

	// SlugTaken reports whether slug belongs to a page OTHER than excludeID.
	SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error)

	List(ctx context.Context, f ListFilter) ([]Page, error)
	Count(ctx context.Context, f ListFilter) (int, error)
	// ListAllActive returns every non-trashed page (for the hierarchy tree and
	// the parent-picker options); the set is small relative to posts.
	ListAllActive(ctx context.Context) ([]Page, error)
	ListChildren(ctx context.Context, parentID uuid.UUID) ([]Page, error)
	ListTrashed(ctx context.Context, limit, offset int) ([]Page, error)
	CountTrashed(ctx context.Context) (int, error)
	ListPublished(ctx context.Context, limit, offset int) ([]Page, error)
	CountPublished(ctx context.Context) (int, error)

	// SitemapItems returns a lightweight enumeration of every published,
	// non-trashed page (slug/title/description/updated_at only) for the M8
	// crawler routes. It loads no body/heavy fields.
	SitemapItems(ctx context.Context) ([]kernel.SitemapItem, error)

	TrashTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
	RestoreTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
	PermanentDeleteTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error

	// --- per-locale content overlay (M7b-2) ---------------------------------

	// UpsertTranslationTx inserts or updates the translation row for a NON-default
	// locale within tx (en content lives on the base page row). Body is sanitized
	// by the service before it reaches here.
	UpsertTranslationTx(ctx context.Context, tx pgx.Tx, pageID uuid.UUID, t Translation) error
	// GetTranslation returns one locale's translation row, or ErrNotFound.
	GetTranslation(ctx context.Context, pageID uuid.UUID, locale string) (Translation, error)
	// ListTranslations returns every translation row for a page (all locales).
	ListTranslations(ctx context.Context, pageID uuid.UUID) ([]Translation, error)
	// TranslatedLocales returns the set of locales that already have a translation
	// row for the page (drives the editor's "has translation" tab markers).
	TranslatedLocales(ctx context.Context, pageID uuid.UUID) ([]string, error)
	// DeleteTranslationTx removes a locale's translation row within tx.
	DeleteTranslationTx(ctx context.Context, tx pgx.Tx, pageID uuid.UUID, locale string) error
	// GetActiveInLocaleByID loads an active page by id with title/body overlaid by
	// locale's translation (empty/absent field -> base fallback).
	GetActiveInLocaleByID(ctx context.Context, id uuid.UUID, locale string) (Page, error)
	// GetPublishedInLocaleBySlug loads a published page by slug overlaid by locale.
	GetPublishedInLocaleBySlug(ctx context.Context, slug, locale string) (Page, error)
}

// Authorizer answers (action, subject) permission questions for a user.
type Authorizer interface {
	Can(ctx context.Context, userID uuid.UUID, action, subject string) bool
}

// Publisher publishes a domain event inside a transaction. *events.Bus satisfies
// it.
type Publisher interface {
	Publish(ctx context.Context, tx pgx.Tx, event events.Event) error
}

// Clock returns the current time; injected so publish timestamps are
// deterministic in tests.
type Clock func() time.Time

// RevisionRepository is re-exported for the service's dependency list.
type RevisionRepository = kernel.RevisionRepository
