package services

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
	// ErrForbidden is returned when the actor lacks the service permission.
	ErrForbidden = errors.New("services: forbidden")
	// ErrTitleRequired is returned when a create/update has no usable title.
	ErrTitleRequired = errors.New("services: title is required")
	// ErrRevisionMismatch is returned when a revision does not belong to the service.
	ErrRevisionMismatch = errors.New("services: revision does not belong to this service")
	// ErrDefaultLocaleTranslation is returned when a translation write targets the
	// default locale (en). The default locale's content lives on the base service
	// row and is edited via Create/Update, not the translation overlay (M7b-2).
	ErrDefaultLocaleTranslation = errors.New("services: cannot store a translation for the default locale")
	// ErrUnsupportedLocale is returned when a translation write/read targets a
	// locale outside the supported set.
	ErrUnsupportedLocale = errors.New("services: unsupported locale")
)

// Manager holds ALL service-page logic. It is named Manager (not Service) so it
// does not collide with the Service domain entity. It accesses data only through
// the repositories, fires side effects only via events, and owns no globals.
// There is NO per-author ownership for services (canon): the permission grant
// (Editor/Administrator) alone gates every action.
type Manager struct {
	pool      db.Beginner
	repo      Repository
	revisions kernel.RevisionRepository
	authz     Authorizer
	bus       Publisher
	now       Clock
}

// NewManager constructs the service Manager with explicit dependencies.
func NewManager(
	pool db.Beginner,
	repo Repository,
	revisions kernel.RevisionRepository,
	authz Authorizer,
	bus Publisher,
	now Clock,
) *Manager {
	if now == nil {
		now = time.Now
	}
	return &Manager{pool: pool, repo: repo, revisions: revisions, authz: authz, bus: bus, now: now}
}

// FAQInput is one submitted FAQ row before persistence (server sanitizes answers).
type FAQInput struct {
	Question string
	Answer   string
}

// CreateInput is the validated create request from the handler. FAQs are saved
// in the submitted order; blank rows (no question) are dropped.
type CreateInput struct {
	Title      string
	Slug       string // optional; derived from title when empty
	Summary    string
	Body       string
	Price      string
	AreaServed string
	Status     kernel.Status
	FAQs       []FAQInput
	// SEO metadata (M8-1). MetaTitle/MetaDescription are the default-locale (en)
	// values stored on the base row; CanonicalURL/NoIndex are structural.
	MetaTitle       string
	MetaDescription string
	CanonicalURL    string
	NoIndex         bool
}

// Create makes a new service. Summary, body, and every FAQ answer are sanitized
// write-time; reading time computed; slug derived+deduped; publishedAt stamped
// when created published. Requires create:service.
func (m *Manager) Create(ctx context.Context, actorID uuid.UUID, in CreateInput) (Service, error) {
	if !m.authz.Can(ctx, actorID, accounts.ActionCreate, accounts.SubjectService) {
		return Service{}, ErrForbidden
	}

	title := strings.TrimSpace(in.Title)
	if title == "" {
		return Service{}, ErrTitleRequired
	}

	body := kernel.SanitizeRichText(in.Body)
	status := in.Status
	if !status.Valid() {
		status = kernel.StatusDraft
	}

	desired := kernel.Slugify(firstNonEmpty(in.Slug, title))
	slug, err := m.uniqueSlug(ctx, desired, uuid.Nil)
	if err != nil {
		return Service{}, err
	}

	var publishedAt *time.Time
	if status == kernel.StatusPublished {
		now := m.now()
		publishedAt = &now
	}

	data := CreateServiceData{
		Title:           title,
		Slug:            slug,
		Summary:         sanitizePlain(in.Summary),
		Body:            body,
		Price:           strings.TrimSpace(in.Price),
		AreaServed:      strings.TrimSpace(in.AreaServed),
		Status:          status,
		PublishedAt:     publishedAt,
		ReadingTime:     kernel.ReadingTimeMinutes(body),
		MetaTitle:       strings.TrimSpace(in.MetaTitle),
		MetaDescription: strings.TrimSpace(in.MetaDescription),
		CanonicalURL:    strings.TrimSpace(in.CanonicalURL),
		NoIndex:         in.NoIndex,
	}
	faqs := prepareFAQs(in.FAQs)

	var created Service
	err = db.RunInTx(ctx, m.pool, func(ctx context.Context, tx pgx.Tx) error {
		svc, err := m.repo.CreateTx(ctx, tx, data)
		if err != nil {
			return fmt.Errorf("create service: %w", err)
		}
		if err := m.repo.ReplaceFAQsTx(ctx, tx, svc.ID, faqs); err != nil {
			return fmt.Errorf("save faqs: %w", err)
		}
		created = svc
		if svc.Published() {
			return m.emitPublished(ctx, tx, svc)
		}
		return nil
	})
	if err != nil {
		return Service{}, err
	}
	created.FAQs, _ = m.repo.ListFAQs(ctx, created.ID)
	return created, nil
}

// UpdateInput is the validated update request. Pointer fields are "set" when
// non-nil; a nil field leaves the existing value unchanged. FAQs is special:
// SetFAQs distinguishes "replace the FAQ list" (SetFAQs=true) from "leave
// unchanged" (SetFAQs=false).
type UpdateInput struct {
	Title      *string
	Slug       *string
	Summary    *string
	Body       *string
	Price      *string
	AreaServed *string
	Status     *kernel.Status
	SetFAQs    bool
	FAQs       []FAQInput
	// SEO metadata (M8-1). Pointer-optional: nil leaves the stored value unchanged.
	MetaTitle       *string
	MetaDescription *string
	CanonicalURL    *string
	NoIndex         *bool
}

// Update mutates an existing service. It snapshots the prior state into a revision
// (SYNC, in-tx) first, then applies the changes: summary/body/FAQ answers
// re-sanitized, reading time recomputed, slug re-deduped. publishedAt is stamped
// on first publish and PRESERVED thereafter. Requires update:service.
func (m *Manager) Update(ctx context.Context, actorID uuid.UUID, id uuid.UUID, in UpdateInput) (Service, error) {
	if !m.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectService) {
		return Service{}, ErrForbidden
	}

	existing, err := m.repo.GetActiveByID(ctx, id)
	if err != nil {
		return Service{}, err
	}

	next := existing
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		if t == "" {
			return Service{}, ErrTitleRequired
		}
		next.Title = t
	}
	if in.Summary != nil {
		next.Summary = sanitizePlain(*in.Summary)
	}
	if in.Body != nil {
		next.Body = kernel.SanitizeRichText(*in.Body)
		next.ReadingTime = kernel.ReadingTimeMinutes(next.Body)
	}
	if in.Price != nil {
		next.Price = strings.TrimSpace(*in.Price)
	}
	if in.AreaServed != nil {
		next.AreaServed = strings.TrimSpace(*in.AreaServed)
	}
	if in.Slug != nil {
		desired := kernel.Slugify(*in.Slug)
		slug, err := m.uniqueSlug(ctx, desired, id)
		if err != nil {
			return Service{}, err
		}
		next.Slug = slug
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
				now := m.now()
				next.PublishedAt = &now
			}
			becamePublished = existing.Status != kernel.StatusPublished
		}
	}

	var faqs []FAQData
	if in.SetFAQs {
		faqs = prepareFAQs(in.FAQs)
	}

	return m.persistUpdate(ctx, actorID, existing, next, becamePublished, in.SetFAQs, faqs)
}

func (m *Manager) persistUpdate(ctx context.Context, actorID uuid.UUID, prior, next Service, becamePublished, setFAQs bool, faqs []FAQData) (Service, error) {
	snap, err := kernel.MarshalSnapshot(snapshot{
		Title:      prior.Title,
		Slug:       prior.Slug,
		Summary:    prior.Summary,
		Body:       prior.Body,
		Price:      prior.Price,
		AreaServed: prior.AreaServed,
		Status:     prior.Status.String(),
	})
	if err != nil {
		return Service{}, fmt.Errorf("marshal snapshot: %w", err)
	}

	var updated Service
	err = db.RunInTx(ctx, m.pool, func(ctx context.Context, tx pgx.Tx) error {
		author := actorID
		rev, err := m.revisions.CreateTx(ctx, tx, kernel.CreateRevisionInput{
			EntityType: kernel.EntityTypeService,
			EntityID:   prior.ID,
			Snapshot:   snap,
			AuthorID:   &author,
		})
		if err != nil {
			return fmt.Errorf("snapshot revision: %w", err)
		}
		if err := m.bus.Publish(ctx, tx, RevisionCreatedEvent{
			RevisionID: rev.ID,
			EntityType: rev.EntityType,
			EntityID:   rev.EntityID,
			AuthorID:   rev.AuthorID,
			CreatedAt:  rev.CreatedAt,
		}); err != nil {
			return err
		}

		svc, err := m.repo.UpdateTx(ctx, tx, prior.ID, UpdateServiceData{
			Title:           next.Title,
			Slug:            next.Slug,
			Summary:         next.Summary,
			Body:            next.Body,
			Price:           next.Price,
			AreaServed:      next.AreaServed,
			Status:          next.Status,
			PublishedAt:     next.PublishedAt,
			ReadingTime:     next.ReadingTime,
			MetaTitle:       next.MetaTitle,
			MetaDescription: next.MetaDescription,
			CanonicalURL:    next.CanonicalURL,
			NoIndex:         next.NoIndex,
		})
		if err != nil {
			return fmt.Errorf("update service: %w", err)
		}
		updated = svc

		if setFAQs {
			if err := m.repo.ReplaceFAQsTx(ctx, tx, prior.ID, faqs); err != nil {
				return fmt.Errorf("save faqs: %w", err)
			}
		}

		if becamePublished && svc.Published() {
			return m.emitPublished(ctx, tx, svc)
		}
		return nil
	})
	if err != nil {
		return Service{}, err
	}
	updated.FAQs, _ = m.repo.ListFAQs(ctx, updated.ID)
	return updated, nil
}

// Publish transitions a service to PUBLISHED, stamping publishedAt once.
func (m *Manager) Publish(ctx context.Context, actorID, id uuid.UUID) (Service, error) {
	published := kernel.StatusPublished
	return m.Update(ctx, actorID, id, UpdateInput{Status: &published})
}

// Unpublish returns a service to DRAFT. publishedAt is PRESERVED.
func (m *Manager) Unpublish(ctx context.Context, actorID, id uuid.UUID) (Service, error) {
	draft := kernel.StatusDraft
	return m.Update(ctx, actorID, id, UpdateInput{Status: &draft})
}

// Trash soft-deletes a service. Requires delete:service.
func (m *Manager) Trash(ctx context.Context, actorID, id uuid.UUID) error {
	if !m.authz.Can(ctx, actorID, accounts.ActionDelete, accounts.SubjectService) {
		return ErrForbidden
	}
	if _, err := m.repo.GetByID(ctx, id); err != nil {
		return err
	}
	return db.RunInTx(ctx, m.pool, func(ctx context.Context, tx pgx.Tx) error {
		return m.repo.TrashTx(ctx, tx, id)
	})
}

// Restore un-trashes a service. Requires update:service.
func (m *Manager) Restore(ctx context.Context, actorID, id uuid.UUID) error {
	if !m.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectService) {
		return ErrForbidden
	}
	if _, err := m.repo.GetByID(ctx, id); err != nil {
		return err
	}
	return db.RunInTx(ctx, m.pool, func(ctx context.Context, tx pgx.Tx) error {
		return m.repo.RestoreTx(ctx, tx, id)
	})
}

// PermanentDelete hard-deletes a trashed service (FAQs cascade). Requires
// delete:service.
func (m *Manager) PermanentDelete(ctx context.Context, actorID, id uuid.UUID) error {
	if !m.authz.Can(ctx, actorID, accounts.ActionDelete, accounts.SubjectService) {
		return ErrForbidden
	}
	if _, err := m.repo.GetByID(ctx, id); err != nil {
		return err
	}
	return db.RunInTx(ctx, m.pool, func(ctx context.Context, tx pgx.Tx) error {
		return m.repo.PermanentDeleteTx(ctx, tx, id)
	})
}

// Revisions lists a service's revision snapshots (newest first). Requires
// update:service.
func (m *Manager) Revisions(ctx context.Context, actorID, id uuid.UUID) ([]kernel.Revision, error) {
	if !m.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectService) {
		return nil, ErrForbidden
	}
	return m.revisions.List(ctx, kernel.EntityTypeService, id)
}

// RestoreRevision applies a prior revision's scalar fields as a NEW update.
// FAQs are NOT part of the snapshot, so they are left unchanged. Requires
// update:service.
func (m *Manager) RestoreRevision(ctx context.Context, actorID, id, revisionID uuid.UUID) (Service, error) {
	rev, err := m.revisions.Get(ctx, revisionID)
	if err != nil {
		return Service{}, err
	}
	if rev.EntityType != kernel.EntityTypeService || rev.EntityID != id {
		return Service{}, ErrRevisionMismatch
	}
	var snap snapshot
	if err := json.Unmarshal(rev.Snapshot, &snap); err != nil {
		return Service{}, fmt.Errorf("decode revision snapshot: %w", err)
	}
	status := kernel.ParseStatus(snap.Status)
	in := UpdateInput{
		Title:      &snap.Title,
		Slug:       &snap.Slug,
		Summary:    &snap.Summary,
		Body:       &snap.Body,
		Price:      &snap.Price,
		AreaServed: &snap.AreaServed,
		Status:     &status,
	}
	return m.Update(ctx, actorID, id, in)
}

// --- public reads (no auth; published only) ---------------------------------

// PublicBySlug returns a published, non-trashed service (with FAQs) by slug in
// the DEFAULT locale (en). Unchanged from M2 — the locale-aware path is
// PublicBySlugLocale.
func (m *Manager) PublicBySlug(ctx context.Context, slug string) (Service, error) {
	svc, err := m.repo.GetPublishedBySlug(ctx, slug)
	if err != nil {
		return Service{}, err
	}
	svc.FAQs, err = m.repo.ListFAQs(ctx, svc.ID)
	if err != nil {
		return Service{}, err
	}
	return svc, nil
}

// PublicBySlugLocale returns a published service by slug with its content
// overlaid by the active locale, falling back to the base (en) row for any
// missing translation field. When locale is the default (en) or unsupported it
// resolves to the base row (identical to PublicBySlug). FAQs are read from the
// base rows (FAQ localization is deferred — see the M7b note in service.go).
func (m *Manager) PublicBySlugLocale(ctx context.Context, slug string, locale i18n.Locale) (Service, error) {
	var (
		svc Service
		err error
	)
	if locale.IsDefault() || !i18n.IsSupported(locale) {
		svc, err = m.repo.GetPublishedBySlug(ctx, slug)
	} else {
		svc, err = m.repo.GetPublishedInLocaleBySlug(ctx, slug, locale.String())
	}
	if err != nil {
		return Service{}, err
	}
	svc.FAQs, err = m.repo.ListFAQs(ctx, svc.ID)
	if err != nil {
		return Service{}, err
	}
	return svc, nil
}

// PublicList returns a page of published services for the public index.
func (m *Manager) PublicList(ctx context.Context, limit, offset int) ([]Service, int, error) {
	items, err := m.repo.ListPublished(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := m.repo.CountPublished(ctx)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// --- admin reads ------------------------------------------------------------

// AdminList returns a filtered, paginated admin listing plus the total count.
func (m *Manager) AdminList(ctx context.Context, f ListFilter) ([]Service, int, error) {
	items, err := m.repo.List(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	total, err := m.repo.Count(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// AdminTrashed returns the trashed listing plus total.
func (m *Manager) AdminTrashed(ctx context.Context, limit, offset int) ([]Service, int, error) {
	items, err := m.repo.ListTrashed(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := m.repo.CountTrashed(ctx)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Get returns a service by id (with FAQs) for the editor. Requires read:service.
func (m *Manager) Get(ctx context.Context, actorID, id uuid.UUID) (Service, error) {
	if !m.authz.Can(ctx, actorID, accounts.ActionRead, accounts.SubjectService) {
		return Service{}, ErrForbidden
	}
	svc, err := m.repo.GetByID(ctx, id)
	if err != nil {
		return Service{}, err
	}
	svc.FAQs, err = m.repo.ListFAQs(ctx, svc.ID)
	if err != nil {
		return Service{}, err
	}
	return svc, nil
}

// --- per-locale content overlay (M7b-2) --------------------------------------

// TranslationInput is the editor's per-locale content save for a NON-default
// locale. Summary is stripped to plain text and body is sanitized (same kernel
// paths as the base row); structural/citable fields are NOT part of it (they are
// shared on the base row and edited via Update). FAQ localization is deferred —
// see the M7b note below.
type TranslationInput struct {
	Title           string
	Summary         string
	Body            string
	MetaTitle       string
	MetaDescription string
}

// SaveTranslation upserts a NON-default locale's content overlay for a service.
// It requires update:service (like Update; services have no per-author
// ownership). The default locale is rejected (its content lives on the base row
// — callers edit it via Update). No revision snapshot is taken and no event is
// emitted: the translation overlay is content, not a publish-state change.
//
// TODO(M7b): per-locale FAQ question/answer overlays (service_faq_translations)
// are wired in sqlc but the editor block + persistence are deferred; base FAQs
// serve every locale for now.
func (m *Manager) SaveTranslation(ctx context.Context, actorID, id uuid.UUID, locale i18n.Locale, in TranslationInput) error {
	if !i18n.IsSupported(locale) {
		return ErrUnsupportedLocale
	}
	if locale.IsDefault() {
		return ErrDefaultLocaleTranslation
	}
	if !m.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectService) {
		return ErrForbidden
	}
	if _, err := m.repo.GetActiveByID(ctx, id); err != nil {
		return err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return ErrTitleRequired
	}
	t := Translation{
		Locale:          locale.String(),
		Title:           title,
		Summary:         sanitizePlain(in.Summary),
		Body:            kernel.SanitizeRichText(in.Body),
		MetaTitle:       strings.TrimSpace(in.MetaTitle),
		MetaDescription: strings.TrimSpace(in.MetaDescription),
	}
	return db.RunInTx(ctx, m.pool, func(ctx context.Context, tx pgx.Tx) error {
		return m.repo.UpsertTranslationTx(ctx, tx, id, t)
	})
}

// GetInLocale loads a service (with FAQs) for the editor with its content
// overlaid by locale (base fallback per field). The default locale resolves to
// the base row. Requires read:service. Used to populate the editor's per-locale
// tab.
func (m *Manager) GetInLocale(ctx context.Context, actorID, id uuid.UUID, locale i18n.Locale) (Service, error) {
	if !m.authz.Can(ctx, actorID, accounts.ActionRead, accounts.SubjectService) {
		return Service{}, ErrForbidden
	}
	var (
		svc Service
		err error
	)
	if locale.IsDefault() || !i18n.IsSupported(locale) {
		svc, err = m.repo.GetByID(ctx, id)
	} else {
		svc, err = m.repo.GetActiveInLocaleByID(ctx, id, locale.String())
	}
	if err != nil {
		return Service{}, err
	}
	svc.FAQs, err = m.repo.ListFAQs(ctx, svc.ID)
	if err != nil {
		return Service{}, err
	}
	return svc, nil
}

// TranslatedLocales returns the NON-default locales that already have a
// translation row for the service (drives the editor's per-tab "has translation"
// markers). Requires read:service.
func (m *Manager) TranslatedLocales(ctx context.Context, actorID, id uuid.UUID) ([]i18n.Locale, error) {
	if !m.authz.Can(ctx, actorID, accounts.ActionRead, accounts.SubjectService) {
		return nil, ErrForbidden
	}
	raw, err := m.repo.TranslatedLocales(ctx, id)
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

// prepareFAQs sanitizes answers, drops blank rows (no question), and assigns
// dense positions in submitted order — the single place FAQ ordering is decided.
func prepareFAQs(in []FAQInput) []FAQData {
	out := make([]FAQData, 0, len(in))
	pos := 0
	for _, f := range in {
		q := strings.TrimSpace(f.Question)
		if q == "" {
			continue
		}
		out = append(out, FAQData{
			Question: q,
			Answer:   kernel.SanitizeRichText(f.Answer),
			Position: pos,
		})
		pos++
	}
	return out
}

// sanitizePlain trims and strips ALL markup from a plain-text field (summary):
// the public template renders it as text, so any tags are removed defensively.
func sanitizePlain(s string) string {
	return strings.TrimSpace(kernel.SanitizePlainText(s))
}

func (m *Manager) uniqueSlug(ctx context.Context, desired string, excludeID uuid.UUID) (string, error) {
	return kernel.UniqueSlug(ctx, desired, func(ctx context.Context, slug string) (bool, error) {
		return m.repo.SlugTaken(ctx, slug, excludeID)
	})
}

func (m *Manager) emitPublished(ctx context.Context, tx pgx.Tx, s Service) error {
	publishedAt := s.UpdatedAt
	if s.PublishedAt != nil {
		publishedAt = *s.PublishedAt
	}
	return m.bus.Publish(ctx, tx, ContentPublishedEvent{
		EntityType:  kernel.EntityTypeService,
		ServiceID:   s.ID,
		Slug:        s.Slug,
		Title:       s.Title,
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
