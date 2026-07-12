package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/i18n"
)

func newTranslationMgr(t *testing.T, actor uuid.UUID) (*Manager, *memRepo) {
	t.Helper()
	repo := newMemRepo()
	mgr := newTestManager(repo, newMemRevisions(), allow(actor), nullBus{}, time.Now())
	return mgr, repo
}

// TestServiceSaveTranslation_DeReadOverlaysAndFallsBackToEn is the headline
// M7b-2 case: a de translation with only title+body set overlays those on a
// locale-aware read; the missing summary falls back to the base (en) content.
func TestServiceSaveTranslation_DeReadOverlaysAndFallsBackToEn(t *testing.T) {
	actor := uuid.New()
	mgr, _ := newTranslationMgr(t, actor)
	ctx := context.Background()

	base, err := mgr.Create(ctx, actor, CreateInput{
		Title: "SEO Audit", Summary: "We audit your site.", Body: "<p>EN body</p>", Price: "$499",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := mgr.Publish(ctx, actor, base.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if err := mgr.SaveTranslation(ctx, actor, base.ID, i18n.LocaleDE, TranslationInput{
		Title: "SEO Pruefung", Body: "<p>DE Text</p>", // summary intentionally empty
	}); err != nil {
		t.Fatalf("save de: %v", err)
	}

	de, err := mgr.PublicBySlugLocale(ctx, base.Slug, i18n.LocaleDE)
	if err != nil {
		t.Fatalf("public de: %v", err)
	}
	if de.Title != "SEO Pruefung" || de.Body != "<p>DE Text</p>" {
		t.Errorf("de overlay wrong: title=%q body=%q", de.Title, de.Body)
	}
	if de.Summary != "We audit your site." {
		t.Errorf("de summary should fall back to en, got %q", de.Summary)
	}
	// Citable fields (price) come from the base row.
	if de.Price != "$499" {
		t.Errorf("de price should be shared base price, got %q", de.Price)
	}
}

// TestServicePublicBySlugLocale_EnPathUnchanged asserts the default-locale read
// is the base row and is unaffected by any translation.
func TestServicePublicBySlugLocale_EnPathUnchanged(t *testing.T) {
	actor := uuid.New()
	mgr, _ := newTranslationMgr(t, actor)
	ctx := context.Background()

	base, _ := mgr.Create(ctx, actor, CreateInput{Title: "EN Only", Summary: "keep", Body: "<p>en</p>"})
	if _, err := mgr.Publish(ctx, actor, base.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := mgr.SaveTranslation(ctx, actor, base.ID, i18n.LocaleDE, TranslationInput{Title: "DE"}); err != nil {
		t.Fatalf("save de: %v", err)
	}

	en, err := mgr.PublicBySlugLocale(ctx, base.Slug, i18n.LocaleEN)
	if err != nil {
		t.Fatalf("public en: %v", err)
	}
	if en.Title != "EN Only" || en.Summary != "keep" || en.Body != "<p>en</p>" {
		t.Errorf("en path changed by a de translation: %+v", en)
	}
	old, err := mgr.PublicBySlug(ctx, base.Slug)
	if err != nil {
		t.Fatalf("public by slug: %v", err)
	}
	if old.Title != "EN Only" {
		t.Errorf("PublicBySlug drifted: %q", old.Title)
	}
}

// TestServiceSaveTranslation_SanitizesSummaryAndBody confirms the translated
// summary is stripped to plain text and the body sanitized on write.
func TestServiceSaveTranslation_SanitizesSummaryAndBody(t *testing.T) {
	actor := uuid.New()
	mgr, repo := newTranslationMgr(t, actor)
	ctx := context.Background()

	base, _ := mgr.Create(ctx, actor, CreateInput{Title: "T", Body: "<p>x</p>"})
	if err := mgr.SaveTranslation(ctx, actor, base.ID, i18n.LocaleRU, TranslationInput{
		Title:   "RU",
		Summary: `Plain <script>alert(1)</script>text`,
		Body:    `<p>ok</p><script>alert(1)</script>`,
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
	if tr.Summary != "Plain text" {
		t.Errorf("translated summary not stripped to plain text: %q", tr.Summary)
	}
}

// TestServiceSaveTranslation_RejectsDefaultLocale ensures en cannot be written
// to the overlay.
func TestServiceSaveTranslation_RejectsDefaultLocale(t *testing.T) {
	actor := uuid.New()
	mgr, _ := newTranslationMgr(t, actor)
	ctx := context.Background()

	base, _ := mgr.Create(ctx, actor, CreateInput{Title: "T", Body: "<p>x</p>"})
	err := mgr.SaveTranslation(ctx, actor, base.ID, i18n.LocaleEN, TranslationInput{Title: "nope"})
	if err != ErrDefaultLocaleTranslation {
		t.Errorf("want ErrDefaultLocaleTranslation, got %v", err)
	}
}

// TestServiceTranslatedLocales_ReportsNonDefaultOnly asserts the editor's
// tab-marker helper returns only non-default locales that actually have a row.
func TestServiceTranslatedLocales_ReportsNonDefaultOnly(t *testing.T) {
	actor := uuid.New()
	mgr, _ := newTranslationMgr(t, actor)
	ctx := context.Background()

	base, _ := mgr.Create(ctx, actor, CreateInput{Title: "T", Body: "<p>x</p>"})
	if err := mgr.SaveTranslation(ctx, actor, base.ID, i18n.LocaleDE, TranslationInput{Title: "DE"}); err != nil {
		t.Fatalf("save de: %v", err)
	}
	locs, err := mgr.TranslatedLocales(ctx, actor, base.ID)
	if err != nil {
		t.Fatalf("translated locales: %v", err)
	}
	if len(locs) != 1 || locs[0] != i18n.LocaleDE {
		t.Errorf("translated locales = %v, want [de]", locs)
	}
}

// TestServiceGetInLocale_OverlaysForEditor asserts the editor read overlays de
// and carries FAQs.
func TestServiceGetInLocale_OverlaysForEditor(t *testing.T) {
	actor := uuid.New()
	mgr, _ := newTranslationMgr(t, actor)
	ctx := context.Background()

	base, _ := mgr.Create(ctx, actor, CreateInput{
		Title: "EN", Body: "<p>en</p>", FAQs: []FAQInput{{Question: "Q", Answer: "A"}},
	})
	if err := mgr.SaveTranslation(ctx, actor, base.ID, i18n.LocaleDE, TranslationInput{Title: "DE Titel", Body: "<p>de</p>"}); err != nil {
		t.Fatalf("save de: %v", err)
	}
	de, err := mgr.GetInLocale(ctx, actor, base.ID, i18n.LocaleDE)
	if err != nil {
		t.Fatalf("get in locale: %v", err)
	}
	if de.Title != "DE Titel" || de.Body != "<p>de</p>" {
		t.Errorf("editor de overlay wrong: %+v", de)
	}
	if len(de.FAQs) != 1 {
		t.Errorf("editor de read should carry base FAQs, got %d", len(de.FAQs))
	}
	en, err := mgr.GetInLocale(ctx, actor, base.ID, i18n.LocaleEN)
	if err != nil {
		t.Fatalf("get en: %v", err)
	}
	if en.Title != "EN" {
		t.Errorf("en editor read drifted: %q", en.Title)
	}
}
