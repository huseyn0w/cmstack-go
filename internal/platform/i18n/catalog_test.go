package i18n

import "testing"

func newCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := LoadCatalog()
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	return c
}

func TestCatalogLookup(t *testing.T) {
	c := newCatalog(t)
	if got := c.Translate(LocaleEN, "nav.blog"); got != "Blog" {
		t.Errorf("en nav.blog = %q, want Blog", got)
	}
	if got := c.Translate(LocaleDE, "nav.services"); got != "Leistungen" {
		t.Errorf("de nav.services = %q, want Leistungen", got)
	}
	if got := c.Translate(LocaleRU, "nav.home"); got != "Главная" {
		t.Errorf("ru nav.home = %q, want Главная", got)
	}
}

func TestCatalogRUHasNativeValues(t *testing.T) {
	c := newCatalog(t)
	// ru.json now carries native translations for these keys (catalog parity).
	if got := c.Translate(LocaleRU, "post.comments"); got != "Комментарии" {
		t.Errorf("ru post.comments = %q, want Комментарии", got)
	}
	if got := c.Translate(LocaleRU, "pagination.next"); got != "Вперёд" {
		t.Errorf("ru pagination.next = %q, want Вперёд", got)
	}
}

func TestCatalogFallbackToEN(t *testing.T) {
	// A key present only in the default (en) table must fall back to en when
	// requested for a non-default locale. We construct a catalog with a de table
	// that omits the key to exercise the fallback path directly.
	c := &Catalog{tables: map[Locale]map[string]string{
		LocaleEN: {"only.en": "English only"},
		LocaleDE: {},
	}}
	if got := c.Translate(LocaleDE, "only.en"); got != "English only" {
		t.Errorf("de only.en = %q, want fallback to en 'English only'", got)
	}
}

func TestCatalogMissingKeyReturnsKey(t *testing.T) {
	c := newCatalog(t)
	// Absent everywhere -> the key itself is returned so the UI is never blank.
	if got := c.Translate(LocaleEN, "does.not.exist"); got != "does.not.exist" {
		t.Errorf("missing key = %q, want the key echoed back", got)
	}
	if _, ok := c.lookup(LocaleDE, "does.not.exist"); ok {
		t.Error("lookup ok should be false for a missing key")
	}
}

func TestCatalogInterpolation(t *testing.T) {
	c := newCatalog(t)
	got := c.Translate(LocaleEN, "search.noResults", "query", "gopher")
	want := "No results for “gopher”."
	if got != want {
		t.Errorf("interpolation = %q, want %q", got, want)
	}
	// Interpolation must also work on a fallback-resolved message.
	deGot := c.Translate(LocaleDE, "search.noResults", "query", "x")
	if deGot != "Keine Ergebnisse für „x“." {
		t.Errorf("de interpolation = %q", deGot)
	}
	// Non-string placeholder value stringifies.
	if got := c.Translate(LocaleEN, "search.noResults", "query", 42); got != "No results for “42”." {
		t.Errorf("int interpolation = %q", got)
	}
}

func TestTranslatorBindsLocale(t *testing.T) {
	c := newCatalog(t)
	de := NewTranslator(c, LocaleDE)
	if de.Locale() != LocaleDE {
		t.Fatalf("Locale() = %q", de.Locale())
	}
	if got := de.T("nav.search"); got != "Suche" {
		t.Errorf("de T(nav.search) = %q, want Suche", got)
	}
	// Unsupported locale collapses to default.
	if NewTranslator(c, Locale("fr")).Locale() != Default() {
		t.Error("unsupported locale should collapse to default")
	}
}

func TestTranslatorNilCatalogEchoesKey(t *testing.T) {
	tr := NewTranslator(nil, LocaleEN)
	if got := tr.T("nav.blog"); got != "nav.blog" {
		t.Errorf("nil catalog T = %q, want key echoed", got)
	}
	if got := tr.T("hi {name}", "name", "x"); got != "hi x" {
		t.Errorf("nil catalog interpolation = %q, want 'hi x'", got)
	}
}
