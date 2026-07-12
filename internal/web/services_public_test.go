package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
	"github.com/huseyn0w/agentic-cms-go/internal/content/services"
	"github.com/huseyn0w/agentic-cms-go/internal/health"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/config"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/i18n"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/security"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/session"
)

type stubServicePublic struct {
	svc    services.Service
	svcErr error
	list   []services.Service
	total  int
	// byLocale returns per-locale services from PublicBySlugLocale so a handler
	// test can assert the active locale reached the read (M7b-2); absent locales
	// fall back to svc.
	byLocale map[i18n.Locale]services.Service
}

func (s stubServicePublic) PublicBySlug(context.Context, string) (services.Service, error) {
	return s.svc, s.svcErr
}

func (s stubServicePublic) PublicBySlugLocale(_ context.Context, _ string, loc i18n.Locale) (services.Service, error) {
	if s.svcErr != nil {
		return services.Service{}, s.svcErr
	}
	if v, ok := s.byLocale[loc]; ok {
		return v, nil
	}
	return s.svc, nil // base (en) fallback
}

func (s stubServicePublic) PublicList(context.Context, int, int) ([]services.Service, int, error) {
	return s.list, s.total, nil
}

// buildServicesPublicEnv wires a full router so /de/services/{slug} resolves.
func buildServicesPublicEnv(t *testing.T, svc ServicePublicService) http.Handler {
	t.Helper()
	sess := session.NewManager(false)
	cat, _ := i18n.LoadCatalog()
	return Router(Deps{
		Config:           config.Config{AppEnv: "test", BaseURL: "https://site.test"},
		Health:           health.NewHandler(health.NewService(nil)),
		Session:          sess,
		AuthMW:           NewAuthMiddleware(sess, fakeUsers{users: map[uuid.UUID]accounts.User{}}, allowAllAuthz{}),
		CSRFFunc:         security.Token,
		ServicePublicSvc: svc,
		SiteName:         "Agentic CMS",
		Locale:           NewLocaleResolver(cat),
	})
}

func TestServicePublic_IndexRendersCardsAndEmpty(t *testing.T) {
	withCards := stubServicePublic{
		list:  []services.Service{{ID: uuid.New(), Title: "SEO Audit", Slug: "seo-audit", Summary: "We audit.", Price: "From $499", Status: kernel.StatusPublished}},
		total: 1,
	}
	h := NewServicePublicHandler(withCards, "Agentic CMS", "https://site.test")
	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	rec := httptest.NewRecorder()
	h.Index(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("index = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`data-testid="services-grid"`, "SEO Audit", "From $499"} {
		if !strings.Contains(body, want) {
			t.Errorf("index missing %q", want)
		}
	}

	empty := NewServicePublicHandler(stubServicePublic{}, "Agentic CMS", "https://site.test")
	rec2 := httptest.NewRecorder()
	empty.Index(rec2, httptest.NewRequest(http.MethodGet, "/services", nil))
	if !strings.Contains(rec2.Body.String(), `data-testid="services-index-empty"`) {
		t.Errorf("empty index missing empty state")
	}
}

func TestServicePublic_DetailRendersFAQAccordion(t *testing.T) {
	svc := services.Service{
		ID: uuid.New(), Title: "SEO Audit", Slug: "seo-audit",
		Summary: "We audit your site.", Body: "<p>Details.</p>", Price: "From $499",
		AreaServed: "Berlin", Status: kernel.StatusPublished,
		FAQs: []services.FAQ{{Question: "How long?", Answer: "<p>A week.</p>"}},
	}
	h := NewServicePublicHandler(stubServicePublic{svc: svc}, "Agentic CMS", "https://site.test")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("slug", "seo-audit")
	req := httptest.NewRequest(http.MethodGet, "/services/seo-audit", nil).
		WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.Show(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`data-testid="service-faq"`, "<details", "How long?", "<p>A week.</p>", "From $499", "Berlin"} {
		if !strings.Contains(body, want) {
			t.Errorf("detail missing %q", want)
		}
	}
}

// TestServicePublic_DeDetailRendersTranslation asserts /de/services/{slug}
// renders the de-overlaid content (M7b-2).
func TestServicePublic_DeDetailRendersTranslation(t *testing.T) {
	en := services.Service{ID: uuid.New(), Title: "SEO Audit", Slug: "seo-audit", Summary: "We audit.", Body: "<p>English</p>", Price: "$499", Status: kernel.StatusPublished}
	de := en
	de.Title = "SEO Pruefung"
	de.Body = "<p>Deutscher Text</p>"
	svc := stubServicePublic{svc: en, byLocale: map[i18n.Locale]services.Service{i18n.LocaleDE: de}}
	r := buildServicesPublicEnv(t, svc)
	req := httptest.NewRequest(http.MethodGet, "/de/services/seo-audit", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/de/services/seo-audit = %d\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "SEO Pruefung") || !strings.Contains(body, "<p>Deutscher Text</p>") {
		t.Errorf("de service did not render de translation:\n%s", body)
	}
}

// TestServicePublic_DeDetailFallsBackToEn asserts a de request for a service
// WITHOUT a de translation falls back to the base (en) content.
func TestServicePublic_DeDetailFallsBackToEn(t *testing.T) {
	en := services.Service{ID: uuid.New(), Title: "OnlyEnglish", Slug: "seo-audit", Body: "<p>English</p>", Status: kernel.StatusPublished}
	svc := stubServicePublic{svc: en} // no byLocale -> fallback
	r := buildServicesPublicEnv(t, svc)
	req := httptest.NewRequest(http.MethodGet, "/de/services/seo-audit", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/de/services/seo-audit = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "OnlyEnglish") {
		t.Error("de request without a translation did not fall back to en content")
	}
}

func TestServicePublic_DetailNotFound(t *testing.T) {
	h := NewServicePublicHandler(stubServicePublic{svcErr: services.ErrNotFound}, "Agentic CMS", "https://site.test")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("slug", "missing")
	req := httptest.NewRequest(http.MethodGet, "/services/missing", nil).
		WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.Show(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing service = %d, want 404", rec.Code)
	}
}
