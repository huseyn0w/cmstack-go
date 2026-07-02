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

	// --- per-locale content overlay (M7b-3) ---------------------------------

	// UpsertTranslationTx inserts or updates the translation row for a NON-default
	// locale within tx (en content lives on the base tag row).
	UpsertTranslationTx(ctx context.Context, tx pgx.Tx, tagID uuid.UUID, t Translation) error
	// GetTranslation returns one locale's translation row, or ErrNotFound.
	GetTranslation(ctx context.Context, tagID uuid.UUID, locale string) (Translation, error)
	// ListTranslations returns every translation row for a tag (all locales).
	ListTranslations(ctx context.Context, tagID uuid.UUID) ([]Translation, error)
	// TranslatedLocales returns the set of locales that already have a translation
	// row for the tag (drives the editor's "has translation" tab markers).
	TranslatedLocales(ctx context.Context, tagID uuid.UUID) ([]string, error)
	// DeleteTranslationTx removes a locale's translation row within tx.
	DeleteTranslationTx(ctx context.Context, tx pgx.Tx, tagID uuid.UUID, locale string) error
	// GetInLocaleByID loads a tag by id with name overlaid by locale's translation
	// (empty/absent name -> base fallback).
	GetInLocaleByID(ctx context.Context, id uuid.UUID, locale string) (Tag, error)
	// GetPublishedInLocaleBySlug loads a tag by slug overlaid by locale.
	GetPublishedInLocaleBySlug(ctx context.Context, slug, locale string) (Tag, error)
}

// Authorizer answers (action, subject) permission questions for a user.
type Authorizer interface {
	Can(ctx context.Context, userID uuid.UUID, action, subject string) bool
}
