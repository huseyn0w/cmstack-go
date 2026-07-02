package categories

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
	// ErrForbidden is returned when the actor lacks the category permission.
	ErrForbidden = errors.New("categories: forbidden")
	// ErrNameRequired is returned when a create/update has no usable name.
	ErrNameRequired = errors.New("categories: name is required")
	// ErrParentCycle is returned when a parent assignment would create a cycle
	// (a category being its own ancestor) or a category being its own parent.
	ErrParentCycle = errors.New("categories: parent would create a cycle")
	// ErrParentNotFound is returned when the chosen parent id does not exist.
	ErrParentNotFound = errors.New("categories: parent not found")
	// ErrDefaultLocaleTranslation is returned when a translation write targets the
	// default locale, whose content lives on the base row (edited via Update).
	ErrDefaultLocaleTranslation = errors.New("categories: cannot store a translation for the default locale")
	// ErrUnsupportedLocale is returned when a translation write/read targets a
	// locale outside the supported set.
	ErrUnsupportedLocale = errors.New("categories: unsupported locale")
)

// Service holds ALL category logic. It accesses data only through the
// repository, owns no globals, and gates every mutating action on the coarse
// `category` permission grant (no per-author ownership — the taxonomy is
// site-wide, exactly like pages).
type Service struct {
	pool  db.Beginner
	repo  Repository
	authz Authorizer
}

// NewService constructs the category Service with explicit dependencies.
func NewService(pool db.Beginner, repo Repository, authz Authorizer) *Service {
	return &Service{pool: pool, repo: repo, authz: authz}
}

// CreateInput is the validated create request from the handler.
type CreateInput struct {
	Name        string
	Slug        string // optional; derived from name when empty
	Description string
	ParentID    *uuid.UUID
}

// Create makes a new category. Description is sanitized, slug derived+deduped,
// and the parent (when set) verified to exist. Requires create:category.
func (s *Service) Create(ctx context.Context, actorID uuid.UUID, in CreateInput) (Category, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionCreate, accounts.SubjectCategory) {
		return Category{}, ErrForbidden
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Category{}, ErrNameRequired
	}

	if err := s.validateParent(ctx, uuid.Nil, in.ParentID); err != nil {
		return Category{}, err
	}

	desired := kernel.Slugify(firstNonEmpty(in.Slug, name))
	slug, err := s.uniqueSlug(ctx, desired, uuid.Nil)
	if err != nil {
		return Category{}, err
	}

	data := CreateCategoryData{
		Name:        name,
		Slug:        slug,
		Description: kernel.SanitizeRichText(in.Description),
		ParentID:    in.ParentID,
	}

	var created Category
	err = db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		c, err := s.repo.CreateTx(ctx, tx, data)
		if err != nil {
			return fmt.Errorf("create category: %w", err)
		}
		created = c
		return nil
	})
	if err != nil {
		return Category{}, err
	}
	return created, nil
}

// UpdateInput is the validated update request. Pointer fields are "set" when
// non-nil. ParentID is special: SetParent distinguishes "clear the parent"
// (SetParent=true, ParentID=nil) from "leave unchanged" (SetParent=false).
type UpdateInput struct {
	Name        *string
	Slug        *string
	Description *string
	SetParent   bool
	ParentID    *uuid.UUID
}

// Update mutates an existing category: description re-sanitized, slug re-deduped,
// parent cycle-checked. Requires update:category.
func (s *Service) Update(ctx context.Context, actorID uuid.UUID, id uuid.UUID, in UpdateInput) (Category, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectCategory) {
		return Category{}, ErrForbidden
	}

	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return Category{}, err
	}

	next := existing
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" {
			return Category{}, ErrNameRequired
		}
		next.Name = n
	}
	if in.Description != nil {
		next.Description = kernel.SanitizeRichText(*in.Description)
	}
	if in.Slug != nil {
		desired := kernel.Slugify(*in.Slug)
		slug, err := s.uniqueSlug(ctx, desired, id)
		if err != nil {
			return Category{}, err
		}
		next.Slug = slug
	}
	if in.SetParent {
		if err := s.validateParent(ctx, id, in.ParentID); err != nil {
			return Category{}, err
		}
		next.ParentID = in.ParentID
	}

	var updated Category
	err = db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		c, err := s.repo.UpdateTx(ctx, tx, id, UpdateCategoryData{
			Name:        next.Name,
			Slug:        next.Slug,
			Description: next.Description,
			ParentID:    next.ParentID,
		})
		if err != nil {
			return fmt.Errorf("update category: %w", err)
		}
		updated = c
		return nil
	})
	if err != nil {
		return Category{}, err
	}
	return updated, nil
}

// Delete hard-deletes a category. post_categories rows cascade; children are
// detached (parent set to NULL). Requires delete:category.
func (s *Service) Delete(ctx context.Context, actorID, id uuid.UUID) error {
	if !s.authz.Can(ctx, actorID, accounts.ActionDelete, accounts.SubjectCategory) {
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

// AssignTx replaces a post's full category set within an EXISTING transaction
// (the post service's write tx). It detaches all current associations then
// attaches the deduped, existence-checked ids, so the post write and its
// taxonomy commit atomically. Unknown ids are skipped (a stale form selection
// must not abort the post save). This is the M3 taxonomy seam.
func (s *Service) AssignTx(ctx context.Context, tx pgx.Tx, postID uuid.UUID, categoryIDs []uuid.UUID) error {
	if err := s.repo.DetachAllTx(ctx, tx, postID); err != nil {
		return fmt.Errorf("detach categories: %w", err)
	}
	for _, id := range kernel.DedupeIDs(categoryIDs) {
		// Verify the category exists before attaching; skip stale selections.
		if _, err := s.repo.GetByID(ctx, id); err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return err
		}
		if err := s.repo.AttachTx(ctx, tx, postID, id); err != nil {
			return fmt.Errorf("attach category: %w", err)
		}
	}
	return nil
}

// --- reads -------------------------------------------------------------------

// Tree returns every category as a depth-annotated, pre-order traversal of the
// hierarchy, for the admin indented list and the parent picker. The walk is
// bounded and cycle-safe (a data anomaly can never loop forever).
func (s *Service) Tree(ctx context.Context) ([]TreeNode, error) {
	all, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	return BuildTree(all), nil
}

// AllFlat returns every category name-ordered (raw, no depth) — used where a
// flat option list is enough.
func (s *Service) AllFlat(ctx context.Context) ([]Category, error) {
	return s.repo.ListAll(ctx)
}

// Get returns a category by id for the editor. Requires read:category.
func (s *Service) Get(ctx context.Context, actorID, id uuid.UUID) (Category, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionRead, accounts.SubjectCategory) {
		return Category{}, ErrForbidden
	}
	return s.repo.GetByID(ctx, id)
}

// CategoriesForPost returns the categories attached to a post (no auth; used in
// both admin and public render paths).
func (s *Service) CategoriesForPost(ctx context.Context, postID uuid.UUID) ([]Category, error) {
	return s.repo.ListForPost(ctx, postID)
}

// IDsForPost returns the attached category ids for editor pre-selection.
func (s *Service) IDsForPost(ctx context.Context, postID uuid.UUID) ([]uuid.UUID, error) {
	return s.repo.IDsForPost(ctx, postID)
}

// PublicBySlug returns a category by slug for the public archive (no auth).
func (s *Service) PublicBySlug(ctx context.Context, slug string) (Category, error) {
	return s.repo.GetBySlug(ctx, slug)
}

// PublicBySlugLocale returns a category by slug with its content overlaid by the
// active locale, falling back to the base (en) row for any missing translation
// field. When locale is the default (en) or unsupported it resolves to the base
// row (identical to PublicBySlug), so nothing breaks (M7b-3).
func (s *Service) PublicBySlugLocale(ctx context.Context, slug string, locale i18n.Locale) (Category, error) {
	if locale.IsDefault() || !i18n.IsSupported(locale) {
		return s.repo.GetBySlug(ctx, slug)
	}
	return s.repo.GetPublishedInLocaleBySlug(ctx, slug, locale.String())
}

// PublishedPostIDs returns published post ids in a category, paginated, plus the
// total — the data behind the public category archive.
func (s *Service) PublishedPostIDs(ctx context.Context, categoryID uuid.UUID, limit, offset int) ([]uuid.UUID, int, error) {
	ids, err := s.repo.ListPublishedPostIDsInCategory(ctx, categoryID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.CountPublishedPostsInCategory(ctx, categoryID)
	if err != nil {
		return nil, 0, err
	}
	return ids, total, nil
}

// --- per-locale content overlay (M7b-3) --------------------------------------

// TranslationInput is the editor's per-locale content save for a NON-default
// locale. The description is sanitized (same kernel sanitizer as the base
// description); structural fields (slug, parent) are NOT part of it (they are
// shared on the base row and edited via Update).
type TranslationInput struct {
	Name        string
	Description string
}

// SaveTranslation upserts a NON-default locale's content overlay for a category.
// It requires update:category (like Update; the taxonomy has no per-author
// ownership). The default locale is rejected (its content lives on the base row —
// callers edit it via Update).
func (s *Service) SaveTranslation(ctx context.Context, actorID, id uuid.UUID, locale i18n.Locale, in TranslationInput) error {
	if !i18n.IsSupported(locale) {
		return ErrUnsupportedLocale
	}
	if locale.IsDefault() {
		return ErrDefaultLocaleTranslation
	}
	if !s.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectCategory) {
		return ErrForbidden
	}
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return ErrNameRequired
	}
	t := Translation{
		Locale:      locale.String(),
		Name:        name,
		Description: kernel.SanitizeRichText(in.Description),
	}
	return db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return s.repo.UpsertTranslationTx(ctx, tx, id, t)
	})
}

// GetInLocale loads a category for the editor with its content overlaid by locale
// (base fallback per field). The default locale resolves to the base row.
// Requires read:category. Used to populate the editor's per-locale tab.
func (s *Service) GetInLocale(ctx context.Context, actorID, id uuid.UUID, locale i18n.Locale) (Category, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionRead, accounts.SubjectCategory) {
		return Category{}, ErrForbidden
	}
	if locale.IsDefault() || !i18n.IsSupported(locale) {
		return s.repo.GetByID(ctx, id)
	}
	return s.repo.GetInLocaleByID(ctx, id, locale.String())
}

// TranslatedLocales returns the NON-default locales that already have a
// translation row for the category (drives the editor's per-tab "has
// translation" markers). Requires read:category.
func (s *Service) TranslatedLocales(ctx context.Context, actorID, id uuid.UUID) ([]i18n.Locale, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionRead, accounts.SubjectCategory) {
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

// validateParent enforces the hierarchy invariants for assigning parentID to the
// category selfID (uuid.Nil on create): the parent must exist; a category may not
// be its own parent; and the parent must not be a descendant of the category
// (which would create a cycle). Mirrors the pages ancestry-walk approach.
func (s *Service) validateParent(ctx context.Context, selfID uuid.UUID, parentID *uuid.UUID) error {
	if parentID == nil {
		return nil
	}
	if selfID != uuid.Nil && *parentID == selfID {
		return ErrParentCycle
	}
	parent, err := s.repo.GetByID(ctx, *parentID)
	if errors.Is(err, ErrNotFound) {
		return ErrParentNotFound
	}
	if err != nil {
		return err
	}
	if selfID != uuid.Nil {
		cur := parent.ParentID
		for i := 0; cur != nil && i < 1000; i++ {
			if *cur == selfID {
				return ErrParentCycle
			}
			anc, err := s.repo.GetByID(ctx, *cur)
			if err != nil {
				break
			}
			cur = anc.ParentID
		}
	}
	return nil
}

// uniqueSlug derives a slug unique across categories, excluding excludeID.
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

// BuildTree converts a flat category slice into a depth-annotated pre-order
// traversal (roots first, each followed by its subtree). Orphans (a parent that
// is not in the set) are treated as roots so nothing is lost. Cycle-safe: each
// node is emitted at most once.
func BuildTree(all []Category) []TreeNode {
	childrenOf := make(map[uuid.UUID][]Category)
	present := make(map[uuid.UUID]bool, len(all))
	for _, c := range all {
		present[c.ID] = true
	}
	var roots []Category
	for _, c := range all {
		if c.ParentID != nil && present[*c.ParentID] {
			childrenOf[*c.ParentID] = append(childrenOf[*c.ParentID], c)
		} else {
			roots = append(roots, c)
		}
	}

	var out []TreeNode
	visited := make(map[uuid.UUID]bool, len(all))
	var walk func(c Category, depth int)
	walk = func(c Category, depth int) {
		if visited[c.ID] || depth > 1000 {
			return
		}
		visited[c.ID] = true
		out = append(out, TreeNode{Category: c, Depth: depth})
		for _, child := range childrenOf[c.ID] {
			walk(child, depth+1)
		}
	}
	for _, root := range roots {
		walk(root, 0)
	}
	// Emit any category not reached (defensive: part of a pure cycle) as a root.
	for _, c := range all {
		if !visited[c.ID] {
			out = append(out, TreeNode{Category: c, Depth: 0})
			visited[c.ID] = true
		}
	}
	return out
}
