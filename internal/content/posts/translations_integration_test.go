package posts_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/posts"
	"github.com/huseyn0w/cmstack-go/internal/platform/db"
)

// TestIntegration_Translation_UpsertGetOverlayFallback exercises the M7b-1
// overlay end-to-end on a real DB: a de translation with only title+body set
// overlays those fields while the excerpt FALLS BACK to the base (en) row. It
// also asserts get-one and all-locales fetch.
func TestIntegration_Translation_UpsertGetOverlayFallback(t *testing.T) {
	w := newPostsWiring(t, time.Now)
	ctx := context.Background()
	author := w.createUser(t, "trauthor@test.local", accounts.RoleAuthor)

	base, err := w.svc.Create(ctx, author, posts.CreateInput{
		Title: "English Title", Excerpt: "English excerpt", Body: "<p>English body</p>",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Upsert a de translation that intentionally leaves excerpt empty.
	err = db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		return w.repo.UpsertTranslationTx(ctx, tx, base.ID, posts.Translation{
			Locale: "de", Title: "Deutscher Titel", Excerpt: "", Body: "<p>Deutscher Text</p>",
		})
	})
	if err != nil {
		t.Fatalf("upsert de: %v", err)
	}

	// Overlay read: title+body are German, excerpt falls back to English.
	got, err := w.repo.GetActiveInLocaleByID(ctx, base.ID, "de")
	if err != nil {
		t.Fatalf("get in de: %v", err)
	}
	if got.Title != "Deutscher Titel" {
		t.Errorf("title = %q, want German", got.Title)
	}
	if got.Body != "<p>Deutscher Text</p>" {
		t.Errorf("body = %q, want German", got.Body)
	}
	if got.Excerpt != "English excerpt" {
		t.Errorf("excerpt = %q, want base (en) fallback", got.Excerpt)
	}
	// Structural fields come from the base row unchanged.
	if got.Slug != base.Slug || got.AuthorID != base.AuthorID {
		t.Errorf("structural fields drifted: slug=%q author=%v", got.Slug, got.AuthorID)
	}

	// An unknown locale resolves to the base row (LEFT JOIN misses -> base).
	en, err := w.repo.GetActiveInLocaleByID(ctx, base.ID, "en")
	if err != nil {
		t.Fatalf("get in en: %v", err)
	}
	if en.Title != "English Title" || en.Excerpt != "English excerpt" {
		t.Errorf("en overlay leaked a translation: %+v", en)
	}

	// GetTranslation returns the stored row.
	tr, err := w.repo.GetTranslation(ctx, base.ID, "de")
	if err != nil {
		t.Fatalf("get translation: %v", err)
	}
	if tr.Title != "Deutscher Titel" {
		t.Errorf("stored de title = %q", tr.Title)
	}

	// Upsert again (same locale) updates in place, not a second row.
	err = db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		return w.repo.UpsertTranslationTx(ctx, tx, base.ID, posts.Translation{
			Locale: "de", Title: "Neuer Titel", Body: "<p>Neu</p>",
		})
	})
	if err != nil {
		t.Fatalf("re-upsert de: %v", err)
	}

	// Add a ru translation, then assert both come back.
	err = db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		return w.repo.UpsertTranslationTx(ctx, tx, base.ID, posts.Translation{
			Locale: "ru", Title: "Русский заголовок",
		})
	})
	if err != nil {
		t.Fatalf("upsert ru: %v", err)
	}
	all, err := w.repo.ListTranslations(ctx, base.ID)
	if err != nil {
		t.Fatalf("list translations: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 translations (de,ru), got %d: %+v", len(all), all)
	}
	locales, err := w.repo.TranslatedLocales(ctx, base.ID)
	if err != nil {
		t.Fatalf("translated locales: %v", err)
	}
	if len(locales) != 2 {
		t.Errorf("translated locales = %v, want [de ru]", locales)
	}
}

// TestIntegration_Translation_CascadeOnPostDelete confirms deleting a post
// removes its translation rows via ON DELETE CASCADE.
func TestIntegration_Translation_CascadeOnPostDelete(t *testing.T) {
	w := newPostsWiring(t, time.Now)
	ctx := context.Background()
	author := w.createUser(t, "casauthor@test.local", accounts.RoleAuthor)

	base, err := w.svc.Create(ctx, author, posts.CreateInput{Title: "Cascade Me", Body: "<p>b</p>"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		return w.repo.UpsertTranslationTx(ctx, tx, base.ID, posts.Translation{Locale: "de", Title: "Los"})
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Hard-delete the post (permanent delete cascades to post_translations).
	if err := db.RunInTx(ctx, w.pool, func(ctx context.Context, tx pgx.Tx) error {
		return w.repo.PermanentDeleteTx(ctx, tx, base.ID)
	}); err != nil {
		t.Fatalf("permanent delete: %v", err)
	}

	if _, err := w.repo.GetTranslation(ctx, base.ID, "de"); err != posts.ErrNotFound {
		t.Errorf("translation survived post delete, got err=%v", err)
	}
}

// TestIntegration_Translation_PublishedInLocaleBySlug asserts the public
// locale-aware slug read overlays the de translation with base fallback and is
// scoped to published, non-trashed posts.
func TestIntegration_Translation_PublishedInLocaleBySlug(t *testing.T) {
	w := newPostsWiring(t, time.Now)
	ctx := context.Background()
	author := w.createUser(t, "pubauthor@test.local", accounts.RoleAuthor)

	base, err := w.svc.Create(ctx, author, posts.CreateInput{
		Title: "Public EN", Excerpt: "EN excerpt", Body: "<p>EN</p>",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := w.svc.Publish(ctx, author, base.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := w.svc.SaveTranslation(ctx, author, base.ID, "de", posts.TranslationInput{
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
	if de.Excerpt != "EN excerpt" {
		t.Errorf("excerpt should fall back to en, got %q", de.Excerpt)
	}
}
