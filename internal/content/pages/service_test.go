package pages

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/platform/events"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
)

// --- fakes -------------------------------------------------------------------

type fakeTx struct{ pgx.Tx }

func (fakeTx) Commit(context.Context) error   { return nil }
func (fakeTx) Rollback(context.Context) error { return nil }

type fakeBeginner struct{}

func (fakeBeginner) Begin(context.Context) (pgx.Tx, error) { return fakeTx{}, nil }

type memRepo struct {
	mu           sync.Mutex
	pages        map[uuid.UUID]Page
	translations map[uuid.UUID]map[string]Translation
}

func newMemRepo() *memRepo {
	return &memRepo{
		pages:        map[uuid.UUID]Page{},
		translations: map[uuid.UUID]map[string]Translation{},
	}
}

// --- per-locale translation overlay (M7b-2) fakes ---------------------------

func (m *memRepo) UpsertTranslationTx(_ context.Context, _ pgx.Tx, pageID uuid.UUID, t Translation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.translations[pageID] == nil {
		m.translations[pageID] = map[string]Translation{}
	}
	m.translations[pageID][t.Locale] = t
	return nil
}

func (m *memRepo) GetTranslation(_ context.Context, pageID uuid.UUID, locale string) (Translation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.translations[pageID][locale]; ok {
		return t, nil
	}
	return Translation{}, ErrNotFound
}

func (m *memRepo) ListTranslations(_ context.Context, pageID uuid.UUID) ([]Translation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Translation, 0, len(m.translations[pageID]))
	for _, t := range m.translations[pageID] {
		out = append(out, t)
	}
	return out, nil
}

func (m *memRepo) TranslatedLocales(_ context.Context, pageID uuid.UUID) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.translations[pageID]))
	for loc := range m.translations[pageID] {
		out = append(out, loc)
	}
	return out, nil
}

func (m *memRepo) DeleteTranslationTx(_ context.Context, _ pgx.Tx, pageID uuid.UUID, locale string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.translations[pageID], locale)
	return nil
}

func (m *memRepo) GetActiveInLocaleByID(_ context.Context, id uuid.UUID, locale string) (Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pages[id]
	if !ok || p.DeletedAt != nil {
		return Page{}, ErrNotFound
	}
	return overlayPage(p, m.translations[id][locale]), nil
}

func (m *memRepo) GetPublishedInLocaleBySlug(_ context.Context, slug, locale string) (Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pages {
		if p.Slug == slug && p.Status == kernel.StatusPublished && p.DeletedAt == nil {
			return overlayPage(p, m.translations[p.ID][locale]), nil
		}
	}
	return Page{}, ErrNotFound
}

// overlayPage applies a translation's non-empty fields onto a base page.
func overlayPage(base Page, t Translation) Page {
	if t.Title != "" {
		base.Title = t.Title
	}
	if t.Body != "" {
		base.Body = t.Body
	}
	// meta_title/meta_description overlay with base fallback; canonical_url/noindex
	// are structural (base row only) and are NOT overridden per-locale.
	if t.MetaTitle != "" {
		base.MetaTitle = t.MetaTitle
	}
	if t.MetaDescription != "" {
		base.MetaDescription = t.MetaDescription
	}
	return base
}

func (m *memRepo) CreateTx(_ context.Context, _ pgx.Tx, in CreatePageData) (Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := Page{
		ID: uuid.New(), Title: in.Title, Slug: in.Slug, Body: in.Body,
		Status: in.Status, PublishedAt: in.PublishedAt, ParentID: in.ParentID,
		Template: in.Template, ReadingTime: in.ReadingTime,
		MetaTitle: in.MetaTitle, MetaDescription: in.MetaDescription,
		CanonicalURL: in.CanonicalURL, NoIndex: in.NoIndex,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	m.pages[p.ID] = p
	return p, nil
}

func (m *memRepo) UpdateTx(_ context.Context, _ pgx.Tx, id uuid.UUID, in UpdatePageData) (Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pages[id]
	if !ok {
		return Page{}, ErrNotFound
	}
	p.Title, p.Slug, p.Body, p.Status = in.Title, in.Slug, in.Body, in.Status
	p.PublishedAt, p.ParentID, p.Template, p.ReadingTime = in.PublishedAt, in.ParentID, in.Template, in.ReadingTime
	p.MetaTitle, p.MetaDescription = in.MetaTitle, in.MetaDescription
	p.CanonicalURL, p.NoIndex = in.CanonicalURL, in.NoIndex
	p.UpdatedAt = time.Now()
	m.pages[id] = p
	return p, nil
}

func (m *memRepo) GetByID(_ context.Context, id uuid.UUID) (Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pages[id]
	if !ok {
		return Page{}, ErrNotFound
	}
	return p, nil
}

func (m *memRepo) GetActiveByID(ctx context.Context, id uuid.UUID) (Page, error) {
	p, err := m.GetByID(ctx, id)
	if err != nil {
		return Page{}, err
	}
	if p.DeletedAt != nil {
		return Page{}, ErrNotFound
	}
	return p, nil
}

func (m *memRepo) GetPublishedBySlug(_ context.Context, slug string) (Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pages {
		if p.Slug == slug && p.Published() {
			return p, nil
		}
	}
	return Page{}, ErrNotFound
}

func (m *memRepo) SlugTaken(_ context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pages {
		if p.Slug == slug && p.ID != excludeID {
			return true, nil
		}
	}
	return false, nil
}

func (m *memRepo) List(context.Context, ListFilter) ([]Page, error) { return nil, nil }
func (m *memRepo) Count(context.Context, ListFilter) (int, error)   { return 0, nil }

func (m *memRepo) ListAllActive(_ context.Context) ([]Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Page
	for _, p := range m.pages {
		if p.DeletedAt == nil {
			out = append(out, p)
		}
	}
	return out, nil
}

func (m *memRepo) ListChildren(_ context.Context, parentID uuid.UUID) ([]Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Page
	for _, p := range m.pages {
		if p.ParentID != nil && *p.ParentID == parentID && p.DeletedAt == nil {
			out = append(out, p)
		}
	}
	return out, nil
}

func (m *memRepo) ListTrashed(context.Context, int, int) ([]Page, error) { return nil, nil }
func (m *memRepo) CountTrashed(context.Context) (int, error)             { return 0, nil }
func (m *memRepo) ListPublished(context.Context, int, int) ([]Page, error) {
	return nil, nil
}
func (m *memRepo) CountPublished(context.Context) (int, error) { return 0, nil }

func (m *memRepo) SitemapItems(context.Context) ([]kernel.SitemapItem, error) { return nil, nil }

func (m *memRepo) TrashTx(_ context.Context, _ pgx.Tx, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.pages[id]
	now := time.Now()
	p.DeletedAt = &now
	m.pages[id] = p
	return nil
}

func (m *memRepo) RestoreTx(_ context.Context, _ pgx.Tx, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.pages[id]
	p.DeletedAt = nil
	m.pages[id] = p
	return nil
}

func (m *memRepo) PermanentDeleteTx(_ context.Context, _ pgx.Tx, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pages, id)
	return nil
}

type memRevisions struct {
	mu   sync.Mutex
	rows map[uuid.UUID]kernel.Revision
}

func newMemRevisions() *memRevisions {
	return &memRevisions{rows: map[uuid.UUID]kernel.Revision{}}
}

func (m *memRevisions) CreateTx(_ context.Context, _ pgx.Tx, in kernel.CreateRevisionInput) (kernel.Revision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := kernel.Revision{
		ID: uuid.New(), EntityType: in.EntityType, EntityID: in.EntityID,
		Snapshot: in.Snapshot, AuthorID: in.AuthorID, CreatedAt: time.Now(),
	}
	m.rows[r.ID] = r
	return r, nil
}

func (m *memRevisions) List(_ context.Context, entityType string, entityID uuid.UUID) ([]kernel.Revision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []kernel.Revision
	for _, r := range m.rows {
		if r.EntityType == entityType && r.EntityID == entityID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memRevisions) Get(_ context.Context, id uuid.UUID) (kernel.Revision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok {
		return kernel.Revision{}, ErrNotFound
	}
	return r, nil
}

type fakeAuthz struct{ allowed map[uuid.UUID]bool }

func (a fakeAuthz) Can(_ context.Context, userID uuid.UUID, _, _ string) bool {
	return a.allowed[userID]
}

type nullBus struct{}

func (nullBus) Publish(context.Context, pgx.Tx, events.Event) error { return nil }

func fixedClock(t time.Time) Clock { return func() time.Time { return t } }

func newTestService(repo Repository, revs kernel.RevisionRepository, authz Authorizer, bus Publisher, now time.Time) *Service {
	return NewService(fakeBeginner{}, repo, revs, authz, bus, fixedClock(now))
}

func allow(ids ...uuid.UUID) fakeAuthz {
	m := map[uuid.UUID]bool{}
	for _, id := range ids {
		m[id] = true
	}
	return fakeAuthz{allowed: m}
}

// --- tests -------------------------------------------------------------------

func TestCreate_SanitizesBodyAndSlug(t *testing.T) {
	actor := uuid.New()
	svc := newTestService(newMemRepo(), newMemRevisions(), allow(actor), nullBus{}, time.Now())

	p, err := svc.Create(context.Background(), actor, CreateInput{
		Title: "About Us", Body: `<p>hi</p><script>x</script>`,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.Slug != "about-us" {
		t.Errorf("slug = %q", p.Slug)
	}
	if p.Body != "<p>hi</p>" {
		t.Errorf("body not sanitized: %q", p.Body)
	}
	if p.Template != TemplateDefault {
		t.Errorf("template = %q, want default", p.Template)
	}
}

func TestCreate_DeniedWithoutPermission(t *testing.T) {
	actor := uuid.New()
	svc := newTestService(newMemRepo(), newMemRevisions(), fakeAuthz{allowed: map[uuid.UUID]bool{}}, nullBus{}, time.Now())
	if _, err := svc.Create(context.Background(), actor, CreateInput{Title: "X"}); err != ErrForbidden {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestTemplateAllowList_RejectsUnknown(t *testing.T) {
	actor := uuid.New()
	svc := newTestService(newMemRepo(), newMemRevisions(), allow(actor), nullBus{}, time.Now())
	p, err := svc.Create(context.Background(), actor, CreateInput{Title: "T", Template: "evil-template"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.Template != TemplateDefault {
		t.Errorf("unknown template not normalized to default: %q", p.Template)
	}
	// A valid one survives.
	p2, _ := svc.Create(context.Background(), actor, CreateInput{Title: "L", Template: TemplateLanding})
	if p2.Template != TemplateLanding {
		t.Errorf("valid template = %q, want landing", p2.Template)
	}
}

func TestPublish_StampsOnceAndPreserves(t *testing.T) {
	actor := uuid.New()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	svc := newTestService(newMemRepo(), newMemRevisions(), allow(actor), nullBus{}, t0)

	p, _ := svc.Create(context.Background(), actor, CreateInput{Title: "P", Body: "<p>b</p>"})
	pub, err := svc.Publish(context.Background(), actor, p.ID)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if pub.PublishedAt == nil || !pub.PublishedAt.Equal(t0) {
		t.Fatalf("first publish stamp = %v, want %v", pub.PublishedAt, t0)
	}
	svc.now = fixedClock(t0.Add(48 * time.Hour))
	if _, err := svc.Unpublish(context.Background(), actor, p.ID); err != nil {
		t.Fatalf("unpublish: %v", err)
	}
	re, _ := svc.Publish(context.Background(), actor, p.ID)
	if re.PublishedAt == nil || !re.PublishedAt.Equal(t0) {
		t.Errorf("re-publish must preserve %v, got %v", t0, re.PublishedAt)
	}
}

func TestParentCycle_Rejected(t *testing.T) {
	actor := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(), allow(actor), nullBus{}, time.Now())

	root, _ := svc.Create(context.Background(), actor, CreateInput{Title: "Root"})
	child, _ := svc.Create(context.Background(), actor, CreateInput{Title: "Child", ParentID: &root.ID})

	// A page cannot be its own parent.
	if _, err := svc.Update(context.Background(), actor, root.ID, UpdateInput{SetParent: true, ParentID: &root.ID}); err != ErrParentCycle {
		t.Fatalf("self-parent = %v, want ErrParentCycle", err)
	}
	// Root cannot become a child of its own descendant (child).
	if _, err := svc.Update(context.Background(), actor, root.ID, UpdateInput{SetParent: true, ParentID: &child.ID}); err != ErrParentCycle {
		t.Fatalf("descendant-as-parent = %v, want ErrParentCycle", err)
	}
	// A legitimate re-parent (child under root, already the case) is fine.
	if _, err := svc.Update(context.Background(), actor, child.ID, UpdateInput{SetParent: true, ParentID: &root.ID}); err != nil {
		t.Fatalf("legit reparent = %v, want nil", err)
	}
}

func TestParent_NotFoundRejected(t *testing.T) {
	actor := uuid.New()
	svc := newTestService(newMemRepo(), newMemRevisions(), allow(actor), nullBus{}, time.Now())
	missing := uuid.New()
	if _, err := svc.Create(context.Background(), actor, CreateInput{Title: "X", ParentID: &missing}); err != ErrParentNotFound {
		t.Fatalf("create under missing parent = %v, want ErrParentNotFound", err)
	}
}

func TestUpdate_SnapshotsRevision(t *testing.T) {
	actor := uuid.New()
	repo := newMemRepo()
	revs := newMemRevisions()
	svc := newTestService(repo, revs, allow(actor), nullBus{}, time.Now())

	p, _ := svc.Create(context.Background(), actor, CreateInput{Title: "V1", Body: "<p>one</p>"})
	nt, nb := "V2", "<p>two</p>"
	if _, err := svc.Update(context.Background(), actor, p.ID, UpdateInput{Title: &nt, Body: &nb}); err != nil {
		t.Fatalf("update: %v", err)
	}
	list, _ := revs.List(context.Background(), kernel.EntityTypePage, p.ID)
	if len(list) != 1 {
		t.Fatalf("want 1 revision, got %d", len(list))
	}
}

func TestRestoreRevision_ReappliesSnapshot(t *testing.T) {
	actor := uuid.New()
	repo := newMemRepo()
	revs := newMemRevisions()
	svc := newTestService(repo, revs, allow(actor), nullBus{}, time.Now())

	root, _ := svc.Create(context.Background(), actor, CreateInput{Title: "Original", Body: "<p>v1</p>", Template: TemplateLanding})
	nt, nb := "Changed", "<p>v2</p>"
	if _, err := svc.Update(context.Background(), actor, root.ID, UpdateInput{Title: &nt, Body: &nb}); err != nil {
		t.Fatalf("update: %v", err)
	}
	list, _ := revs.List(context.Background(), kernel.EntityTypePage, root.ID)
	restored, err := svc.RestoreRevision(context.Background(), actor, root.ID, list[0].ID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Title != "Original" || restored.Body != "<p>v1</p>" || restored.Template != TemplateLanding {
		t.Errorf("restore did not reapply snapshot: %+v", restored)
	}
}

func TestSlugDedupe(t *testing.T) {
	actor := uuid.New()
	svc := newTestService(newMemRepo(), newMemRevisions(), allow(actor), nullBus{}, time.Now())
	p1, _ := svc.Create(context.Background(), actor, CreateInput{Title: "Same Title"})
	p2, _ := svc.Create(context.Background(), actor, CreateInput{Title: "Same Title"})
	if p1.Slug != "same-title" || p2.Slug != "same-title-2" {
		t.Errorf("dedupe failed: %q / %q", p1.Slug, p2.Slug)
	}
}

func TestSEOMeta_RoundTripsAndTranslationOverlay(t *testing.T) {
	actor := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(), allow(actor), nullBus{}, time.Now())

	p, err := svc.Create(context.Background(), actor, CreateInput{
		Title: "SEO", Body: "<p>b</p>",
		MetaTitle: "  Base Meta  ", MetaDescription: "Base desc",
		CanonicalURL: " https://x/seo ", NoIndex: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.MetaTitle != "Base Meta" || p.MetaDescription != "Base desc" {
		t.Errorf("base meta not round-tripped: %q / %q", p.MetaTitle, p.MetaDescription)
	}
	if p.CanonicalURL != "https://x/seo" || !p.NoIndex {
		t.Errorf("structural SEO not persisted: canonical=%q noindex=%v", p.CanonicalURL, p.NoIndex)
	}

	de := mustLocale(t, "de")
	if err := svc.SaveTranslation(context.Background(), actor, p.ID, de, TranslationInput{
		Title: "DE", MetaTitle: "DE Meta",
	}); err != nil {
		t.Fatalf("save translation: %v", err)
	}
	got, err := svc.GetInLocale(context.Background(), actor, p.ID, de)
	if err != nil {
		t.Fatalf("get in locale: %v", err)
	}
	if got.MetaTitle != "DE Meta" {
		t.Errorf("meta_title overlay = %q, want DE Meta", got.MetaTitle)
	}
	if got.MetaDescription != "Base desc" {
		t.Errorf("meta_description should fall back to base, got %q", got.MetaDescription)
	}
	if got.CanonicalURL != "https://x/seo" || !got.NoIndex {
		t.Errorf("structural SEO must not be per-locale: canonical=%q noindex=%v", got.CanonicalURL, got.NoIndex)
	}
}

func mustLocale(t *testing.T, s string) i18n.Locale {
	t.Helper()
	l, ok := i18n.Parse(s)
	if !ok {
		t.Fatalf("locale %q not supported", s)
	}
	return l
}

func TestAncestors_RootFirstAndCycleSafe(t *testing.T) {
	actor := uuid.New()
	svc := newTestService(newMemRepo(), newMemRevisions(), allow(actor), nullBus{}, time.Now())
	root, _ := svc.Create(context.Background(), actor, CreateInput{Title: "Root"})
	mid, _ := svc.Create(context.Background(), actor, CreateInput{Title: "Mid", ParentID: &root.ID})
	leaf, _ := svc.Create(context.Background(), actor, CreateInput{Title: "Leaf", ParentID: &mid.ID})

	chain, err := svc.Ancestors(context.Background(), leaf)
	if err != nil {
		t.Fatalf("ancestors: %v", err)
	}
	if len(chain) != 2 || chain[0].Title != "Root" || chain[1].Title != "Mid" {
		t.Errorf("ancestor chain = %v, want [Root Mid]", titles(chain))
	}
}

func titles(ps []Page) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Title)
	}
	return out
}

// compile-time: keep accounts import used (role constants referenced by handlers).
var _ = accounts.RoleEditor
