package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// renderHomeIn renders the public Home page through a locale-populated context,
// mirroring what the middleware installs, and returns the HTML.
func renderHomeIn(t *testing.T, loc i18n.Locale, rest, rawQuery string) string {
	t.Helper()
	cat, err := i18n.LoadCatalog()
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	ctx := withLocale(context.Background(), localeState{
		locale:     loc,
		rest:       rest,
		rawQuery:   rawQuery,
		translator: i18n.NewTranslator(cat, loc),
	})
	html, err := render.ToString(ctx, webtempl.Home())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return html
}

func TestSwitcherRendersThreeLocalizedLinks(t *testing.T) {
	html := renderHomeIn(t, i18n.LocaleEN, "/blog", "tag=x")

	// The switcher container is present with its testid.
	if !strings.Contains(html, `data-testid="locale-switcher"`) {
		t.Error("missing locale-switcher container")
	}
	// One link per locale, each to the CURRENT page (path + query) in it.
	for _, want := range []string{
		`href="/blog?tag=x"`,    // en (default, unprefixed)
		`href="/de/blog?tag=x"`, // de prefixed
		`href="/ru/blog?tag=x"`, // ru prefixed
		`data-testid="locale-option-en"`,
		`data-testid="locale-option-de"`,
		`data-testid="locale-option-ru"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("switcher missing %q", want)
		}
	}
}

func TestSwitcherMarksActiveLocale(t *testing.T) {
	html := renderHomeIn(t, i18n.LocaleDE, "/services", "")
	// The active (de) option carries aria-current; verify it sits on the de link.
	if !strings.Contains(html, `aria-current="true"`) {
		t.Fatal("no aria-current marker rendered")
	}
	// The active de option and its aria-current must co-occur before the ru link.
	deIdx := strings.Index(html, `data-testid="locale-option-de"`)
	ariaIdx := strings.Index(html, `aria-current="true"`)
	ruIdx := strings.Index(html, `data-testid="locale-option-ru"`)
	if ariaIdx < 0 || deIdx < 0 || ruIdx < 0 || ariaIdx >= ruIdx {
		t.Errorf("aria-current not positioned on active de option (aria=%d de=%d ru=%d)", ariaIdx, deIdx, ruIdx)
	}
}

func TestLayoutSetsHTMLLangFromLocale(t *testing.T) {
	if html := renderHomeIn(t, i18n.LocaleDE, "/", ""); !strings.Contains(html, `lang="de"`) {
		t.Error("de render missing lang=\"de\"")
	}
	if html := renderHomeIn(t, i18n.LocaleRU, "/", ""); !strings.Contains(html, `lang="ru"`) {
		t.Error("ru render missing lang=\"ru\"")
	}
}

func TestHeaderStringsLocalized(t *testing.T) {
	de := renderHomeIn(t, i18n.LocaleDE, "/", "")
	if !strings.Contains(de, "Suche") { // search label/placeholder in de
		t.Error("de header not localized (expected 'Suche')")
	}
	ru := renderHomeIn(t, i18n.LocaleRU, "/", "")
	if !strings.Contains(ru, "Поиск") {
		t.Error("ru header not localized (expected 'Поиск')")
	}
}

// End-to-end through the router: the middleware strips the prefix and the layout
// emits the matching <html lang>.
func TestRouterLocaleEndToEnd(t *testing.T) {
	cat, err := i18n.LoadCatalog()
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	d := Deps{
		Config: config.Config{AppEnv: "test"},
		Locale: NewLocaleResolver(cat),
	}
	r := Router(d)

	cases := []struct {
		url  string
		lang string
	}{
		{"/", `lang="en"`},
		{"/de", `lang="de"`},
		{"/ru", `lang="ru"`},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.url, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", c.url, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), c.lang) {
			t.Errorf("GET %s: body missing %s", c.url, c.lang)
		}
	}
}
