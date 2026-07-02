package tags

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
)

type fakeTx struct{ pgx.Tx }

func (fakeTx) Commit(context.Context) error   { return nil }
func (fakeTx) Rollback(context.Context) error { return nil }

type fakeBeginner struct{}

func (fakeBeginner) Begin(context.Context) (pgx.Tx, error) { return fakeTx{}, nil }

type memRepo struct {
	mu           sync.Mutex
	tags         map[uuid.UUID]Tag
	links        map[uuid.UUID][]uuid.UUID
	translations map[uuid.UUID]map[string]Translation
}

func newMemRepo() *memRepo {
	return &memRepo{
		tags:         map[uuid.UUID]Tag{},
		links:        map[uuid.UUID][]uuid.UUID{},
		translations: map[uuid.UUID]map[string]Translation{},
	}
}

func (m *memRepo) CreateTx(_ context.Context, _ pgx.Tx, in CreateTagData) (Tag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := Tag{ID: uuid.New(), Name: in.Name, Slug: in.Slug}
	m.tags[t.ID] = t
	return t, nil
}

func (m *memRepo) UpdateTx(_ context.Context, _ pgx.Tx, id uuid.UUID, in UpdateTagData) (Tag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tags[id]
	if !ok {
		return Tag{}, ErrNotFound
	}
	t.Name, t.Slug = in.Name, in.Slug
	m.tags[id] = t
	return t, nil
}

func (m *memRepo) DeleteTx(_ context.Context, _ pgx.Tx, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tags, id)
	return nil
}

func (m *memRepo) GetByID(_ context.Context, id uuid.UUID) (Tag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tags[id]
	if !ok {
		return Tag{}, ErrNotFound
	}
	return t, nil
}

func (m *memRepo) GetBySlug(_ context.Context, slug string) (Tag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tags {
		if t.Slug == slug {
			return t, nil
		}
	}
	return Tag{}, ErrNotFound
}

func (m *memRepo) SlugTaken(_ context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tags {
		if t.Slug == slug && t.ID != excludeID {
			return true, nil
		}
	}
	return false, nil
}

func (m *memRepo) ListAll(_ context.Context) ([]Tag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Tag, 0, len(m.tags))
	for _, t := range m.tags {
		out = append(out, t)
	}
	return out, nil
}

func (m *memRepo) List(_ context.Context, _, _ int) ([]Tag, error) {
	return m.ListAll(context.Background())
}
func (m *memRepo) Count(_ context.Context) (int, error) { return len(m.tags), nil }

func (m *memRepo) AttachTx(_ context.Context, _ pgx.Tx, postID, tagID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.links[postID] = append(m.links[postID], tagID)
	return nil
}

func (m *memRepo) DetachAllTx(_ context.Context, _ pgx.Tx, postID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.links, postID)
	return nil
}

func (m *memRepo) ListForPost(_ context.Context, postID uuid.UUID) ([]Tag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Tag
	for _, id := range m.links[postID] {
		if t, ok := m.tags[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

func (m *memRepo) IDsForPost(_ context.Context, postID uuid.UUID) ([]uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]uuid.UUID(nil), m.links[postID]...), nil
}

func (m *memRepo) ListPublishedPostIDsInTag(_ context.Context, _ uuid.UUID, _, _ int) ([]uuid.UUID, error) {
	return nil, nil
}

func (m *memRepo) CountPublishedPostsInTag(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}

// --- per-locale translation overlay (M7b-3) fakes ---------------------------

func (m *memRepo) UpsertTranslationTx(_ context.Context, _ pgx.Tx, tagID uuid.UUID, t Translation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.translations[tagID] == nil {
		m.translations[tagID] = map[string]Translation{}
	}
	m.translations[tagID][t.Locale] = t
	return nil
}

func (m *memRepo) GetTranslation(_ context.Context, tagID uuid.UUID, locale string) (Translation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.translations[tagID][locale]; ok {
		return t, nil
	}
	return Translation{}, ErrNotFound
}

func (m *memRepo) ListTranslations(_ context.Context, tagID uuid.UUID) ([]Translation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Translation, 0, len(m.translations[tagID]))
	for _, t := range m.translations[tagID] {
		out = append(out, t)
	}
	return out, nil
}

func (m *memRepo) TranslatedLocales(_ context.Context, tagID uuid.UUID) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.translations[tagID]))
	for loc := range m.translations[tagID] {
		out = append(out, loc)
	}
	return out, nil
}

func (m *memRepo) DeleteTranslationTx(_ context.Context, _ pgx.Tx, tagID uuid.UUID, locale string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.translations[tagID], locale)
	return nil
}

func (m *memRepo) GetInLocaleByID(_ context.Context, id uuid.UUID, locale string) (Tag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tg, ok := m.tags[id]
	if !ok {
		return Tag{}, ErrNotFound
	}
	return overlayTag(tg, m.translations[id][locale]), nil
}

func (m *memRepo) GetPublishedInLocaleBySlug(_ context.Context, slug, locale string) (Tag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, tg := range m.tags {
		if tg.Slug == slug {
			return overlayTag(tg, m.translations[tg.ID][locale]), nil
		}
	}
	return Tag{}, ErrNotFound
}

// overlayTag applies a translation's non-empty name onto a base tag.
func overlayTag(base Tag, t Translation) Tag {
	if t.Name != "" {
		base.Name = t.Name
	}
	return base
}

type allowAuthorizer struct{}

func (allowAuthorizer) Can(context.Context, uuid.UUID, string, string) bool { return true }

type denyAuthorizer struct{}

func (denyAuthorizer) Can(context.Context, uuid.UUID, string, string) bool { return false }

func newSvc(repo Repository, authz Authorizer) *Service {
	return NewService(fakeBeginner{}, repo, authz)
}

func TestCreate_DerivesAndDedupesSlug(t *testing.T) {
	repo := newMemRepo()
	svc := newSvc(repo, allowAuthorizer{})
	ctx := context.Background()
	actor := uuid.New()

	a, err := svc.Create(ctx, actor, CreateInput{Name: "Cloud Native"})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	if a.Slug != "cloud-native" {
		t.Fatalf("slug = %q", a.Slug)
	}
	b, _ := svc.Create(ctx, actor, CreateInput{Name: "Cloud Native"})
	if b.Slug != "cloud-native-2" {
		t.Fatalf("dedupe slug = %q, want cloud-native-2", b.Slug)
	}
}

func TestCreate_Forbidden(t *testing.T) {
	svc := newSvc(newMemRepo(), denyAuthorizer{})
	if _, err := svc.Create(context.Background(), uuid.New(), CreateInput{Name: "X"}); err != ErrForbidden {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestCreate_NameRequired(t *testing.T) {
	svc := newSvc(newMemRepo(), allowAuthorizer{})
	if _, err := svc.Create(context.Background(), uuid.New(), CreateInput{Name: ""}); err != ErrNameRequired {
		t.Fatalf("err = %v, want ErrNameRequired", err)
	}
}

func TestAssignTx_ReplacesSet(t *testing.T) {
	repo := newMemRepo()
	svc := newSvc(repo, allowAuthorizer{})
	ctx := context.Background()
	a, _ := svc.Create(ctx, uuid.New(), CreateInput{Name: "A"})
	b, _ := svc.Create(ctx, uuid.New(), CreateInput{Name: "B"})
	post := uuid.New()

	if err := svc.AssignTx(ctx, fakeTx{}, post, []uuid.UUID{a.ID, b.ID}); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if ids, _ := repo.IDsForPost(ctx, post); len(ids) != 2 {
		t.Fatalf("len = %d, want 2", len(ids))
	}
	if err := svc.AssignTx(ctx, fakeTx{}, post, nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if ids, _ := repo.IDsForPost(ctx, post); len(ids) != 0 {
		t.Fatalf("after clear len = %d, want 0", len(ids))
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
		{name: "happy", authz: allowAuthorizer{}, locale: "de", in: TranslationInput{Name: "Etikett"}},
		{name: "default locale rejected", authz: allowAuthorizer{}, locale: "en", in: TranslationInput{Name: "X"}, wantErr: ErrDefaultLocaleTranslation},
		{name: "unsupported rejected", authz: allowAuthorizer{}, locale: "xx", in: TranslationInput{Name: "X"}, wantErr: ErrUnsupportedLocale},
		{name: "empty name rejected", authz: allowAuthorizer{}, locale: "de", in: TranslationInput{Name: "   "}, wantErr: ErrNameRequired},
		{name: "forbidden", authz: denyAuthorizer{}, locale: "de", in: TranslationInput{Name: "X"}, wantErr: ErrForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMemRepo()
			base, _ := newSvc(repo, allowAuthorizer{}).Create(ctx, uuid.New(), CreateInput{Name: "Tag"})
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

func TestGetInLocale(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := newSvc(repo, allowAuthorizer{})
	base, _ := svc.Create(ctx, uuid.New(), CreateInput{Name: "Tag"})
	if err := svc.SaveTranslation(ctx, uuid.New(), base.ID, i18n.LocaleDE, TranslationInput{Name: "Etikett"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Overlay: translated name wins.
	de, err := svc.GetInLocale(ctx, uuid.New(), base.ID, i18n.LocaleDE)
	if err != nil {
		t.Fatalf("get de: %v", err)
	}
	if de.Name != "Etikett" {
		t.Fatalf("de name = %q, want Etikett", de.Name)
	}

	// Missing translation (ru) falls back to base name.
	ru, err := svc.GetInLocale(ctx, uuid.New(), base.ID, i18n.LocaleRU)
	if err != nil {
		t.Fatalf("get ru: %v", err)
	}
	if ru.Name != "Tag" {
		t.Fatalf("ru name = %q, want Tag (base fallback)", ru.Name)
	}

	// Default locale returns the base row.
	en, err := svc.GetInLocale(ctx, uuid.New(), base.ID, i18n.LocaleEN)
	if err != nil {
		t.Fatalf("get en: %v", err)
	}
	if en.Name != "Tag" {
		t.Fatalf("en name = %q, want Tag", en.Name)
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
	base, _ := svc.Create(ctx, uuid.New(), CreateInput{Name: "Tag"})
	_ = svc.SaveTranslation(ctx, uuid.New(), base.ID, i18n.LocaleDE, TranslationInput{Name: "Etikett"})
	_ = svc.SaveTranslation(ctx, uuid.New(), base.ID, i18n.LocaleRU, TranslationInput{Name: "Метка"})

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
	base, _ := svc.Create(ctx, uuid.New(), CreateInput{Name: "Tag"})
	_ = svc.SaveTranslation(ctx, uuid.New(), base.ID, i18n.LocaleDE, TranslationInput{Name: "Etikett"})

	de, err := svc.PublicBySlugLocale(ctx, base.Slug, i18n.LocaleDE)
	if err != nil {
		t.Fatalf("de: %v", err)
	}
	if de.Name != "Etikett" {
		t.Fatalf("de name = %q, want Etikett", de.Name)
	}

	// Default locale resolves to the base row.
	en, err := svc.PublicBySlugLocale(ctx, base.Slug, i18n.LocaleEN)
	if err != nil {
		t.Fatalf("en: %v", err)
	}
	if en.Name != "Tag" {
		t.Fatalf("en name = %q, want Tag", en.Name)
	}
}
