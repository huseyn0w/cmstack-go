package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/services"
)

type stubServicePublic struct {
	svc    services.Service
	svcErr error
	list   []services.Service
	total  int
}

func (s stubServicePublic) PublicBySlug(context.Context, string) (services.Service, error) {
	return s.svc, s.svcErr
}

func (s stubServicePublic) PublicList(context.Context, int, int) ([]services.Service, int, error) {
	return s.list, s.total, nil
}

func TestServicePublic_IndexRendersCardsAndEmpty(t *testing.T) {
	withCards := stubServicePublic{
		list:  []services.Service{{ID: uuid.New(), Title: "SEO Audit", Slug: "seo-audit", Summary: "We audit.", Price: "From $499", Status: kernel.StatusPublished}},
		total: 1,
	}
	h := NewServicePublicHandler(withCards, "CMStack", "https://site.test")
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

	empty := NewServicePublicHandler(stubServicePublic{}, "CMStack", "https://site.test")
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
	h := NewServicePublicHandler(stubServicePublic{svc: svc}, "CMStack", "https://site.test")

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

func TestServicePublic_DetailNotFound(t *testing.T) {
	h := NewServicePublicHandler(stubServicePublic{svcErr: services.ErrNotFound}, "CMStack", "https://site.test")
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
