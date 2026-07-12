package categories

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/i18n"
)

// --- fakes -------------------------------------------------------------------

type fakeTx struct{ pgx.Tx }

func (fakeTx) Commit(context.Context) error   { return nil }
func (fakeTx) Rollback(context.Context) error { return nil }

type fakeBeginner struct{}

func (fakeBeginner) Begin(context.Context) (pgx.Tx, error) { return fakeTx{}, nil }

// memRepo is an in-memory category Repository.
type memRepo struct {
	mu           sync.Mutex
	cats         map[uuid.UUID]Category
	links        map[uuid.UUID][]uuid.UUID // postID -> categoryIDs
	translations map[uuid.UUID]map[string]Translation
}

func newMemRepo() *memRepo {
	return &memRepo{
		cats:         map[uuid.UUID]Category{},
		links:        map[uuid.UUID][]uuid.UUID{},
		translations: map[uuid.UUID]map[string]Translation{},
	}
}

func (m *memRepo) CreateTx(_ context.Context, _ pgx.Tx, in CreateCategoryData) (Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := Category{ID: uuid.New(), Name: in.Name, Slug: in.Slug, Description: in.Description, ParentID: in.ParentID}
	m.cats[c.ID] = c
	return c, nil
}

func (m *memRepo) UpdateTx(_ context.Context, _ pgx.Tx, id uuid.UUID, in UpdateCategoryData) (Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cats[id]
	if !ok {
		return Category{}, ErrNotFound
	}
	c.Name, c.Slug, c.Description, c.ParentID = in.Name, in.Slug, in.Description, in.ParentID
	m.cats[id] = c
	return c, nil
}

func (m *memRepo) DeleteTx(_ context.Context, _ pgx.Tx, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.cats, id)
	return nil
}

func (m *memRepo) GetByID(_ context.Context, id uuid.UUID) (Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cats[id]
	if !ok {
		return Category{}, ErrNotFound
	}
	return c, nil
}

func (m *memRepo) GetBySlug(_ context.Context, slug string) (Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.cats {
		if c.Slug == slug {
			return c, nil
		}
	}
	return Category{}, ErrNotFound
}

func (m *memRepo) SlugTaken(_ context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.cats {
		if c.Slug == slug && c.ID != excludeID {
			return true, nil
		}
	}
	return false, nil
}

func (m *memRepo) ListAll(_ context.Context) ([]Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Category, 0, len(m.cats))
	for _, c := range m.cats {
		out = append(out, c)
	}
	return out, nil
}

func (m *memRepo) ListChildren(_ context.Context, parentID uuid.UUID) ([]Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Category
	for _, c := range m.cats {
		if c.ParentID != nil && *c.ParentID == parentID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memRepo) List(_ context.Context, _, _ int) ([]Category, error) {
	return m.ListAll(context.Background())
}
func (m *memRepo) Count(_ context.Context) (int, error) { return len(m.cats), nil }

func (m *memRepo) AttachTx(_ context.Context, _ pgx.Tx, postID, categoryID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.links[postID] = append(m.links[postID], categoryID)
	return nil
}

func (m *memRepo) DetachAllTx(_ context.Context, _ pgx.Tx, postID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.links, postID)
	return nil
}

func (m *memRepo) ListForPost(_ context.Context, postID uuid.UUID) ([]Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Category
	for _, id := range m.links[postID] {
		if c, ok := m.cats[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memRepo) IDsForPost(_ context.Context, postID uuid.UUID) ([]uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]uuid.UUID(nil), m.links[postID]...), nil
}

func (m *memRepo) ListPublishedPostIDsInCategory(_ context.Context, _ uuid.UUID, _, _ int) ([]uuid.UUID, error) {
	return nil, nil
}

func (m *memRepo) CountPublishedPostsInCategory(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}

// --- per-locale translation overlay (M7b-3) fakes ---------------------------

func (m *memRepo) UpsertTranslationTx(_ context.Context, _ pgx.Tx, categoryID uuid.UUID, t Translation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.translations[categoryID] == nil {
		m.translations[categoryID] = map[string]Translation{}
	}
	m.translations[categoryID][t.Locale] = t
	return nil
}

func (m *memRepo) GetTranslation(_ context.Context, categoryID uuid.UUID, locale string) (Translation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.translations[categoryID][locale]; ok {
		return t, nil
	}
	return Translation{}, ErrNotFound
}

func (m *memRepo) ListTranslations(_ context.Context, categoryID uuid.UUID) ([]Translation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Translation, 0, len(m.translations[categoryID]))
	for _, t := range m.translations[categoryID] {
		out = append(out, t)
	}
	return out, nil
}

func (m *memRepo) TranslatedLocales(_ context.Context, categoryID uuid.UUID) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.translations[categoryID]))
	for loc := range m.translations[categoryID] {
		out = append(out, loc)
	}
	return out, nil
}

func (m *memRepo) DeleteTranslationTx(_ context.Context, _ pgx.Tx, categoryID uuid.UUID, locale string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.translations[categoryID], locale)
	return nil
}

func (m *memRepo) GetInLocaleByID(_ context.Context, id uuid.UUID, locale string) (Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cats[id]
	if !ok {
		return Category{}, ErrNotFound
	}
	return overlayCategory(c, m.translations[id][locale]), nil
}

func (m *memRepo) GetPublishedInLocaleBySlug(_ context.Context, slug, locale string) (Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.cats {
		if c.Slug == slug {
			return overlayCategory(c, m.translations[c.ID][locale]), nil
		}
	}
	return Category{}, ErrNotFound
}

// overlayCategory applies a translation's non-empty fields onto a base category.
func overlayCategory(base Category, t Translation) Category {
	if t.Name != "" {
		base.Name = t.Name
	}
	if t.Description != "" {
		base.Description = t.Description
	}
	return base
}

// allowAuthorizer always grants.
type allowAuthorizer struct{}

func (allowAuthorizer) Can(context.Context, uuid.UUID, string, string) bool { return true }

// denyAuthorizer always denies.
type denyAuthorizer struct{}

func (denyAuthorizer) Can(context.Context, uuid.UUID, string, string) bool { return false }

func newSvc(repo Repository, authz Authorizer) *Service {
	return NewService(fakeBeginner{}, repo, authz)
}

// --- tests -------------------------------------------------------------------

func TestCreate_DerivesAndDedupesSlug(t *testing.T) {
	repo := newMemRepo()
	svc := newSvc(repo, allowAuthorizer{})
	ctx := context.Background()
	actor := uuid.New()

	a, err := svc.Create(ctx, actor, CreateInput{Name: "Go Tips"})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	if a.Slug != "go-tips" {
		t.Fatalf("slug = %q, want go-tips", a.Slug)
	}
	b, err := svc.Create(ctx, actor, CreateInput{Name: "Go Tips"})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	if b.Slug != "go-tips-2" {
		t.Fatalf("dedupe slug = %q, want go-tips-2", b.Slug)
	}
}

func TestCreate_SanitizesDescription(t *testing.T) {
	repo := newMemRepo()
	svc := newSvc(repo, allowAuthorizer{})
	c, err := svc.Create(context.Background(), uuid.New(), CreateInput{
		Name:        "Sec",
		Description: `<p>ok</p><script>alert(1)</script>`,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if want := "<p>ok</p>"; c.Description != want {
		t.Fatalf("description = %q, want %q (script stripped)", c.Description, want)
	}
}

func TestCreate_NameRequired(t *testing.T) {
	svc := newSvc(newMemRepo(), allowAuthorizer{})
	if _, err := svc.Create(context.Background(), uuid.New(), CreateInput{Name: "   "}); err != ErrNameRequired {
		t.Fatalf("err = %v, want ErrNameRequired", err)
	}
}

func TestCreate_Forbidden(t *testing.T) {
	svc := newSvc(newMemRepo(), denyAuthorizer{})
	if _, err := svc.Create(context.Background(), uuid.New(), CreateInput{Name: "X"}); err != ErrForbidden {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestUpdate_ParentCycleRejected(t *testing.T) {
	repo := newMemRepo()
	svc := newSvc(repo, allowAuthorizer{})
	ctx := context.Background()
	actor := uuid.New()

	root, _ := svc.Create(ctx, actor, CreateInput{Name: "Root"})
	child, _ := svc.Create(ctx, actor, CreateInput{Name: "Child", ParentID: &root.ID})

	// Making root a child of its own descendant (child) must be rejected.
	_, err := svc.Update(ctx, actor, root.ID, UpdateInput{SetParent: true, ParentID: &child.ID})
	if err != ErrParentCycle {
		t.Fatalf("err = %v, want ErrParentCycle", err)
	}

	// A category may not be its own parent.
	_, err = svc.Update(ctx, actor, root.ID, UpdateInput{SetParent: true, ParentID: &root.ID})
	if err != ErrParentCycle {
		t.Fatalf("self-parent err = %v, want ErrParentCycle", err)
	}
}

func TestCreate_ParentNotFound(t *testing.T) {
	svc := newSvc(newMemRepo(), allowAuthorizer{})
	ghost := uuid.New()
	if _, err := svc.Create(context.Background(), uuid.New(), CreateInput{Name: "X", ParentID: &ghost}); err != ErrParentNotFound {
		t.Fatalf("err = %v, want ErrParentNotFound", err)
	}
}

func TestBuildTree_DepthOrdering(t *testing.T) {
	root := Category{ID: uuid.New(), Name: "Root"}
	childID := uuid.New()
	child := Category{ID: childID, Name: "Child", ParentID: &root.ID}
	grand := Category{ID: uuid.New(), Name: "Grand", ParentID: &childID}
	// Pass in a deliberately scrambled order.
	nodes := BuildTree([]Category{grand, root, child})

	if len(nodes) != 3 {
		t.Fatalf("len = %d, want 3", len(nodes))
	}
	if nodes[0].Category.ID != root.ID || nodes[0].Depth != 0 {
		t.Fatalf("node0 = %+v, want root depth 0", nodes[0])
	}
	if nodes[1].Category.ID != child.ID || nodes[1].Depth != 1 {
		t.Fatalf("node1 = %+v, want child depth 1", nodes[1])
	}
	if nodes[2].Category.ID != grand.ID || nodes[2].Depth != 2 {
		t.Fatalf("node2 = %+v, want grand depth 2", nodes[2])
	}
}

func TestAssignTx_ReplacesSet(t *testing.T) {
	repo := newMemRepo()
	svc := newSvc(repo, allowAuthorizer{})
	ctx := context.Background()
	actor := uuid.New()
	a, _ := svc.Create(ctx, actor, CreateInput{Name: "A"})
	b, _ := svc.Create(ctx, actor, CreateInput{Name: "B"})
	post := uuid.New()

	if err := svc.AssignTx(ctx, fakeTx{}, post, []uuid.UUID{a.ID, b.ID, a.ID}); err != nil {
		t.Fatalf("assign: %v", err)
	}
	ids, _ := repo.IDsForPost(ctx, post)
	if len(ids) != 2 {
		t.Fatalf("after assign len = %d, want 2 (deduped)", len(ids))
	}

	// Re-assign a smaller set: the previous set must be fully replaced.
	if err := svc.AssignTx(ctx, fakeTx{}, post, []uuid.UUID{b.ID}); err != nil {
		t.Fatalf("reassign: %v", err)
	}
	ids, _ = repo.IDsForPost(ctx, post)
	if len(ids) != 1 || ids[0] != b.ID {
		t.Fatalf("after reassign ids = %v, want [b]", ids)
	}
}

func TestAssignTx_SkipsUnknownIDs(t *testing.T) {
	repo := newMemRepo()
	svc := newSvc(repo, allowAuthorizer{})
	ctx := context.Background()
	a, _ := svc.Create(ctx, uuid.New(), CreateInput{Name: "A"})
	post := uuid.New()
	ghost := uuid.New()

	if err := svc.AssignTx(ctx, fakeTx{}, post, []uuid.UUID{a.ID, ghost}); err != nil {
		t.Fatalf("assign: %v", err)
	}
	ids, _ := repo.IDsForPost(ctx, post)
	if len(ids) != 1 || ids[0] != a.ID {
		t.Fatalf("ids = %v, want [a] (ghost skipped)", ids)
	}
}

// --- per-locale content overlay (M7b-3) tests --------------------------------

func TestSaveTranslation(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		authz   Authorizer
		locale  string
		in      TranslationInput
		wantErr error
	}{
		{name: "happy", authz: allowAuthorizer{}, locale: "de", in: TranslationInput{Name: "Kategorie"}},
		{name: "default locale rejected", authz: allowAuthorizer{}, locale: "en", in: TranslationInput{Name: "X"}, wantErr: ErrDefaultLocaleTranslation},
		{name: "unsupported rejected", authz: allowAuthorizer{}, locale: "xx", in: TranslationInput{Name: "X"}, wantErr: ErrUnsupportedLocale},
		{name: "empty name rejected", authz: allowAuthorizer{}, locale: "de", in: TranslationInput{Name: "   "}, wantErr: ErrNameRequired},
		{name: "forbidden", authz: denyAuthorizer{}, locale: "de", in: TranslationInput{Name: "X"}, wantErr: ErrForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMemRepo()
			// A base category to translate (created with an always-allow svc).
			base, _ := newSvc(repo, allowAuthorizer{}).Create(ctx, uuid.New(), CreateInput{Name: "Category"})
			svc := newSvc(repo, tc.authz)
			err := svc.SaveTranslation(ctx, uuid.New(), base.ID, i18n.Locale(tc.locale), tc.in)
			if err != tc.wantErr {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil {
				if _, ok := repo.translations[base.ID][tc.locale]; !ok {
					t.Fatalf("translation not stored for %s", tc.locale)
				}
			}
		})
	}
}

func TestSaveTranslation_SanitizesDescription(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := newSvc(repo, allowAuthorizer{})
	base, _ := svc.Create(ctx, uuid.New(), CreateInput{Name: "Category"})

	err := svc.SaveTranslation(ctx, uuid.New(), base.ID, i18n.LocaleDE, TranslationInput{
		Name:        "Kategorie",
		Description: `<p>ok</p><script>alert(1)</script>`,
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := repo.translations[base.ID]["de"].Description; got != "<p>ok</p>" {
		t.Fatalf("description = %q, want script stripped", got)
	}
}

func TestGetInLocale(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := newSvc(repo, allowAuthorizer{})
	base, _ := svc.Create(ctx, uuid.New(), CreateInput{Name: "Category", Description: "<p>base</p>"})
	if err := svc.SaveTranslation(ctx, uuid.New(), base.ID, i18n.LocaleDE, TranslationInput{Name: "Kategorie"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Overlay: name is translated, description falls back to base.
	de, err := svc.GetInLocale(ctx, uuid.New(), base.ID, i18n.LocaleDE)
	if err != nil {
		t.Fatalf("get de: %v", err)
	}
	if de.Name != "Kategorie" {
		t.Fatalf("de name = %q, want Kategorie", de.Name)
	}
	if de.Description != "<p>base</p>" {
		t.Fatalf("de description = %q, want base fallback", de.Description)
	}

	// Default locale returns the base row untouched.
	en, err := svc.GetInLocale(ctx, uuid.New(), base.ID, i18n.LocaleEN)
	if err != nil {
		t.Fatalf("get en: %v", err)
	}
	if en.Name != "Category" {
		t.Fatalf("en name = %q, want Category", en.Name)
	}

	// Forbidden read.
	if _, err := newSvc(repo, denyAuthorizer{}).GetInLocale(ctx, uuid.New(), base.ID, i18n.LocaleDE); err != ErrForbidden {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestTranslatedLocales(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := newSvc(repo, allowAuthorizer{})
	base, _ := svc.Create(ctx, uuid.New(), CreateInput{Name: "Category"})
	_ = svc.SaveTranslation(ctx, uuid.New(), base.ID, i18n.LocaleDE, TranslationInput{Name: "Kategorie"})
	_ = svc.SaveTranslation(ctx, uuid.New(), base.ID, i18n.LocaleRU, TranslationInput{Name: "Категория"})

	locs, err := svc.TranslatedLocales(ctx, uuid.New(), base.ID)
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	if len(locs) != 2 {
		t.Fatalf("locales = %v, want 2 (de, ru; en excluded)", locs)
	}
	for _, l := range locs {
		if l.IsDefault() {
			t.Fatalf("default locale must be excluded, got %v", locs)
		}
	}
}

func TestPublicBySlugLocale(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := newSvc(repo, allowAuthorizer{})
	base, _ := svc.Create(ctx, uuid.New(), CreateInput{Name: "Category"})
	_ = svc.SaveTranslation(ctx, uuid.New(), base.ID, i18n.LocaleDE, TranslationInput{Name: "Kategorie"})

	de, err := svc.PublicBySlugLocale(ctx, base.Slug, i18n.LocaleDE)
	if err != nil {
		t.Fatalf("de: %v", err)
	}
	if de.Name != "Kategorie" {
		t.Fatalf("de name = %q, want Kategorie", de.Name)
	}

	// Default locale resolves to the base row.
	en, err := svc.PublicBySlugLocale(ctx, base.Slug, i18n.LocaleEN)
	if err != nil {
		t.Fatalf("en: %v", err)
	}
	if en.Name != "Category" {
		t.Fatalf("en name = %q, want Category", en.Name)
	}
}
