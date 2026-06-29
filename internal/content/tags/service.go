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
)

// Domain errors carried to the handler's error summary.
var (
	// ErrForbidden is returned when the actor lacks the tag permission.
	ErrForbidden = errors.New("tags: forbidden")
	// ErrNameRequired is returned when a create/update has no usable name.
	ErrNameRequired = errors.New("tags: name is required")
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
