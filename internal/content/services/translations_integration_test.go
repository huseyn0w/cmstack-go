package services_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/agentic-cms-go/internal/content/services"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/db"
)

// TestIntegration_ServiceTranslation_UpsertGetOverlayFallback exercises the
// M7b-2 overlay end-to-end on a real DB: a de translation with only title+body
// set overlays those while summary FALLS BACK to the base (en) row. It also
// asserts get-one, all-locales fetch, and in-place upsert.
func TestIntegration_ServiceTranslation_UpsertGetOverlayFallback(t *testing.T) {
	w, actor := newServicesWiring(t, time.Now)
	ctx := context.Background()

	base, err := w.mgr.Create(ctx, actor, services.CreateInput{
		Title: "English Title", Summary: "English summary", Body: "<p>English body</p>", Price: "$499",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Upsert a de translation that intentionally leaves summary empty.
	if err := db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		return w.repo.UpsertTranslationTx(ctx, tx, base.ID, services.Translation{
			Locale: "de", Title: "Deutscher Titel", Summary: "", Body: "<p>Deutscher Text</p>",
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
	if got.Summary != "English summary" {
		t.Errorf("summary = %q, want base (en) fallback", got.Summary)
	}
	// Structural/citable fields come from the base row unchanged.
	if got.Slug != base.Slug || got.Price != "$499" {
		t.Errorf("structural fields drifted: slug=%q price=%q", got.Slug, got.Price)
	}

	// The en overlay resolves to the base row (LEFT JOIN miss -> base).
	en, err := w.repo.GetActiveInLocaleByID(ctx, base.ID, "en")
	if err != nil {
		t.Fatalf("get in en: %v", err)
	}
	if en.Title != "English Title" || en.Summary != "English summary" {
		t.Errorf("en overlay leaked a translation: %+v", en)
	}

	tr, err := w.repo.GetTranslation(ctx, base.ID, "de")
	if err != nil {
		t.Fatalf("get translation: %v", err)
	}
	if tr.Title != "Deutscher Titel" {
		t.Errorf("stored de title = %q", tr.Title)
	}

	// Re-upsert same locale updates in place.
	if err := db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		return w.repo.UpsertTranslationTx(ctx, tx, base.ID, services.Translation{Locale: "de", Title: "Neuer Titel", Body: "<p>Neu</p>"})
	}); err != nil {
		t.Fatalf("re-upsert de: %v", err)
	}
	// Add ru, assert both come back.
	if err := db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		return w.repo.UpsertTranslationTx(ctx, tx, base.ID, services.Translation{Locale: "ru", Title: "Русский"})
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

// TestIntegration_ServiceTranslation_CascadeOnDelete confirms deleting a service
// removes its translation rows via ON DELETE CASCADE.
func TestIntegration_ServiceTranslation_CascadeOnDelete(t *testing.T) {
	w, actor := newServicesWiring(t, time.Now)
	ctx := context.Background()

	base, err := w.mgr.Create(ctx, actor, services.CreateInput{Title: "Cascade Me", Body: "<p>b</p>"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		return w.repo.UpsertTranslationTx(ctx, tx, base.ID, services.Translation{Locale: "de", Title: "Los"})
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		return w.repo.PermanentDeleteTx(ctx, tx, base.ID)
	}); err != nil {
		t.Fatalf("permanent delete: %v", err)
	}
	if _, err := w.repo.GetTranslation(ctx, base.ID, "de"); err != services.ErrNotFound {
		t.Errorf("translation survived service delete, got err=%v", err)
	}
}

// TestIntegration_ServiceTranslation_PublishedInLocaleBySlug asserts the public
// locale-aware slug read overlays the de translation with base fallback, scoped
// to published, non-trashed services.
func TestIntegration_ServiceTranslation_PublishedInLocaleBySlug(t *testing.T) {
	w, actor := newServicesWiring(t, time.Now)
	ctx := context.Background()

	base, err := w.mgr.Create(ctx, actor, services.CreateInput{
		Title: "Public EN", Summary: "EN summary", Body: "<p>EN</p>",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := w.mgr.Publish(ctx, actor, base.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := w.mgr.SaveTranslation(ctx, actor, base.ID, "de", services.TranslationInput{
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
	if de.Summary != "EN summary" {
		t.Errorf("summary should fall back to en, got %q", de.Summary)
	}
}
