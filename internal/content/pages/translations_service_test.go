package pages

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/i18n"
)

func newTranslationSvc(t *testing.T, actor uuid.UUID) (*Service, *memRepo) {
	t.Helper()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(), allow(actor), nullBus{}, time.Now())
	return svc, repo
}

// TestSaveTranslation_DeReadOverlaysAndFallsBackToEn is the headline M7b-2 case:
// a de translation with only title+body set overlays those on a locale-aware
// read; the base (en) row is unchanged.
func TestSaveTranslation_DeReadOverlaysAndFallsBackToEn(t *testing.T) {
	actor := uuid.New()
	svc, _ := newTranslationSvc(t, actor)
	ctx := context.Background()

	base, err := svc.Create(ctx, actor, CreateInput{Title: "EN Title", Body: "<p>EN body</p>"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Publish(ctx, actor, base.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if err := svc.SaveTranslation(ctx, actor, base.ID, i18n.LocaleDE, TranslationInput{
		Title: "DE Titel", Body: "<p>DE Text</p>",
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
	// Structural fields (slug/status) come from the base row.
	if de.Slug != base.Slug {
		t.Errorf("de slug should be shared base slug, got %q", de.Slug)
	}
}

// TestPagePublicBySlugLocale_EnPathUnchanged asserts the default-locale read is
// the base row and is unaffected by any translation.
func TestPagePublicBySlugLocale_EnPathUnchanged(t *testing.T) {
	actor := uuid.New()
	svc, _ := newTranslationSvc(t, actor)
	ctx := context.Background()

	base, _ := svc.Create(ctx, actor, CreateInput{Title: "EN Only", Body: "<p>en</p>"})
	if _, err := svc.Publish(ctx, actor, base.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := svc.SaveTranslation(ctx, actor, base.ID, i18n.LocaleDE, TranslationInput{Title: "DE"}); err != nil {
		t.Fatalf("save de: %v", err)
	}

	en, err := svc.PublicBySlugLocale(ctx, base.Slug, i18n.LocaleEN)
	if err != nil {
		t.Fatalf("public en: %v", err)
	}
	if en.Title != "EN Only" || en.Body != "<p>en</p>" {
		t.Errorf("en path changed by a de translation: %+v", en)
	}
	old, err := svc.PublicBySlug(ctx, base.Slug)
	if err != nil {
		t.Fatalf("public by slug: %v", err)
	}
	if old.Title != "EN Only" {
		t.Errorf("PublicBySlug drifted: %q", old.Title)
	}
}

// TestPageSaveTranslation_SanitizesBody confirms the translated body is run
// through the same kernel sanitizer as the base body on write.
func TestPageSaveTranslation_SanitizesBody(t *testing.T) {
	actor := uuid.New()
	svc, repo := newTranslationSvc(t, actor)
	ctx := context.Background()

	base, _ := svc.Create(ctx, actor, CreateInput{Title: "T", Body: "<p>x</p>"})
	if err := svc.SaveTranslation(ctx, actor, base.ID, i18n.LocaleRU, TranslationInput{
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

// TestPageSaveTranslation_RejectsDefaultLocale ensures en cannot be written to
// the overlay (its content lives on the base row).
func TestPageSaveTranslation_RejectsDefaultLocale(t *testing.T) {
	actor := uuid.New()
	svc, _ := newTranslationSvc(t, actor)
	ctx := context.Background()

	base, _ := svc.Create(ctx, actor, CreateInput{Title: "T", Body: "<p>x</p>"})
	err := svc.SaveTranslation(ctx, actor, base.ID, i18n.LocaleEN, TranslationInput{Title: "nope"})
	if err != ErrDefaultLocaleTranslation {
		t.Errorf("want ErrDefaultLocaleTranslation, got %v", err)
	}
}

// TestPageTranslatedLocales_ReportsNonDefaultOnly asserts the editor's tab-marker
// helper returns only non-default locales that actually have a row.
func TestPageTranslatedLocales_ReportsNonDefaultOnly(t *testing.T) {
	actor := uuid.New()
	svc, _ := newTranslationSvc(t, actor)
	ctx := context.Background()

	base, _ := svc.Create(ctx, actor, CreateInput{Title: "T", Body: "<p>x</p>"})
	if err := svc.SaveTranslation(ctx, actor, base.ID, i18n.LocaleDE, TranslationInput{Title: "DE"}); err != nil {
		t.Fatalf("save de: %v", err)
	}
	locs, err := svc.TranslatedLocales(ctx, actor, base.ID)
	if err != nil {
		t.Fatalf("translated locales: %v", err)
	}
	if len(locs) != 1 || locs[0] != i18n.LocaleDE {
		t.Errorf("translated locales = %v, want [de]", locs)
	}
}

// TestPageGetInLocale_OverlaysForEditor asserts the editor read overlays de.
func TestPageGetInLocale_OverlaysForEditor(t *testing.T) {
	actor := uuid.New()
	svc, _ := newTranslationSvc(t, actor)
	ctx := context.Background()

	base, _ := svc.Create(ctx, actor, CreateInput{Title: "EN", Body: "<p>en</p>"})
	if err := svc.SaveTranslation(ctx, actor, base.ID, i18n.LocaleDE, TranslationInput{Title: "DE Titel", Body: "<p>de</p>"}); err != nil {
		t.Fatalf("save de: %v", err)
	}
	de, err := svc.GetInLocale(ctx, actor, base.ID, i18n.LocaleDE)
	if err != nil {
		t.Fatalf("get in locale: %v", err)
	}
	if de.Title != "DE Titel" || de.Body != "<p>de</p>" {
		t.Errorf("editor de overlay wrong: %+v", de)
	}
	// en tab resolves to the base row.
	en, err := svc.GetInLocale(ctx, actor, base.ID, i18n.LocaleEN)
	if err != nil {
		t.Fatalf("get en: %v", err)
	}
	if en.Title != "EN" {
		t.Errorf("en editor read drifted: %q", en.Title)
	}
}
