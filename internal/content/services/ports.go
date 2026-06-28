package services

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/platform/events"
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
	Title       string
	Slug        string
	Summary     string
	Body        string
	Price       string
	AreaServed  string
	Status      kernel.Status
	PublishedAt *time.Time
	ReadingTime int
}

// UpdateServiceData is the fully-prepared row the repo writes on update.
type UpdateServiceData struct {
	Title       string
	Slug        string
	Summary     string
	Body        string
	Price       string
	AreaServed  string
	Status      kernel.Status
	PublishedAt *time.Time
	ReadingTime int
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

	TrashTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
	RestoreTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
	PermanentDeleteTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error

	// FAQs read path (ordered by position).
	ListFAQs(ctx context.Context, serviceID uuid.UUID) ([]FAQ, error)
	// ReplaceFAQsTx atomically deletes the service's existing FAQs and inserts
	// the supplied list in order (the simple, race-free reorder/upsert strategy).
	ReplaceFAQsTx(ctx context.Context, tx pgx.Tx, serviceID uuid.UUID, faqs []FAQData) error
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
