package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/posts"
	"github.com/huseyn0w/cmstack-go/internal/health"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
	"github.com/huseyn0w/cmstack-go/internal/platform/session"
)

// stubPostPublic is a controllable PostPublicService.
type stubPostPublic struct {
	bySlug    posts.Post
	bySlugErr error
	list      []posts.Post
	total     int
}

func (s stubPostPublic) PublicBySlug(context.Context, string) (posts.Post, error) {
	return s.bySlug, s.bySlugErr
}

func (s stubPostPublic) PublicList(context.Context, int, int) ([]posts.Post, int, error) {
	return s.list, s.total, nil
}

func (s stubPostPublic) PublicListFiltered(context.Context, string, string, int, int) ([]posts.Post, int, error) {
	return s.list, s.total, nil
}

func (s stubPostPublic) Related(context.Context, uuid.UUID, int) ([]posts.Post, error) {
	return nil, nil
}

func (s stubPostPublic) Like(context.Context, uuid.UUID, uuid.UUID) (posts.Post, error) {
	return s.bySlug, nil
}

func (s stubPostPublic) Unlike(context.Context, uuid.UUID, uuid.UUID) (posts.Post, error) {
	return s.bySlug, nil
}

func (s stubPostPublic) HasLiked(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}

func buildPublicEnv(t *testing.T, svc PostPublicService) http.Handler {
	t.Helper()
	sess := session.NewManager(false)
	return Router(Deps{
		Config:        config.Config{AppEnv: "test", BaseURL: "https://site.test"},
		Health:        health.NewHandler(health.NewService(nil)),
		Session:       sess,
		AuthMW:        NewAuthMiddleware(sess, fakeUsers{users: map[uuid.UUID]accounts.User{}}, allowAllAuthz{}),
		CSRFFunc:      security.Token,
		PostPublicSvc: svc,
		Authors:       fakeUsers{users: map[uuid.UUID]accounts.User{}},
		SiteName:      "CMStack",
	})
}

func TestPublicBlog_DetailRenders(t *testing.T) {
	p := posts.Post{
		ID: uuid.New(), Title: "Hello", Slug: "hello", Body: "<p>Body</p>",
		Status: kernel.StatusPublished, AuthorID: uuid.New(),
		PublishedAt: ptrTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), ReadingTime: 2,
	}
	r := buildPublicEnv(t, stubPostPublic{bySlug: p})
	req := httptest.NewRequest(http.MethodGet, "/blog/hello", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/blog/hello = %d\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"<article", "Hello", "<p>Body</p>", "application/ld+json", `data-testid="like-signin"`} {
		if !strings.Contains(body, want) {
			t.Errorf("post detail missing %q", want)
		}
	}
}

func TestPublicBlog_UnknownSlugIs404(t *testing.T) {
	r := buildPublicEnv(t, stubPostPublic{bySlugErr: posts.ErrNotFound})
	req := httptest.NewRequest(http.MethodGet, "/blog/missing", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown slug = %d, want 404", rec.Code)
	}
}

func TestPublicBlog_IndexEmptyState(t *testing.T) {
	r := buildPublicEnv(t, stubPostPublic{})
	req := httptest.NewRequest(http.MethodGet, "/blog", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/blog = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `data-testid="blog-empty"`) {
		t.Error("empty blog index missing empty state")
	}
}

func TestPublicBlog_LikeRequiresAuth(t *testing.T) {
	r := buildPublicEnv(t, stubPostPublic{bySlug: posts.Post{Slug: "hello"}})
	req := httptest.NewRequest(http.MethodPost, "/blog/hello/like", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	// An anonymous like must be blocked: either CSRF rejects it (400, the token is
	// missing) or RequireAuth redirects to /login (303). Never a successful like.
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusBadRequest {
		t.Fatalf("anon like = %d, want 303 (login) or 400 (csrf)", rec.Code)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
