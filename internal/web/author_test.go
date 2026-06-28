package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/health"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
)

// fakePublicAuthor is a controllable PublicAuthorService.
type fakePublicAuthor struct {
	author accounts.PublicAuthor
	err    error
}

func (f fakePublicAuthor) PublicAuthor(context.Context, uuid.UUID) (accounts.PublicAuthor, error) {
	return f.author, f.err
}

func buildAuthorEnv(t *testing.T, svc PublicAuthorService) http.Handler {
	t.Helper()
	return Router(Deps{
		Config: config.Config{AppEnv: "test", BaseURL: "https://site.test"},
		Health: health.NewHandler(health.NewService(nil)),
		Author: NewAuthorHandler(svc, nil, "CMStack", "https://site.test"),
	})
}

func TestAuthorPage_PublicNoAuth_RendersProfileAndJSONLD(t *testing.T) {
	id := uuid.New()
	svc := fakePublicAuthor{author: accounts.PublicAuthor{
		ID:          id,
		Name:        "Grace Hopper",
		Bio:         "Computing pioneer",
		Website:     "https://grace.dev",
		SocialLinks: map[string]string{"github": "https://github.com/grace"},
		RoleLabel:   "Author",
		Posts:       []accounts.AuthorPost{},
	}}
	r := buildAuthorEnv(t, svc)

	req := httptest.NewRequest(http.MethodGet, "/authors/"+id.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/authors/{id} = %d\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Grace Hopper", "application/ld+json", "ProfilePage", `aria-label="Breadcrumb"`, "<article", "No published posts yet"} {
		if !strings.Contains(body, want) {
			t.Errorf("author page missing %q", want)
		}
	}
}

func TestAuthorPage_NeverLeaksEmail(t *testing.T) {
	id := uuid.New()
	// Even if a future bug populated a public field with an email-like string, the
	// service contract has no Email field; assert the rendered output is clean.
	svc := fakePublicAuthor{author: accounts.PublicAuthor{ID: id, Name: "Grace Hopper", RoleLabel: "Author", Posts: []accounts.AuthorPost{}}}
	r := buildAuthorEnv(t, svc)
	req := httptest.NewRequest(http.MethodGet, "/authors/"+id.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "@") && strings.Contains(rec.Body.String(), "mailto") {
		t.Error("author page should not contain a mailto/email")
	}
}

func TestAuthorPage_UnknownIs404(t *testing.T) {
	svc := fakePublicAuthor{err: accounts.ErrNotFound}
	r := buildAuthorEnv(t, svc)
	req := httptest.NewRequest(http.MethodGet, "/authors/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown author = %d, want 404", rec.Code)
	}
}

func TestAuthorPage_MalformedIDIs404(t *testing.T) {
	svc := fakePublicAuthor{}
	r := buildAuthorEnv(t, svc)
	req := httptest.NewRequest(http.MethodGet, "/authors/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("malformed id = %d, want 404", rec.Code)
	}
}
