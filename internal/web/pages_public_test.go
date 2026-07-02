package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/pages"
	"github.com/huseyn0w/cmstack-go/internal/health"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
	"github.com/huseyn0w/cmstack-go/internal/platform/session"
)

type stubPagePublic struct {
	page      pages.Page
	pageErr   error
	ancestors []pages.Page
	// byLocale returns per-locale pages from PublicBySlugLocale so a handler test
	// can assert the active locale reached the read (M7b-2); absent locales fall
	// back to page (the base-fallback contract).
	byLocale map[i18n.Locale]pages.Page
}

func (s stubPagePublic) PublicBySlug(context.Context, string) (pages.Page, error) {
	return s.page, s.pageErr
}

func (s stubPagePublic) PublicBySlugLocale(_ context.Context, _ string, loc i18n.Locale) (pages.Page, error) {
	if s.pageErr != nil {
		return pages.Page{}, s.pageErr
	}
	if p, ok := s.byLocale[loc]; ok {
		return p, nil
	}
	return s.page, nil // base (en) fallback
}

func (s stubPagePublic) Ancestors(context.Context, pages.Page) ([]pages.Page, error) {
	return s.ancestors, nil
}

// buildPagesPublicEnv wires a full router so /de/p/{slug} resolves the locale.
func buildPagesPublicEnv(t *testing.T, svc PagePublicService) http.Handler {
	t.Helper()
	sess := session.NewManager(false)
	cat, _ := i18n.LoadCatalog()
	return Router(Deps{
		Config:        config.Config{AppEnv: "test", BaseURL: "https://site.test"},
		Health:        health.NewHandler(health.NewService(nil)),
		Session:       sess,
		AuthMW:        NewAuthMiddleware(sess, fakeUsers{users: map[uuid.UUID]accounts.User{}}, allowAllAuthz{}),
		CSRFFunc:      security.Token,
		PagePublicSvc: svc,
		SiteName:      "CMStack",
		Locale:        NewLocaleResolver(cat),
	})
}

func TestPagePublic_ShowRendersWithBreadcrumbs(t *testing.T) {
	root := pages.Page{ID: uuid.New(), Title: "About", Slug: "about", Status: kernel.StatusPublished}
	page := pages.Page{
		ID: uuid.New(), Title: "Team", Slug: "team", Body: "<p>Our team.</p>",
		Template: "default", Status: kernel.StatusPublished, ParentID: &root.ID,
	}
	h := NewPagePublicHandler(stubPagePublic{page: page, ancestors: []pages.Page{root}}, "CMStack", "https://site.test")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("slug", "team")
	req := httptest.NewRequest(http.MethodGet, "/p/team", nil).
		WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.Show(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("show = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`data-testid="page-article"`, "Team", "<p>Our team.</p>", "About", "/p/about"} {
		if !strings.Contains(body, want) {
			t.Errorf("page detail missing %q", want)
		}
	}
}

// TestPagePublic_DeDetailRendersTranslation asserts /de/p/{slug} renders the
// de-overlaid content (M7b-2): the active locale reaches PublicBySlugLocale.
func TestPagePublic_DeDetailRendersTranslation(t *testing.T) {
	en := pages.Page{ID: uuid.New(), Title: "About", Slug: "about", Body: "<p>English</p>", Template: "default", Status: kernel.StatusPublished}
	de := en
	de.Title = "Ueber uns"
	de.Body = "<p>Deutscher Text</p>"
	svc := stubPagePublic{page: en, byLocale: map[i18n.Locale]pages.Page{i18n.LocaleDE: de}}
	r := buildPagesPublicEnv(t, svc)
	req := httptest.NewRequest(http.MethodGet, "/de/p/about", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/de/p/about = %d\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Ueber uns") || !strings.Contains(body, "<p>Deutscher Text</p>") {
		t.Errorf("de page did not render de translation:\n%s", body)
	}
}

// TestPagePublic_DeDetailFallsBackToEn asserts a de request for a page WITHOUT a
// de translation falls back to the base (en) content.
func TestPagePublic_DeDetailFallsBackToEn(t *testing.T) {
	en := pages.Page{ID: uuid.New(), Title: "OnlyEnglish", Slug: "about", Body: "<p>English</p>", Template: "default", Status: kernel.StatusPublished}
	svc := stubPagePublic{page: en} // no byLocale entry -> fallback
	r := buildPagesPublicEnv(t, svc)
	req := httptest.NewRequest(http.MethodGet, "/de/p/about", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/de/p/about = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "OnlyEnglish") {
		t.Error("de request without a translation did not fall back to en content")
	}
}

func TestPagePublic_ShowNotFound(t *testing.T) {
	h := NewPagePublicHandler(stubPagePublic{pageErr: pages.ErrNotFound}, "CMStack", "https://site.test")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("slug", "missing")
	req := httptest.NewRequest(http.MethodGet, "/p/missing", nil).
		WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.Show(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing page = %d, want 404", rec.Code)
	}
}
