package pages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/platform/db"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
)

// Domain errors carried to the handler's error summary.
var (
	// ErrForbidden is returned when the actor lacks the page permission.
	ErrForbidden = errors.New("pages: forbidden")
	// ErrTitleRequired is returned when a create/update has no usable title.
	ErrTitleRequired = errors.New("pages: title is required")
	// ErrRevisionMismatch is returned when a revision does not belong to the page.
	ErrRevisionMismatch = errors.New("pages: revision does not belong to this page")
	// ErrParentCycle is returned when a parent assignment would create a cycle
	// (a page being its own ancestor) or a page being its own parent.
	ErrParentCycle = errors.New("pages: parent would create a cycle")
	// ErrParentNotFound is returned when the chosen parent id does not exist.
	ErrParentNotFound = errors.New("pages: parent not found")
	// ErrDefaultLocaleTranslation is returned when a translation write targets the
	// default locale (en). The default locale's content lives on the base page row
	// and is edited via Create/Update, not the translation overlay (M7b-2).
	ErrDefaultLocaleTranslation = errors.New("pages: cannot store a translation for the default locale")
	// ErrUnsupportedLocale is returned when a translation write/read targets a
	// locale outside the supported set.
	ErrUnsupportedLocale = errors.New("pages: unsupported locale")
)

// Service holds ALL page logic. It accesses data only through the repositories,
// fires side effects only via events, and owns no globals. There is NO per-author
// ownership for pages (canon): the permission grant alone gates every action.
type Service struct {
	pool      db.Beginner
	repo      Repository
	revisions kernel.RevisionRepository
	authz     Authorizer
	bus       Publisher
	now       Clock
}

// NewService constructs the page Service with explicit dependencies.
func NewService(
	pool db.Beginner,
	repo Repository,
	revisions kernel.RevisionRepository,
	authz Authorizer,
	bus Publisher,
	now Clock,
) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{pool: pool, repo: repo, revisions: revisions, authz: authz, bus: bus, now: now}
}

// CreateInput is the validated create request from the handler.
type CreateInput struct {
	Title    string
	Slug     string // optional; derived from title when empty
	Body     string
	Status   kernel.Status
	ParentID *uuid.UUID
	Template string
	// SEO metadata (M8-1). MetaTitle/MetaDescription are the default-locale (en)
	// values stored on the base row; CanonicalURL/NoIndex are structural.
	MetaTitle       string
	MetaDescription string
	CanonicalURL    string
	NoIndex         bool
}

// Create makes a new page. Body is sanitized, reading time computed, slug
// derived+deduped, the template validated against the allow-list, and the parent
// (when set) verified to exist. publishedAt is stamped when created published.
// Requires create:page.
func (s *Service) Create(ctx context.Context, actorID uuid.UUID, in CreateInput) (Page, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionCreate, accounts.SubjectPage) {
		return Page{}, ErrForbidden
	}

	title := strings.TrimSpace(in.Title)
	if title == "" {
		return Page{}, ErrTitleRequired
	}

	if err := s.validateParent(ctx, uuid.Nil, in.ParentID); err != nil {
		return Page{}, err
	}

	body := kernel.SanitizeRichText(in.Body)
	status := in.Status
	if !status.Valid() {
		status = kernel.StatusDraft
	}

	desired := kernel.Slugify(firstNonEmpty(in.Slug, title))
	slug, err := s.uniqueSlug(ctx, desired, uuid.Nil)
	if err != nil {
		return Page{}, err
	}

	var publishedAt *time.Time
	if status == kernel.StatusPublished {
		now := s.now()
		publishedAt = &now
	}

	data := CreatePageData{
		Title:           title,
		Slug:            slug,
		Body:            body,
		Status:          status,
		PublishedAt:     publishedAt,
		ParentID:        in.ParentID,
		Template:        normalizeTemplate(in.Template),
		ReadingTime:     kernel.ReadingTimeMinutes(body),
		MetaTitle:       strings.TrimSpace(in.MetaTitle),
		MetaDescription: strings.TrimSpace(in.MetaDescription),
		CanonicalURL:    strings.TrimSpace(in.CanonicalURL),
		NoIndex:         in.NoIndex,
	}

	var created Page
	err = db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		p, err := s.repo.CreateTx(ctx, tx, data)
		if err != nil {
			return fmt.Errorf("create page: %w", err)
		}
		created = p
		if p.Published() {
			return s.emitPublished(ctx, tx, p)
		}
		return nil
	})
	if err != nil {
		return Page{}, err
	}
	return created, nil
}

// UpdateInput is the validated update request. Pointer fields are "set" when
// non-nil; a nil field leaves the existing value unchanged. ParentID is special:
// SetParent distinguishes "clear the parent" (SetParent=true, ParentID=nil) from
// "leave unchanged" (SetParent=false).
type UpdateInput struct {
	Title     *string
	Slug      *string
	Body      *string
	Status    *kernel.Status
	Template  *string
	SetParent bool
	ParentID  *uuid.UUID
	// SEO metadata (M8-1). Pointer-optional: nil leaves the stored value unchanged.
	MetaTitle       *string
	MetaDescription *string
	CanonicalURL    *string
	NoIndex         *bool
}

// Update mutates an existing page. It snapshots the prior state into a revision
// (SYNC, in-tx) first, then applies the changes: body re-sanitized, reading time
// recomputed, slug re-deduped, template re-validated, parent cycle-checked.
// publishedAt is stamped on first publish and PRESERVED thereafter. Requires
// update:page.
func (s *Service) Update(ctx context.Context, actorID uuid.UUID, id uuid.UUID, in UpdateInput) (Page, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectPage) {
		return Page{}, ErrForbidden
	}

	existing, err := s.repo.GetActiveByID(ctx, id)
	if err != nil {
		return Page{}, err
	}

	next := existing
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		if t == "" {
			return Page{}, ErrTitleRequired
		}
		next.Title = t
	}
	if in.Body != nil {
		next.Body = kernel.SanitizeRichText(*in.Body)
		next.ReadingTime = kernel.ReadingTimeMinutes(next.Body)
	}
	if in.Slug != nil {
		desired := kernel.Slugify(*in.Slug)
		slug, err := s.uniqueSlug(ctx, desired, id)
		if err != nil {
			return Page{}, err
		}
		next.Slug = slug
	}
	if in.Template != nil {
		next.Template = normalizeTemplate(*in.Template)
	}
	if in.SetParent {
		if err := s.validateParent(ctx, id, in.ParentID); err != nil {
			return Page{}, err
		}
		next.ParentID = in.ParentID
	}
	if in.MetaTitle != nil {
		next.MetaTitle = strings.TrimSpace(*in.MetaTitle)
	}
	if in.MetaDescription != nil {
		next.MetaDescription = strings.TrimSpace(*in.MetaDescription)
	}
	if in.CanonicalURL != nil {
		next.CanonicalURL = strings.TrimSpace(*in.CanonicalURL)
	}
	if in.NoIndex != nil {
		next.NoIndex = *in.NoIndex
	}

	becamePublished := false
	if in.Status != nil && in.Status.Valid() {
		next.Status = *in.Status
		if *in.Status == kernel.StatusPublished {
			if existing.PublishedAt == nil {
				now := s.now()
				next.PublishedAt = &now
			}
			becamePublished = existing.Status != kernel.StatusPublished
		}
	}

	return s.persistUpdate(ctx, actorID, existing, next, becamePublished)
}

// persistUpdate snapshots the prior state and writes the next state in one
// transaction, emitting the sync revision event and (when newly published) the
// async publish event.
func (s *Service) persistUpdate(ctx context.Context, actorID uuid.UUID, prior, next Page, becamePublished bool) (Page, error) {
	snap, err := kernel.MarshalSnapshot(snapshot{
		Title:    prior.Title,
		Slug:     prior.Slug,
		Body:     prior.Body,
		Status:   prior.Status.String(),
		Template: prior.Template,
		ParentID: uuidPtrString(prior.ParentID),
	})
	if err != nil {
		return Page{}, fmt.Errorf("marshal snapshot: %w", err)
	}

	var updated Page
	err = db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		author := actorID
		rev, err := s.revisions.CreateTx(ctx, tx, kernel.CreateRevisionInput{
			EntityType: kernel.EntityTypePage,
			EntityID:   prior.ID,
			Snapshot:   snap,
			AuthorID:   &author,
		})
		if err != nil {
			return fmt.Errorf("snapshot revision: %w", err)
		}
		if err := s.bus.Publish(ctx, tx, RevisionCreatedEvent{
			RevisionID: rev.ID,
			EntityType: rev.EntityType,
			EntityID:   rev.EntityID,
			AuthorID:   rev.AuthorID,
			CreatedAt:  rev.CreatedAt,
		}); err != nil {
			return err
		}

		p, err := s.repo.UpdateTx(ctx, tx, prior.ID, UpdatePageData{
			Title:           next.Title,
			Slug:            next.Slug,
			Body:            next.Body,
			Status:          next.Status,
			PublishedAt:     next.PublishedAt,
			ParentID:        next.ParentID,
			Template:        next.Template,
			ReadingTime:     next.ReadingTime,
			MetaTitle:       next.MetaTitle,
			MetaDescription: next.MetaDescription,
			CanonicalURL:    next.CanonicalURL,
			NoIndex:         next.NoIndex,
		})
		if err != nil {
			return fmt.Errorf("update page: %w", err)
		}
		updated = p

		if becamePublished && p.Published() {
			return s.emitPublished(ctx, tx, p)
		}
		return nil
	})
	if err != nil {
		return Page{}, err
	}
	return updated, nil
}

// Publish transitions a page to PUBLISHED, stamping publishedAt once. Requires
// update:page.
func (s *Service) Publish(ctx context.Context, actorID, id uuid.UUID) (Page, error) {
	published := kernel.StatusPublished
	return s.Update(ctx, actorID, id, UpdateInput{Status: &published})
}

// Unpublish returns a page to DRAFT. publishedAt is PRESERVED.
func (s *Service) Unpublish(ctx context.Context, actorID, id uuid.UUID) (Page, error) {
	draft := kernel.StatusDraft
	return s.Update(ctx, actorID, id, UpdateInput{Status: &draft})
}

// Trash soft-deletes a page. Requires delete:page.
func (s *Service) Trash(ctx context.Context, actorID, id uuid.UUID) error {
	if !s.authz.Can(ctx, actorID, accounts.ActionDelete, accounts.SubjectPage) {
		return ErrForbidden
	}
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return err
	}
	return db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return s.repo.TrashTx(ctx, tx, id)
	})
}

// Restore un-trashes a page. Requires update:page.
func (s *Service) Restore(ctx context.Context, actorID, id uuid.UUID) error {
	if !s.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectPage) {
		return ErrForbidden
	}
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return err
	}
	return db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return s.repo.RestoreTx(ctx, tx, id)
	})
}

// PermanentDelete hard-deletes a trashed page. Requires delete:page.
func (s *Service) PermanentDelete(ctx context.Context, actorID, id uuid.UUID) error {
	if !s.authz.Can(ctx, actorID, accounts.ActionDelete, accounts.SubjectPage) {
		return ErrForbidden
	}
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return err
	}
	return db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return s.repo.PermanentDeleteTx(ctx, tx, id)
	})
}

// Revisions lists a page's revision snapshots (newest first). Requires
// update:page.
func (s *Service) Revisions(ctx context.Context, actorID, id uuid.UUID) ([]kernel.Revision, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectPage) {
		return nil, ErrForbidden
	}
	return s.revisions.List(ctx, kernel.EntityTypePage, id)
}

// RestoreRevision applies a prior revision's scalar fields as a NEW update (which
// itself snapshots the current state first, so the restore is reversible).
// Requires update:page.
func (s *Service) RestoreRevision(ctx context.Context, actorID, id, revisionID uuid.UUID) (Page, error) {
	rev, err := s.revisions.Get(ctx, revisionID)
	if err != nil {
		return Page{}, err
	}
	if rev.EntityType != kernel.EntityTypePage || rev.EntityID != id {
		return Page{}, ErrRevisionMismatch
	}
	var snap snapshot
	if err := json.Unmarshal(rev.Snapshot, &snap); err != nil {
		return Page{}, fmt.Errorf("decode revision snapshot: %w", err)
	}
	status := kernel.ParseStatus(snap.Status)
	parent := parseUUIDPtr(snap.ParentID)
	in := UpdateInput{
		Title:     &snap.Title,
		Slug:      &snap.Slug,
		Body:      &snap.Body,
		Status:    &status,
		Template:  &snap.Template,
		SetParent: true,
		ParentID:  parent,
	}
	return s.Update(ctx, actorID, id, in)
}

// --- public reads (no auth; published only) ---------------------------------

// PublicBySlug returns a published, non-trashed page for the public detail page
// in the DEFAULT locale (en). Unchanged from M2 — the locale-aware path is
// PublicBySlugLocale.
func (s *Service) PublicBySlug(ctx context.Context, slug string) (Page, error) {
	return s.repo.GetPublishedBySlug(ctx, slug)
}

// PublicBySlugLocale returns a published page by slug with its content overlaid
// by the active locale, falling back to the base (en) row for any missing
// translation field. When locale is the default (en) or unsupported it resolves
// to the base row (identical to PublicBySlug), so nothing breaks (M7b-2).
func (s *Service) PublicBySlugLocale(ctx context.Context, slug string, locale i18n.Locale) (Page, error) {
	if locale.IsDefault() || !i18n.IsSupported(locale) {
		return s.repo.GetPublishedBySlug(ctx, slug)
	}
	return s.repo.GetPublishedInLocaleBySlug(ctx, slug, locale.String())
}

// Ancestors returns a page's ancestor chain from the root down to (but excluding)
// the page itself, for breadcrumbs. It walks parents defensively (bounded) so a
// data anomaly can never loop forever. Only the chain is returned; trashed/missing
// parents terminate the walk.
func (s *Service) Ancestors(ctx context.Context, p Page) ([]Page, error) {
	var chain []Page
	seen := map[uuid.UUID]bool{p.ID: true}
	cur := p.ParentID
	for i := 0; cur != nil && i < 32; i++ {
		if seen[*cur] {
			break
		}
		seen[*cur] = true
		parent, err := s.repo.GetByID(ctx, *cur)
		if err != nil {
			break
		}
		if parent.Trashed() {
			break
		}
		chain = append(chain, parent)
		cur = parent.ParentID
	}
	// Reverse so the slice is root-first.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

// --- admin reads ------------------------------------------------------------

// AdminList returns a filtered, paginated admin listing plus the total count.
func (s *Service) AdminList(ctx context.Context, f ListFilter) ([]Page, int, error) {
	items, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.Count(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// AllActive returns every non-trashed page — the data behind the hierarchy tree
// and the parent picker.
func (s *Service) AllActive(ctx context.Context) ([]Page, error) {
	return s.repo.ListAllActive(ctx)
}

// AdminTrashed returns the trashed listing plus total.
func (s *Service) AdminTrashed(ctx context.Context, limit, offset int) ([]Page, int, error) {
	items, err := s.repo.ListTrashed(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.CountTrashed(ctx)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Get returns a page by id for the editor. Requires read:page.
func (s *Service) Get(ctx context.Context, actorID, id uuid.UUID) (Page, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionRead, accounts.SubjectPage) {
		return Page{}, ErrForbidden
	}
	return s.repo.GetByID(ctx, id)
}

// --- per-locale content overlay (M7b-2) --------------------------------------

// TranslationInput is the editor's per-locale content save for a NON-default
// locale. The body is sanitized (same kernel sanitizer as the base body);
// structural fields are NOT part of it (they are shared on the base row and
// edited via Update).
type TranslationInput struct {
	Title           string
	Body            string
	MetaTitle       string
	MetaDescription string
}

// SaveTranslation upserts a NON-default locale's content overlay for a page.
// It requires update:page (like Update; pages have no per-author ownership). The
// default locale is rejected (its content lives on the base row — callers edit it
// via Update). No revision snapshot is taken and no event is emitted: the
// translation overlay is content, not a publish-state change.
func (s *Service) SaveTranslation(ctx context.Context, actorID, id uuid.UUID, locale i18n.Locale, in TranslationInput) error {
	if !i18n.IsSupported(locale) {
		return ErrUnsupportedLocale
	}
	if locale.IsDefault() {
		return ErrDefaultLocaleTranslation
	}
	if !s.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectPage) {
		return ErrForbidden
	}
	if _, err := s.repo.GetActiveByID(ctx, id); err != nil {
		return err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return ErrTitleRequired
	}
	t := Translation{
		Locale:          locale.String(),
		Title:           title,
		Body:            kernel.SanitizeRichText(in.Body),
		MetaTitle:       strings.TrimSpace(in.MetaTitle),
		MetaDescription: strings.TrimSpace(in.MetaDescription),
	}
	return db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return s.repo.UpsertTranslationTx(ctx, tx, id, t)
	})
}

// GetInLocale loads a page for the editor with its content overlaid by locale
// (base fallback per field). The default locale resolves to the base row.
// Requires read:page. Used to populate the editor's per-locale tab.
func (s *Service) GetInLocale(ctx context.Context, actorID, id uuid.UUID, locale i18n.Locale) (Page, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionRead, accounts.SubjectPage) {
		return Page{}, ErrForbidden
	}
	if locale.IsDefault() || !i18n.IsSupported(locale) {
		return s.repo.GetByID(ctx, id)
	}
	return s.repo.GetActiveInLocaleByID(ctx, id, locale.String())
}

// TranslatedLocales returns the NON-default locales that already have a
// translation row for the page (drives the editor's per-tab "has translation"
// markers). Requires read:page.
func (s *Service) TranslatedLocales(ctx context.Context, actorID, id uuid.UUID) ([]i18n.Locale, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionRead, accounts.SubjectPage) {
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
// page selfID (uuid.Nil on create): the parent must exist and not be trashed; a
// page may not be its own parent; and the parent must not be a descendant of the
// page (which would create a cycle). On create selfID is Nil, so only existence
// is checked.
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
	if parent.Trashed() {
		return ErrParentNotFound
	}
	// Walk the chosen parent's ancestry: if selfID appears, the parent is a
	// descendant of self and the assignment would create a cycle.
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

// uniqueSlug derives a slug unique across pages, excluding excludeID.
func (s *Service) uniqueSlug(ctx context.Context, desired string, excludeID uuid.UUID) (string, error) {
	return kernel.UniqueSlug(ctx, desired, func(ctx context.Context, slug string) (bool, error) {
		return s.repo.SlugTaken(ctx, slug, excludeID)
	})
}

// emitPublished publishes the async content.published event inside tx.
func (s *Service) emitPublished(ctx context.Context, tx pgx.Tx, p Page) error {
	publishedAt := p.UpdatedAt
	if p.PublishedAt != nil {
		publishedAt = *p.PublishedAt
	}
	return s.bus.Publish(ctx, tx, ContentPublishedEvent{
		EntityType:  kernel.EntityTypePage,
		PageID:      p.ID,
		Slug:        p.Slug,
		Title:       p.Title,
		PublishedAt: publishedAt,
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

func uuidPtrString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func parseUUIDPtr(s string) *uuid.UUID {
	if s == "" {
		return nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	return &id
}
