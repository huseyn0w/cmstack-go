package search

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// fakeRepo is a scripted Repository for the service strategy/URL/pagination tests.
// It records the arguments each method was called with and returns canned data.
type fakeRepo struct {
	ftsHits   []Hit
	ftsCount  int
	ftsErr    error
	ilikeHits []Hit
	ilikeCnt  int
	ilikeErr  error

	ftsCalled   bool
	ilikeCalled bool
	lastLimit   int
	lastOffset  int
	lastQuery   string
}

func (f *fakeRepo) FTS(_ context.Context, q string, limit, offset int) ([]Hit, error) {
	f.ftsCalled = true
	f.lastQuery, f.lastLimit, f.lastOffset = q, limit, offset
	return f.ftsHits, f.ftsErr
}

func (f *fakeRepo) CountFTS(_ context.Context, q string) (int, error) {
	f.lastQuery = q
	return f.ftsCount, f.ftsErr
}

func (f *fakeRepo) ILIKE(_ context.Context, q string, limit, offset int) ([]Hit, error) {
	f.ilikeCalled = true
	f.lastQuery, f.lastLimit, f.lastOffset = q, limit, offset
	return f.ilikeHits, f.ilikeErr
}

func (f *fakeRepo) CountILIKE(_ context.Context, q string) (int, error) {
	f.lastQuery = q
	return f.ilikeCnt, f.ilikeErr
}

func TestSearch_BlankQueryReturnsEmptyNoRepoCall(t *testing.T) {
	for _, q := range []string{"", "   ", "\t\n"} {
		repo := &fakeRepo{ftsCount: 5, ftsHits: []Hit{{Type: HitPost}}}
		svc := NewService(repo)
		res, err := svc.Search(context.Background(), q, 1, 10)
		if err != nil {
			t.Fatalf("q=%q: unexpected err %v", q, err)
		}
		if len(res.Hits) != 0 || res.Total != 0 {
			t.Errorf("q=%q: expected empty result, got %d hits / total %d", q, len(res.Hits), res.Total)
		}
		if res.Query != "" {
			t.Errorf("q=%q: expected trimmed empty query, got %q", q, res.Query)
		}
		if repo.ftsCalled || repo.ilikeCalled {
			t.Errorf("q=%q: repo should not be queried for a blank query", q)
		}
	}
}

func TestSearch_FTSFirstNoFallbackWhenMatches(t *testing.T) {
	repo := &fakeRepo{
		ftsCount: 2,
		ftsHits: []Hit{
			{Type: HitPost, Slug: "a", Title: "A"},
			{Type: HitService, Slug: "b", Title: "B"},
		},
	}
	svc := NewService(repo)
	res, err := svc.Search(context.Background(), "  golang  ", 1, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if res.Query != "golang" {
		t.Errorf("query not trimmed: %q", res.Query)
	}
	if !repo.ftsCalled {
		t.Error("FTS should have been called")
	}
	if repo.ilikeCalled {
		t.Error("ILIKE fallback should NOT run when FTS matched")
	}
	if res.Fallback {
		t.Error("Fallback flag should be false when FTS matched")
	}
	if res.Total != 2 || len(res.Hits) != 2 {
		t.Errorf("expected 2 hits, got total=%d len=%d", res.Total, len(res.Hits))
	}
}

func TestSearch_FallsBackToILIKEWhenFTSEmpty(t *testing.T) {
	repo := &fakeRepo{
		ftsCount:  0,
		ilikeCnt:  1,
		ilikeHits: []Hit{{Type: HitPage, Slug: "about", Title: "About"}},
	}
	svc := NewService(repo)
	res, err := svc.Search(context.Background(), "tgres", 1, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !repo.ilikeCalled {
		t.Error("ILIKE fallback should run when FTS found nothing")
	}
	if !res.Fallback {
		t.Error("Fallback flag should be true")
	}
	if res.Total != 1 || len(res.Hits) != 1 {
		t.Errorf("expected 1 fallback hit, got total=%d len=%d", res.Total, len(res.Hits))
	}
}

func TestSearch_FallbackEmptyWhenNeitherMatches(t *testing.T) {
	repo := &fakeRepo{ftsCount: 0, ilikeCnt: 0}
	svc := NewService(repo)
	res, err := svc.Search(context.Background(), "zzzznope", 1, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !res.Fallback || res.Total != 0 || len(res.Hits) != 0 {
		t.Errorf("expected empty fallback result, got fallback=%v total=%d len=%d", res.Fallback, res.Total, len(res.Hits))
	}
	if repo.ilikeCalled {
		t.Error("ILIKE hit-fetch should be skipped when its count is 0")
	}
}

func TestSearch_BuildsPublicURLsPerType(t *testing.T) {
	repo := &fakeRepo{
		ftsCount: 3,
		ftsHits: []Hit{
			{Type: HitPost, Slug: "my-post"},
			{Type: HitPage, Slug: "about"},
			{Type: HitService, Slug: "seo"},
		},
	}
	svc := NewService(repo)
	res, err := svc.Search(context.Background(), "x", 1, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	want := map[HitType]string{
		HitPost:    "/blog/my-post",
		HitPage:    "/p/about",
		HitService: "/services/seo",
	}
	for _, h := range res.Hits {
		if h.URL != want[h.Type] {
			t.Errorf("type %s: URL = %q, want %q", h.Type, h.URL, want[h.Type])
		}
	}
}

func TestSearch_PaginationMath(t *testing.T) {
	repo := &fakeRepo{ftsCount: 25, ftsHits: []Hit{{Type: HitPost, Slug: "s"}}}
	svc := NewService(repo)
	if _, err := svc.Search(context.Background(), "x", 3, 10); err != nil {
		t.Fatalf("search: %v", err)
	}
	if repo.lastLimit != 10 {
		t.Errorf("limit = %d, want 10", repo.lastLimit)
	}
	if repo.lastOffset != 20 { // (page 3 - 1) * 10
		t.Errorf("offset = %d, want 20", repo.lastOffset)
	}
}

func TestSearch_DefaultsAndCaps(t *testing.T) {
	repo := &fakeRepo{ftsCount: 1, ftsHits: []Hit{{Type: HitPost, Slug: "s"}}}
	svc := NewService(repo)

	// perPage <= 0 -> default; page < 1 -> 1.
	res, _ := svc.Search(context.Background(), "x", 0, 0)
	if res.PerPage != DefaultPerPage || res.Page != 1 {
		t.Errorf("defaults: perPage=%d page=%d", res.PerPage, res.Page)
	}
	// perPage over cap -> maxPerPage.
	res, _ = svc.Search(context.Background(), "x", 1, 9999)
	if res.PerPage != maxPerPage {
		t.Errorf("cap: perPage=%d want %d", res.PerPage, maxPerPage)
	}
}

func TestSearch_PropagatesRepoError(t *testing.T) {
	repo := &fakeRepo{ftsErr: errors.New("boom")}
	svc := NewService(repo)
	if _, err := svc.Search(context.Background(), "x", 1, 10); err == nil {
		t.Fatal("expected error to propagate")
	}
}

// ensure uuid import is exercised (hits carry ids in real use).
var _ = uuid.Nil
