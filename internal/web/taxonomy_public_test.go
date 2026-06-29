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
	"github.com/huseyn0w/cmstack-go/internal/content/categories"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/posts"
	"github.com/huseyn0w/cmstack-go/internal/content/tags"
	"github.com/huseyn0w/cmstack-go/internal/health"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
	"github.com/huseyn0w/cmstack-go/internal/platform/session"
)

type stubCategoryPublic struct {
	cat    categories.Category
	catErr error
	ids    []uuid.UUID
	total  int
}

func (s stubCategoryPublic) PublicBySlug(context.Context, string) (categories.Category, error) {
	return s.cat, s.catErr
}

func (s stubCategoryPublic) PublishedPostIDs(context.Context, uuid.UUID, int, int) ([]uuid.UUID, int, error) {
	return s.ids, s.total, nil
}

type stubTagPublic struct {
	tag    tags.Tag
	tagErr error
	ids    []uuid.UUID
	total  int
}

func (s stubTagPublic) PublicBySlug(context.Context, string) (tags.Tag, error) {
	return s.tag, s.tagErr
}

func (s stubTagPublic) PublishedPostIDs(context.Context, uuid.UUID, int, int) ([]uuid.UUID, int, error) {
	return s.ids, s.total, nil
}

type stubHydrator struct{ out []posts.Post }

func (s stubHydrator) PublishedByIDs(context.Context, []uuid.UUID) ([]posts.Post, error) {
	return s.out, nil
}

func buildArchiveEnv(t *testing.T, cat CategoryPublicService, tag TagPublicService, hyd PostHydrator) http.Handler {
	t.Helper()
	sess := session.NewManager(false)
	return Router(Deps{
		Config:            config.Config{AppEnv: "test", BaseURL: "https://site.test"},
		Health:            health.NewHandler(health.NewService(nil)),
		Session:           sess,
		AuthMW:            NewAuthMiddleware(sess, fakeUsers{users: map[uuid.UUID]accounts.User{}}, allowAllAuthz{}),
		CSRFFunc:          security.Token,
		Authors:           fakeUsers{users: map[uuid.UUID]accounts.User{}},
		SiteName:          "CMStack",
		CategoryPublicSvc: cat,
		TagPublicSvc:      tag,
		PostHydrateSvc:    hyd,
	})
}

func TestArchive_CategoryRendersPosts(t *testing.T) {
	postID := uuid.New()
	cat := stubCategoryPublic{
		cat:   categories.Category{ID: uuid.New(), Name: "Engineering", Slug: "engineering", Description: "<p>About eng</p>"},
		ids:   []uuid.UUID{postID},
		total: 1,
	}
	hyd := stubHydrator{out: []posts.Post{{ID: postID, Title: "Hello", Slug: "hello", Status: kernel.StatusPublished, PublishedAt: ptrTime(time.Now())}}}
	r := buildArchiveEnv(t, cat, stubTagPublic{}, hyd)

	req := httptest.NewRequest(http.MethodGet, "/categories/engineering", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("archive = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"archive-grid", "Engineering", "Hello", "About eng"} {
		if !strings.Contains(body, want) {
			t.Fatalf("archive body missing %q", want)
		}
	}
}

func TestArchive_CategoryEmptyState(t *testing.T) {
	cat := stubCategoryPublic{cat: categories.Category{ID: uuid.New(), Name: "Empty", Slug: "empty"}}
	r := buildArchiveEnv(t, cat, stubTagPublic{}, stubHydrator{})
	req := httptest.NewRequest(http.MethodGet, "/categories/empty", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "archive-empty") {
		t.Fatalf("empty archive state not rendered")
	}
}

func TestArchive_UnknownCategoryIs404(t *testing.T) {
	cat := stubCategoryPublic{catErr: categories.ErrNotFound}
	r := buildArchiveEnv(t, cat, stubTagPublic{}, stubHydrator{})
	req := httptest.NewRequest(http.MethodGet, "/categories/nope", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown category = %d, want 404", rec.Code)
	}
}

func TestArchive_TagRendersPosts(t *testing.T) {
	postID := uuid.New()
	tag := stubTagPublic{
		tag:   tags.Tag{ID: uuid.New(), Name: "Go", Slug: "go"},
		ids:   []uuid.UUID{postID},
		total: 1,
	}
	hyd := stubHydrator{out: []posts.Post{{ID: postID, Title: "Concurrency", Slug: "concurrency", Status: kernel.StatusPublished, PublishedAt: ptrTime(time.Now())}}}
	r := buildArchiveEnv(t, stubCategoryPublic{}, tag, hyd)
	req := httptest.NewRequest(http.MethodGet, "/tags/go", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Concurrency") {
		t.Fatalf("tag archive code=%d missing post", rec.Code)
	}
}
