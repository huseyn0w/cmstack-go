package posts_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/categories"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/posts"
	"github.com/huseyn0w/cmstack-go/internal/content/tags"
	"github.com/huseyn0w/cmstack-go/internal/content/taxonomy"
	"github.com/huseyn0w/cmstack-go/internal/platform/events"
)

// taxWiring is a posts stack with the M3 taxonomy assigner wired in.
type taxWiring struct {
	postsWiring
	catSvc *categories.Service
	tagSvc *tags.Service
}

func newTaxWiring(t *testing.T) taxWiring {
	t.Helper()
	w := newPostsWiring(t, time.Now)

	users := accounts.NewUserRepoPG(w.queries)
	roles := accounts.NewRoleRepoPG(w.queries)
	authz := accounts.NewAuthorizer(users, roles)

	catSvc := categories.NewService(w.pool, categories.NewRepoPG(w.queries), authz)
	tagSvc := tags.NewService(w.pool, tags.NewRepoPG(w.queries), authz)

	// Rebuild the post service with the taxonomy assigner attached so Create/
	// Update persist the M2M inside the post write tx.
	roleKeys := posts.NewRoleKeyResolver(users, roles)
	bus := events.NewBus(events.NewOutboxRepository())
	posts.NewPublishListener(slogDiscard(), nil, nil).Register(bus)
	svc := posts.NewService(w.pool, w.repo, w.revisions, authz, roleKeys, bus, time.Now).
		WithTaxonomy(taxonomy.NewAssigner(catSvc, tagSvc))
	w.svc = svc

	return taxWiring{postsWiring: w, catSvc: catSvc, tagSvc: tagSvc}
}

func TestIntegration_PostTaxonomyAssignPersistsAndLoads(t *testing.T) {
	w := newTaxWiring(t)
	ctx := context.Background()
	author := w.createUser(t, "taxauthor@test.local", accounts.RoleAdministrator)

	cat, err := w.catSvc.Create(ctx, author, categories.CreateInput{Name: "Engineering"})
	if err != nil {
		t.Fatalf("create cat: %v", err)
	}
	tag, err := w.tagSvc.Create(ctx, author, tags.CreateInput{Name: "Go"})
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}

	// Create a post WITH taxonomy in one shot (same tx as the insert).
	p, err := w.svc.Create(ctx, author, posts.CreateInput{
		Title:       "Tagged Post",
		Status:      kernel.StatusPublished,
		CategoryIDs: []uuid.UUID{cat.ID},
		TagIDs:      []uuid.UUID{tag.ID},
	})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}

	// The associations are loadable.
	gotCats, err := w.catSvc.CategoriesForPost(ctx, p.ID)
	if err != nil || len(gotCats) != 1 || gotCats[0].ID != cat.ID {
		t.Fatalf("categoriesForPost = %v / %v", gotCats, err)
	}
	gotTags, err := w.tagSvc.TagsForPost(ctx, p.ID)
	if err != nil || len(gotTags) != 1 || gotTags[0].ID != tag.ID {
		t.Fatalf("tagsForPost = %v / %v", gotTags, err)
	}

	// Update replacing taxonomy with an empty set clears it (in the update tx).
	if _, err := w.svc.Update(ctx, author, p.ID, posts.UpdateInput{SetTaxonomy: true}); err != nil {
		t.Fatalf("update clear taxonomy: %v", err)
	}
	gotCats, _ = w.catSvc.CategoriesForPost(ctx, p.ID)
	gotTags, _ = w.tagSvc.TagsForPost(ctx, p.ID)
	if len(gotCats) != 0 || len(gotTags) != 0 {
		t.Fatalf("after clear cats=%d tags=%d, want 0 0", len(gotCats), len(gotTags))
	}
}

func TestIntegration_RelatedPosts(t *testing.T) {
	w := newTaxWiring(t)
	ctx := context.Background()
	author := w.createUser(t, "related@test.local", accounts.RoleAdministrator)

	cat, _ := w.catSvc.Create(ctx, author, categories.CreateInput{Name: "Shared"})

	a, _ := w.svc.Create(ctx, author, posts.CreateInput{Title: "A", Status: kernel.StatusPublished, CategoryIDs: []uuid.UUID{cat.ID}})
	b, _ := w.svc.Create(ctx, author, posts.CreateInput{Title: "B", Status: kernel.StatusPublished, CategoryIDs: []uuid.UUID{cat.ID}})
	// C shares nothing.
	_, _ = w.svc.Create(ctx, author, posts.CreateInput{Title: "C", Status: kernel.StatusPublished})

	related, err := w.svc.Related(ctx, a.ID, 10)
	if err != nil {
		t.Fatalf("related: %v", err)
	}
	if len(related) != 1 || related[0].ID != b.ID {
		t.Fatalf("related = %v, want [B] (shares the category; excludes self + unrelated)", related)
	}
}

func TestIntegration_FilteredListingExcludesDrafts(t *testing.T) {
	w := newTaxWiring(t)
	ctx := context.Background()
	author := w.createUser(t, "filter@test.local", accounts.RoleAdministrator)

	cat, _ := w.catSvc.Create(ctx, author, categories.CreateInput{Name: "Filterable"})

	// A published post in the category, and a DRAFT post in the same category.
	pub, _ := w.svc.Create(ctx, author, posts.CreateInput{Title: "Pub", Status: kernel.StatusPublished, CategoryIDs: []uuid.UUID{cat.ID}})
	_, _ = w.svc.Create(ctx, author, posts.CreateInput{Title: "Draft", Status: kernel.StatusDraft, CategoryIDs: []uuid.UUID{cat.ID}})

	items, total, err := w.svc.PublicListFiltered(ctx, cat.Slug, "", 10, 0)
	if err != nil {
		t.Fatalf("filtered: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != pub.ID {
		t.Fatalf("filtered items=%v total=%d, want [pub] 1 (draft excluded)", items, total)
	}

	// A tag filter for a tag nobody has yields zero.
	_, total2, err := w.svc.PublicListFiltered(ctx, "", "nonexistent-tag", 10, 0)
	if err != nil {
		t.Fatalf("filtered empty: %v", err)
	}
	if total2 != 0 {
		t.Fatalf("empty filter total = %d, want 0", total2)
	}
}
