package posts_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/content/categories"
	"github.com/huseyn0w/agentic-cms-go/internal/content/posts"
	"github.com/huseyn0w/agentic-cms-go/internal/content/tags"
)

// TestIntegration_AdminListFilteredByCategoryTagQ exercises the admin
// List/Count path's new CategorySlug/TagSlug/Q filters (in addition to the
// pre-existing Status/pagination). It seeds posts across 2 categories and 2
// tags with distinct titles/excerpts and asserts each axis, their
// intersection, and that empty filters reproduce prior (unfiltered) behavior.
func TestIntegration_AdminListFilteredByCategoryTagQ(t *testing.T) {
	w := newTaxWiring(t)
	ctx := context.Background()
	author := w.createUser(t, "adminfilter@test.local", accounts.RoleAdministrator)

	catGo, err := w.catSvc.Create(ctx, author, categories.CreateInput{Name: "Go"})
	if err != nil {
		t.Fatalf("create category Go: %v", err)
	}
	catRust, err := w.catSvc.Create(ctx, author, categories.CreateInput{Name: "Rust"})
	if err != nil {
		t.Fatalf("create category Rust: %v", err)
	}
	tagNews, err := w.tagSvc.Create(ctx, author, tags.CreateInput{Name: "News"})
	if err != nil {
		t.Fatalf("create tag News: %v", err)
	}
	tagTips, err := w.tagSvc.Create(ctx, author, tags.CreateInput{Name: "Tips"})
	if err != nil {
		t.Fatalf("create tag Tips: %v", err)
	}

	// goNews: category Go + tag News, title/excerpt contain "Concurrency".
	goNews, err := w.svc.Create(ctx, author, posts.CreateInput{
		Title:       "Concurrency Patterns",
		Excerpt:     "A tour of concurrency",
		Body:        "<p>b</p>",
		CategoryIDs: []uuid.UUID{catGo.ID},
		TagIDs:      []uuid.UUID{tagNews.ID},
	})
	if err != nil {
		t.Fatalf("create goNews: %v", err)
	}
	// goTips: category Go + tag Tips, distinct title, no "Concurrency" text.
	goTips, err := w.svc.Create(ctx, author, posts.CreateInput{
		Title:       "Editor Shortcuts",
		Excerpt:     "Handy tips",
		Body:        "<p>b</p>",
		CategoryIDs: []uuid.UUID{catGo.ID},
		TagIDs:      []uuid.UUID{tagTips.ID},
	})
	if err != nil {
		t.Fatalf("create goTips: %v", err)
	}
	// rustNews: category Rust + tag News.
	rustNews, err := w.svc.Create(ctx, author, posts.CreateInput{
		Title:       "Ownership Basics",
		Excerpt:     "Rust ownership",
		Body:        "<p>b</p>",
		CategoryIDs: []uuid.UUID{catRust.ID},
		TagIDs:      []uuid.UUID{tagNews.ID},
	})
	if err != nil {
		t.Fatalf("create rustNews: %v", err)
	}
	// both: assigned to BOTH tags (News + Tips) so a two-tag match must not be
	// duplicated when filtering by either tag individually.
	both, err := w.svc.Create(ctx, author, posts.CreateInput{
		Title:       "Dual Tagged Concurrency",
		Excerpt:     "shares both tags",
		Body:        "<p>b</p>",
		CategoryIDs: []uuid.UUID{catGo.ID},
		TagIDs:      []uuid.UUID{tagNews.ID, tagTips.ID},
	})
	if err != nil {
		t.Fatalf("create both: %v", err)
	}

	// --- CategorySlug axis ---------------------------------------------------
	items, err := w.repo.List(ctx, posts.ListFilter{CategorySlug: catGo.Slug, Limit: 50})
	if err != nil {
		t.Fatalf("list by category: %v", err)
	}
	if got := idSet(items); !got[goNews.ID] || !got[goTips.ID] || !got[both.ID] || got[rustNews.ID] {
		t.Fatalf("category filter ids = %v, want {goNews,goTips,both} excluding rustNews", got)
	}
	n, err := w.repo.Count(ctx, posts.ListFilter{CategorySlug: catGo.Slug})
	if err != nil || n != 3 {
		t.Fatalf("count by category = %d, %v, want 3", n, err)
	}

	// --- TagSlug axis: a post in two matching tags is not duplicated --------
	items, err = w.repo.List(ctx, posts.ListFilter{TagSlug: tagNews.Slug, Limit: 50})
	if err != nil {
		t.Fatalf("list by tag: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("tag filter len = %d, want 3 (no duplicate for 'both')", len(items))
	}
	if got := idSet(items); !got[goNews.ID] || !got[rustNews.ID] || !got[both.ID] || got[goTips.ID] {
		t.Fatalf("tag filter ids = %v, want {goNews,rustNews,both} excluding goTips", got)
	}
	n, err = w.repo.Count(ctx, posts.ListFilter{TagSlug: tagNews.Slug})
	if err != nil || n != 3 {
		t.Fatalf("count by tag = %d, %v, want 3", n, err)
	}

	// --- Q axis: matches title OR excerpt -------------------------------------
	items, err = w.repo.List(ctx, posts.ListFilter{Q: "Concurrency", Limit: 50})
	if err != nil {
		t.Fatalf("list by q: %v", err)
	}
	if got := idSet(items); len(got) != 2 || !got[goNews.ID] || !got[both.ID] {
		t.Fatalf("q filter ids = %v, want {goNews,both}", got)
	}
	n, err = w.repo.Count(ctx, posts.ListFilter{Q: "Concurrency"})
	if err != nil || n != 2 {
		t.Fatalf("count by q = %d, %v, want 2", n, err)
	}

	// --- Combined filters intersect ------------------------------------------
	items, err = w.repo.List(ctx, posts.ListFilter{CategorySlug: catGo.Slug, TagSlug: tagTips.Slug, Limit: 50})
	if err != nil {
		t.Fatalf("list combined: %v", err)
	}
	if got := idSet(items); len(got) != 2 || !got[goTips.ID] || !got[both.ID] {
		t.Fatalf("combined category+tag ids = %v, want {goTips,both}", got)
	}

	items, err = w.repo.List(ctx, posts.ListFilter{CategorySlug: catRust.Slug, Q: "Concurrency", Limit: 50})
	if err != nil {
		t.Fatalf("list combined rust+q: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("rust category has no 'Concurrency' post, got %v", idSet(items))
	}

	// --- Empty filters reproduce prior (unfiltered) behavior ------------------
	items, err = w.repo.List(ctx, posts.ListFilter{Limit: 50})
	if err != nil {
		t.Fatalf("list unfiltered: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("unfiltered list len = %d, want 4", len(items))
	}
	n, err = w.repo.Count(ctx, posts.ListFilter{})
	if err != nil || n != 4 {
		t.Fatalf("unfiltered count = %d, %v, want 4", n, err)
	}
}

func idSet(items []posts.Post) map[uuid.UUID]bool {
	out := make(map[uuid.UUID]bool, len(items))
	for _, p := range items {
		out[p.ID] = true
	}
	return out
}
