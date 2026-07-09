package menus

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/platform/cache"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
)

// countingRepo wraps memRepo to count ListItemsInLocale calls — the resolve
// query. A stable count across two resolves proves the second was a cache hit.
type countingRepo struct {
	*memRepo
	resolveCalls int
}

func (c *countingRepo) ListItemsInLocale(ctx context.Context, menuID uuid.UUID, locale string) ([]Item, error) {
	c.resolveCalls++
	return c.memRepo.ListItemsInLocale(ctx, menuID, locale)
}

func newCountingRepo() *countingRepo { return &countingRepo{memRepo: newMemRepo()} }

func TestResolveForLocation_CachedThenInvalidatedOnMutation(t *testing.T) {
	ctx := context.Background()
	repo := newCountingRepo()
	svc := NewService(nil, repo, allowAuthz{}).WithCache(cache.NewMemory(), time.Minute)

	// Seed a menu + item at "header". These mutations invalidate the (empty) cache.
	menu, err := svc.CreateMenu(ctx, actor, "Primary", "header")
	if err != nil {
		t.Fatalf("create menu: %v", err)
	}
	if _, err := svc.AddItem(ctx, actor, menu.ID, ItemInput{Type: ItemCustom, URL: "/about", Label: "About"}); err != nil {
		t.Fatalf("add item: %v", err)
	}

	// First resolve: cache miss -> queries the repo.
	if _, err := svc.ResolveForLocation(ctx, "header", i18n.Default()); err != nil {
		t.Fatalf("resolve 1: %v", err)
	}
	if repo.resolveCalls != 1 {
		t.Fatalf("resolve queries after first resolve = %d, want 1", repo.resolveCalls)
	}

	// Second resolve: cache hit -> no repo query.
	if _, err := svc.ResolveForLocation(ctx, "header", i18n.Default()); err != nil {
		t.Fatalf("resolve 2: %v", err)
	}
	if repo.resolveCalls != 1 {
		t.Fatalf("resolve queries after cached resolve = %d, want 1 (served from cache)", repo.resolveCalls)
	}

	// A menu mutation invalidates the cache.
	if _, err := svc.AddItem(ctx, actor, menu.ID, ItemInput{Type: ItemCustom, URL: "/contact", Label: "Contact"}); err != nil {
		t.Fatalf("add second item: %v", err)
	}

	// Third resolve: cache was cleared -> re-queries the repo.
	if _, err := svc.ResolveForLocation(ctx, "header", i18n.Default()); err != nil {
		t.Fatalf("resolve 3: %v", err)
	}
	if repo.resolveCalls != 2 {
		t.Fatalf("resolve queries after mutation = %d, want 2 (mutation must invalidate)", repo.resolveCalls)
	}
}

func TestResolveForLocation_NilCacheQueriesEveryTime(t *testing.T) {
	ctx := context.Background()
	repo := newCountingRepo()
	svc := NewService(nil, repo, allowAuthz{}) // no cache

	menu, _ := svc.CreateMenu(ctx, actor, "Primary", "header")
	_, _ = svc.AddItem(ctx, actor, menu.ID, ItemInput{Type: ItemCustom, URL: "/about", Label: "About"})

	_, _ = svc.ResolveForLocation(ctx, "header", i18n.Default())
	_, _ = svc.ResolveForLocation(ctx, "header", i18n.Default())
	if repo.resolveCalls != 2 {
		t.Fatalf("resolve queries = %d, want 2 (nil cache queries every time)", repo.resolveCalls)
	}
}
