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
	"github.com/huseyn0w/cmstack-go/internal/content/pages"
)

type stubPagePublic struct {
	page      pages.Page
	pageErr   error
	ancestors []pages.Page
}

func (s stubPagePublic) PublicBySlug(context.Context, string) (pages.Page, error) {
	return s.page, s.pageErr
}

func (s stubPagePublic) Ancestors(context.Context, pages.Page) ([]pages.Page, error) {
	return s.ancestors, nil
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
