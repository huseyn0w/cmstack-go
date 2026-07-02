package tags

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/platform/db"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
)

// Domain errors carried to the handler's error summary.
var (
	// ErrForbidden is returned when the actor lacks the tag permission.
	ErrForbidden = errors.New("tags: forbidden")
	// ErrNameRequired is returned when a create/update has no usable name.
	ErrNameRequired = errors.New("tags: name is required")
	// ErrDefaultLocaleTranslation is returned when a translation write targets the
	// default locale, whose content lives on the base row (edited via Update).
	ErrDefaultLocaleTranslation = errors.New("tags: cannot store a translation for the default locale")
	// ErrUnsupportedLocale is returned when a translation write/read targets a
	// locale outside the supported set.
	ErrUnsupportedLocale = errors.New("tags: unsupported locale")
)

// Service holds ALL tag logic. It accesses data only through the repository,
// owns no globals, and gates every mutating action on the coarse `tag`
// permission grant (no per-author ownership — tags are site-wide).
type Service struct {
	pool  db.Beginner
	repo  Repository
	authz Authorizer
}

// NewService constructs the tag Service with explicit dependencies.
func NewService(pool db.Beginner, repo Repository, authz Authorizer) *Service {
	return &Service{pool: pool, repo: repo, authz: authz}
}

// CreateInput is the validated create request from the handler.
type CreateInput struct {
	Name string
	Slug string // optional; derived from name when empty
}

// Create makes a new tag with a derived+deduped slug. Requires create:tag.
func (s *Service) Create(ctx context.Context, actorID uuid.UUID, in CreateInput) (Tag, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionCreate, accounts.SubjectTag) {
		return Tag{}, ErrForbidden
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Tag{}, ErrNameRequired
	}

	desired := kernel.Slugify(firstNonEmpty(in.Slug, name))
	slug, err := s.uniqueSlug(ctx, desired, uuid.Nil)
	if err != nil {
		return Tag{}, err
	}

	var created Tag
	err = db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		t, err := s.repo.CreateTx(ctx, tx, CreateTagData{Name: name, Slug: slug})
		if err != nil {
			return fmt.Errorf("create tag: %w", err)
		}
		created = t
		return nil
	})
	if err != nil {
		return Tag{}, err
	}
	return created, nil
}

// UpdateInput is the validated update request. Pointer fields are "set" when
// non-nil.
type UpdateInput struct {
	Name *string
	Slug *string
}

// Update mutates an existing tag (slug re-deduped). Requires update:tag.
func (s *Service) Update(ctx context.Context, actorID uuid.UUID, id uuid.UUID, in UpdateInput) (Tag, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectTag) {
		return Tag{}, ErrForbidden
	}

	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return Tag{}, err
	}

	next := existing
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" {
			return Tag{}, ErrNameRequired
		}
		next.Name = n
	}
	if in.Slug != nil {
		desired := kernel.Slugify(*in.Slug)
		slug, err := s.uniqueSlug(ctx, desired, id)
		if err != nil {
			return Tag{}, err
		}
		next.Slug = slug
	}

	var updated Tag
	err = db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		t, err := s.repo.UpdateTx(ctx, tx, id, UpdateTagData{Name: next.Name, Slug: next.Slug})
		if err != nil {
			return fmt.Errorf("update tag: %w", err)
		}
		updated = t
		return nil
	})
	if err != nil {
		return Tag{}, err
	}
	return updated, nil
}

// Delete hard-deletes a tag. post_tags rows cascade. Requires delete:tag.
func (s *Service) Delete(ctx context.Context, actorID, id uuid.UUID) error {
	if !s.authz.Can(ctx, actorID, accounts.ActionDelete, accounts.SubjectTag) {
		return ErrForbidden
	}
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return err
	}
	return db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return s.repo.DeleteTx(ctx, tx, id)
	})
}

// --- M2M assignment (driven by the post service) -----------------------------

// AssignTx replaces a post's full tag set within an EXISTING transaction (the
// post service's write tx). It detaches all current associations then attaches
// the deduped, existence-checked ids, so the post write and its taxonomy commit
// atomically. Unknown ids are skipped. This is the M3 taxonomy seam.
func (s *Service) AssignTx(ctx context.Context, tx pgx.Tx, postID uuid.UUID, tagIDs []uuid.UUID) error {
	if err := s.repo.DetachAllTx(ctx, tx, postID); err != nil {
		return fmt.Errorf("detach tags: %w", err)
	}
	for _, id := range kernel.DedupeIDs(tagIDs) {
		if _, err := s.repo.GetByID(ctx, id); err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return err
		}
		if err := s.repo.AttachTx(ctx, tx, postID, id); err != nil {
			return fmt.Errorf("attach tag: %w", err)
		}
	}
	return nil
}

// --- reads -------------------------------------------------------------------

// AdminList returns a paginated tag listing plus the total count.
func (s *Service) AdminList(ctx context.Context, limit, offset int) ([]Tag, int, error) {
	items, err := s.repo.List(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// AllFlat returns every tag name-ordered — used by the post editor selector.
func (s *Service) AllFlat(ctx context.Context) ([]Tag, error) {
	return s.repo.ListAll(ctx)
}

// Get returns a tag by id for the editor. Requires read:tag.
func (s *Service) Get(ctx context.Context, actorID, id uuid.UUID) (Tag, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionRead, accounts.SubjectTag) {
		return Tag{}, ErrForbidden
	}
	return s.repo.GetByID(ctx, id)
}

// TagsForPost returns the tags attached to a post (no auth).
func (s *Service) TagsForPost(ctx context.Context, postID uuid.UUID) ([]Tag, error) {
	return s.repo.ListForPost(ctx, postID)
}

// IDsForPost returns the attached tag ids for editor pre-selection.
func (s *Service) IDsForPost(ctx context.Context, postID uuid.UUID) ([]uuid.UUID, error) {
	return s.repo.IDsForPost(ctx, postID)
}

// PublicBySlug returns a tag by slug for the public archive (no auth).
func (s *Service) PublicBySlug(ctx context.Context, slug string) (Tag, error) {
	return s.repo.GetBySlug(ctx, slug)
}

// PublicBySlugLocale returns a tag by slug with its name overlaid by the active
// locale, falling back to the base (en) row for a missing translation. When
// locale is the default (en) or unsupported it resolves to the base row
// (identical to PublicBySlug), so nothing breaks (M7b-3).
func (s *Service) PublicBySlugLocale(ctx context.Context, slug string, locale i18n.Locale) (Tag, error) {
	if locale.IsDefault() || !i18n.IsSupported(locale) {
		return s.repo.GetBySlug(ctx, slug)
	}
	return s.repo.GetPublishedInLocaleBySlug(ctx, slug, locale.String())
}

// PublishedPostIDs returns published post ids in a tag, paginated, plus total.
func (s *Service) PublishedPostIDs(ctx context.Context, tagID uuid.UUID, limit, offset int) ([]uuid.UUID, int, error) {
	ids, err := s.repo.ListPublishedPostIDsInTag(ctx, tagID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.CountPublishedPostsInTag(ctx, tagID)
	if err != nil {
		return nil, 0, err
	}
	return ids, total, nil
}

// --- per-locale content overlay (M7b-3) --------------------------------------

// TranslationInput is the editor's per-locale content save for a NON-default
// locale. Only the name is translatable; the slug is shared on the base row.
type TranslationInput struct {
	Name string
}

// SaveTranslation upserts a NON-default locale's name overlay for a tag. It
// requires update:tag (like Update; tags have no per-author ownership). The
// default locale is rejected (its name lives on the base row — callers edit it
// via Update).
func (s *Service) SaveTranslation(ctx context.Context, actorID, id uuid.UUID, locale i18n.Locale, in TranslationInput) error {
	if !i18n.IsSupported(locale) {
		return ErrUnsupportedLocale
	}
	if locale.IsDefault() {
		return ErrDefaultLocaleTranslation
	}
	if !s.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectTag) {
		return ErrForbidden
	}
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return ErrNameRequired
	}
	t := Translation{Locale: locale.String(), Name: name}
	return db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return s.repo.UpsertTranslationTx(ctx, tx, id, t)
	})
}

// GetInLocale loads a tag for the editor with its name overlaid by locale (base
// fallback). The default locale resolves to the base row. Requires read:tag.
// Used to populate the editor's per-locale tab.
func (s *Service) GetInLocale(ctx context.Context, actorID, id uuid.UUID, locale i18n.Locale) (Tag, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionRead, accounts.SubjectTag) {
		return Tag{}, ErrForbidden
	}
	if locale.IsDefault() || !i18n.IsSupported(locale) {
		return s.repo.GetByID(ctx, id)
	}
	return s.repo.GetInLocaleByID(ctx, id, locale.String())
}

// TranslatedLocales returns the NON-default locales that already have a
// translation row for the tag (drives the editor's per-tab "has translation"
// markers). Requires read:tag.
func (s *Service) TranslatedLocales(ctx context.Context, actorID, id uuid.UUID) ([]i18n.Locale, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionRead, accounts.SubjectTag) {
		return nil, ErrForbidden
	}
	raw, err := s.repo.TranslatedLocales(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]i18n.Locale, 0, len(raw))
	for _, r := range raw {
		if l, ok := i18n.Parse(r); ok && !l.IsDefault() {
			out = append(out, l)
		}
	}
	return out, nil
}

// --- helpers -----------------------------------------------------------------

func (s *Service) uniqueSlug(ctx context.Context, desired string, excludeID uuid.UUID) (string, error) {
	return kernel.UniqueSlug(ctx, desired, func(ctx context.Context, slug string) (bool, error) {
		return s.repo.SlugTaken(ctx, slug, excludeID)
	})
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
