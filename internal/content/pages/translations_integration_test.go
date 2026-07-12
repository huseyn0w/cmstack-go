package pages_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/agentic-cms-go/internal/content/pages"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/db"
)

// TestIntegration_PageTranslation_UpsertGetOverlayFallback exercises the M7b-2
// overlay end-to-end on a real DB: a de translation with title+body set overlays
// those fields on a locale-aware read; the base (en) row is unchanged. It also
// asserts get-one, all-locales fetch, and in-place upsert.
func TestIntegration_PageTranslation_UpsertGetOverlayFallback(t *testing.T) {
	w, actor := newPagesWiring(t, time.Now)
	ctx := context.Background()

	base, err := w.svc.Create(ctx, actor, pages.CreateInput{Title: "English Title", Body: "<p>English body</p>"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		return w.repo.UpsertTranslationTx(ctx, tx, base.ID, pages.Translation{
			Locale: "de", Title: "Deutscher Titel", Body: "<p>Deutscher Text</p>",
		})
	}); err != nil {
		t.Fatalf("upsert de: %v", err)
	}

	got, err := w.repo.GetActiveInLocaleByID(ctx, base.ID, "de")
	if err != nil {
		t.Fatalf("get in de: %v", err)
	}
	if got.Title != "Deutscher Titel" || got.Body != "<p>Deutscher Text</p>" {
		t.Errorf("de overlay wrong: title=%q body=%q", got.Title, got.Body)
	}
	// Structural fields come from the base row unchanged.
	if got.Slug != base.Slug || got.Template != base.Template {
		t.Errorf("structural fields drifted: slug=%q template=%q", got.Slug, got.Template)
	}

	en, err := w.repo.GetActiveInLocaleByID(ctx, base.ID, "en")
	if err != nil {
		t.Fatalf("get in en: %v", err)
	}
	if en.Title != "English Title" || en.Body != "<p>English body</p>" {
		t.Errorf("en overlay leaked a translation: %+v", en)
	}

	// Re-upsert same locale updates in place; add ru; assert both come back.
	if err := db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		return w.repo.UpsertTranslationTx(ctx, tx, base.ID, pages.Translation{Locale: "de", Title: "Neuer Titel"})
	}); err != nil {
		t.Fatalf("re-upsert de: %v", err)
	}
	if err := db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		return w.repo.UpsertTranslationTx(ctx, tx, base.ID, pages.Translation{Locale: "ru", Title: "Русский"})
	}); err != nil {
		t.Fatalf("upsert ru: %v", err)
	}
	all, err := w.repo.ListTranslations(ctx, base.ID)
	if err != nil {
		t.Fatalf("list translations: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 translations (de,ru), got %d", len(all))
	}
	locales, err := w.repo.TranslatedLocales(ctx, base.ID)
	if err != nil {
		t.Fatalf("translated locales: %v", err)
	}
	if len(locales) != 2 {
		t.Errorf("translated locales = %v, want [de ru]", locales)
	}
}

// TestIntegration_PageTranslation_CascadeOnDelete confirms deleting a page
// removes its translation rows via ON DELETE CASCADE.
func TestIntegration_PageTranslation_CascadeOnDelete(t *testing.T) {
	w, actor := newPagesWiring(t, time.Now)
	ctx := context.Background()

	base, err := w.svc.Create(ctx, actor, pages.CreateInput{Title: "Cascade Me", Body: "<p>b</p>"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		return w.repo.UpsertTranslationTx(ctx, tx, base.ID, pages.Translation{Locale: "de", Title: "Los"})
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		return w.repo.PermanentDeleteTx(ctx, tx, base.ID)
	}); err != nil {
		t.Fatalf("permanent delete: %v", err)
	}
	if _, err := w.repo.GetTranslation(ctx, base.ID, "de"); err != pages.ErrNotFound {
		t.Errorf("translation survived page delete, got err=%v", err)
	}
}

// TestIntegration_PageTranslation_PublishedInLocaleBySlug asserts the public
// locale-aware slug read overlays the de translation with base fallback, scoped
// to published, non-trashed pages.
func TestIntegration_PageTranslation_PublishedInLocaleBySlug(t *testing.T) {
	w, actor := newPagesWiring(t, time.Now)
	ctx := context.Background()

	base, err := w.svc.Create(ctx, actor, pages.CreateInput{Title: "Public EN", Body: "<p>EN</p>"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := w.svc.Publish(ctx, actor, base.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := w.svc.SaveTranslation(ctx, actor, base.ID, "de", pages.TranslationInput{
		Title: "Öffentlich DE", Body: "<p>DE</p>",
	}); err != nil {
		t.Fatalf("save de translation: %v", err)
	}

	de, err := w.repo.GetPublishedInLocaleBySlug(ctx, base.Slug, "de")
	if err != nil {
		t.Fatalf("published by slug de: %v", err)
	}
	if de.Title != "Öffentlich DE" || de.Body != "<p>DE</p>" {
		t.Errorf("de overlay wrong: %+v", de)
	}
}
