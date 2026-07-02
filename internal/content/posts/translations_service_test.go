package posts

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
)

func newTranslationSvc(t *testing.T, author uuid.UUID) (*Service, *memRepo) {
	t.Helper()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, time.Now())
	return svc, repo
}

// TestSaveTranslation_DeReadOverlaysAndFallsBackToEn is the headline M7b-1 case:
// a de translation with only title+body set overlays those, while the missing
// excerpt falls back to the base (en) content on a locale-aware read.
func TestSaveTranslation_DeReadOverlaysAndFallsBackToEn(t *testing.T) {
	author := uuid.New()
	svc, _ := newTranslationSvc(t, author)
	ctx := context.Background()

	base, err := svc.Create(ctx, author, CreateInput{
		Title: "EN Title", Excerpt: "EN excerpt", Body: "<p>EN body</p>",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Publish(ctx, author, base.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if err := svc.SaveTranslation(ctx, author, base.ID, i18n.LocaleDE, TranslationInput{
		Title: "DE Titel", Body: "<p>DE Text</p>", // excerpt intentionally empty
	}); err != nil {
		t.Fatalf("save de: %v", err)
	}

	de, err := svc.PublicBySlugLocale(ctx, base.Slug, i18n.LocaleDE)
	if err != nil {
		t.Fatalf("public de: %v", err)
	}
	if de.Title != "DE Titel" || de.Body != "<p>DE Text</p>" {
		t.Errorf("de overlay wrong: title=%q body=%q", de.Title, de.Body)
	}
	if de.Excerpt != "EN excerpt" {
		t.Errorf("de excerpt should fall back to en, got %q", de.Excerpt)
	}
}

// TestPublicBySlugLocale_EnPathUnchanged asserts the default-locale read is the
// base row and is unaffected by any translation (the existing en behavior).
func TestPublicBySlugLocale_EnPathUnchanged(t *testing.T) {
	author := uuid.New()
	svc, _ := newTranslationSvc(t, author)
	ctx := context.Background()

	base, _ := svc.Create(ctx, author, CreateInput{Title: "EN Only", Excerpt: "keep", Body: "<p>en</p>"})
	if _, err := svc.Publish(ctx, author, base.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := svc.SaveTranslation(ctx, author, base.ID, i18n.LocaleDE, TranslationInput{Title: "DE"}); err != nil {
		t.Fatalf("save de: %v", err)
	}

	en, err := svc.PublicBySlugLocale(ctx, base.Slug, i18n.LocaleEN)
	if err != nil {
		t.Fatalf("public en: %v", err)
	}
	if en.Title != "EN Only" || en.Excerpt != "keep" || en.Body != "<p>en</p>" {
		t.Errorf("en path changed by a de translation: %+v", en)
	}

	// PublicBySlug (the pre-M7b method) must also still resolve to the base row.
	old, err := svc.PublicBySlug(ctx, base.Slug)
	if err != nil {
		t.Fatalf("public by slug: %v", err)
	}
	if old.Title != "EN Only" {
		t.Errorf("PublicBySlug drifted: %q", old.Title)
	}
}

// TestSaveTranslation_SanitizesBody confirms the translated body is run through
// the same kernel sanitizer as the base body on write.
func TestSaveTranslation_SanitizesBody(t *testing.T) {
	author := uuid.New()
	svc, repo := newTranslationSvc(t, author)
	ctx := context.Background()

	base, _ := svc.Create(ctx, author, CreateInput{Title: "T", Body: "<p>x</p>"})
	if err := svc.SaveTranslation(ctx, author, base.ID, i18n.LocaleRU, TranslationInput{
		Title: "RU", Body: `<p>ok</p><script>alert(1)</script>`,
	}); err != nil {
		t.Fatalf("save ru: %v", err)
	}
	tr, err := repo.GetTranslation(ctx, base.ID, "ru")
	if err != nil {
		t.Fatalf("get ru: %v", err)
	}
	if tr.Body != "<p>ok</p>" {
		t.Errorf("translated body not sanitized: %q", tr.Body)
	}
}

// TestSaveTranslation_RejectsDefaultLocale ensures en cannot be written to the
// overlay (its content lives on the base row).
func TestSaveTranslation_RejectsDefaultLocale(t *testing.T) {
	author := uuid.New()
	svc, _ := newTranslationSvc(t, author)
	ctx := context.Background()

	base, _ := svc.Create(ctx, author, CreateInput{Title: "T", Body: "<p>x</p>"})
	err := svc.SaveTranslation(ctx, author, base.ID, i18n.LocaleEN, TranslationInput{Title: "nope"})
	if err != ErrDefaultLocaleTranslation {
		t.Errorf("want ErrDefaultLocaleTranslation, got %v", err)
	}
}

// TestTranslatedLocales_ReportsNonDefaultOnly asserts the editor's tab-marker
// helper returns only non-default locales that actually have a row.
func TestTranslatedLocales_ReportsNonDefaultOnly(t *testing.T) {
	author := uuid.New()
	svc, _ := newTranslationSvc(t, author)
	ctx := context.Background()

	base, _ := svc.Create(ctx, author, CreateInput{Title: "T", Body: "<p>x</p>"})
	if err := svc.SaveTranslation(ctx, author, base.ID, i18n.LocaleDE, TranslationInput{Title: "DE"}); err != nil {
		t.Fatalf("save de: %v", err)
	}
	locs, err := svc.TranslatedLocales(ctx, author, base.ID)
	if err != nil {
		t.Fatalf("translated locales: %v", err)
	}
	if len(locs) != 1 || locs[0] != i18n.LocaleDE {
		t.Errorf("translated locales = %v, want [de]", locs)
	}
}
