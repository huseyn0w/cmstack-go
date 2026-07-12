package services

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/events"
)

// ErrNotFound is the sentinel the repository returns when a service is absent.
var ErrNotFound = errors.New("services: not found")

// ListFilter narrows an admin service listing. A nil status means "no
// constraint".
type ListFilter struct {
	Status *kernel.Status
	Limit  int
	Offset int
}

// CreateServiceData is the fully-prepared row the repo inserts.
type CreateServiceData struct {
	Title           string
	Slug            string
	Summary         string
	Body            string
	Price           string
	AreaServed      string
	Status          kernel.Status
	PublishedAt     *time.Time
	ReadingTime     int
	MetaTitle       string
	MetaDescription string
	CanonicalURL    string
	NoIndex         bool
}

// UpdateServiceData is the fully-prepared row the repo writes on update.
type UpdateServiceData struct {
	Title           string
	Slug            string
	Summary         string
	Body            string
	Price           string
	AreaServed      string
	Status          kernel.Status
	PublishedAt     *time.Time
	ReadingTime     int
	MetaTitle       string
	MetaDescription string
	CanonicalURL    string
	NoIndex         bool
}

// FAQData is one FAQ row the repo writes when replacing a service's FAQ list.
// The service has already sanitized the answer and assigned positions.
type FAQData struct {
	Question string
	Answer   string
	Position int
}

// Repository is the data-access contract for services. It is the ONLY layer
// permitted to touch sqlc/pgx for services and their FAQs.
type Repository interface {
	CreateTx(ctx context.Context, tx pgx.Tx, in CreateServiceData) (Service, error)
	UpdateTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, in UpdateServiceData) (Service, error)

	GetByID(ctx context.Context, id uuid.UUID) (Service, error)
	GetActiveByID(ctx context.Context, id uuid.UUID) (Service, error)
	GetPublishedBySlug(ctx context.Context, slug string) (Service, error)

	SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error)

	List(ctx context.Context, f ListFilter) ([]Service, error)
	Count(ctx context.Context, f ListFilter) (int, error)
	ListTrashed(ctx context.Context, limit, offset int) ([]Service, error)
	CountTrashed(ctx context.Context) (int, error)
	ListPublished(ctx context.Context, limit, offset int) ([]Service, error)
	CountPublished(ctx context.Context) (int, error)

	// SitemapItems returns a lightweight enumeration of every published,
	// non-trashed service (slug/title/description/updated_at only) for the M8
	// crawler routes. It loads no body/heavy fields.
	SitemapItems(ctx context.Context) ([]kernel.SitemapItem, error)

	TrashTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
	RestoreTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
	PermanentDeleteTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error

	// FAQs read path (ordered by position).
	ListFAQs(ctx context.Context, serviceID uuid.UUID) ([]FAQ, error)
	// ReplaceFAQsTx atomically deletes the service's existing FAQs and inserts
	// the supplied list in order (the simple, race-free reorder/upsert strategy).
	ReplaceFAQsTx(ctx context.Context, tx pgx.Tx, serviceID uuid.UUID, faqs []FAQData) error

	// --- per-locale content overlay (M7b-2) ---------------------------------

	// UpsertTranslationTx inserts or updates the translation row for a NON-default
	// locale within tx (en content lives on the base services row). Body/summary
	// are sanitized by the service before they reach here.
	UpsertTranslationTx(ctx context.Context, tx pgx.Tx, serviceID uuid.UUID, t Translation) error
	// GetTranslation returns one locale's translation row, or ErrNotFound.
	GetTranslation(ctx context.Context, serviceID uuid.UUID, locale string) (Translation, error)
	// ListTranslations returns every translation row for a service (all locales).
	ListTranslations(ctx context.Context, serviceID uuid.UUID) ([]Translation, error)
	// TranslatedLocales returns the set of locales that already have a translation
	// row for the service (drives the editor's "has translation" tab markers).
	TranslatedLocales(ctx context.Context, serviceID uuid.UUID) ([]string, error)
	// DeleteTranslationTx removes a locale's translation row within tx.
	DeleteTranslationTx(ctx context.Context, tx pgx.Tx, serviceID uuid.UUID, locale string) error
	// GetActiveInLocaleByID loads an active service by id with title/summary/body
	// overlaid by locale's translation (empty/absent field -> base fallback).
	GetActiveInLocaleByID(ctx context.Context, id uuid.UUID, locale string) (Service, error)
	// GetPublishedInLocaleBySlug loads a published service by slug overlaid by locale.
	GetPublishedInLocaleBySlug(ctx context.Context, slug, locale string) (Service, error)
}

// Authorizer answers (action, subject) permission questions for a user.
type Authorizer interface {
	Can(ctx context.Context, userID uuid.UUID, action, subject string) bool
}

// Publisher publishes a domain event inside a transaction.
type Publisher interface {
	Publish(ctx context.Context, tx pgx.Tx, event events.Event) error
}

// Clock returns the current time; injected for deterministic publish timestamps.
type Clock func() time.Time

// RevisionRepository is re-exported for the service's dependency list.
type RevisionRepository = kernel.RevisionRepository
