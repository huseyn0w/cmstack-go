package web

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/agentic-cms-go/internal/content/posts"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/cache"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/events"
)

// CacheInvalidator drops the public read caches (page responses + sitemap) when
// content is published. It runs IN the web process — the cache lives here, not
// in the outbox worker — via a SYNCHRONOUS in-process listener on the
// content.published event. The bus dispatches sync listeners inside the
// publishing transaction, so this fires the moment a post/page/service is
// published on the same server that owns the cache.
//
// Scope note (staleness bound): content.published is the only reliable in-tx
// signal — there is no unpublish/trash/silent-edit event. Those mutations are
// therefore bounded by the page-cache TTL rather than invalidated eagerly. A
// publish (the common freshness-critical case) clears immediately.
type CacheInvalidator struct {
	cache cache.Cache
}

// NewCacheInvalidator builds the invalidator over c. A nil cache yields a nil
// *CacheInvalidator whose Register is a no-op, so wiring stays optional.
func NewCacheInvalidator(c cache.Cache) *CacheInvalidator {
	if c == nil {
		return nil
	}
	return &CacheInvalidator{cache: c}
}

// Register subscribes the invalidator to content.published as a synchronous
// listener on the web bus. Call it on the SERVER bus (the process that serves
// public reads); it is a no-op when the invalidator is nil.
func (ci *CacheInvalidator) Register(bus *events.Bus) {
	if ci == nil || bus == nil {
		return
	}
	bus.SubscribeSync(posts.EventContentPublished, ci.onPublished)
}

// onPublished clears the page + sitemap caches. It ignores the event payload:
// any publish invalidates every public read (the caches are cheap to rebuild and
// correctness beats granularity). An over-broad clear is always safe — the worst
// case is a cache miss and a re-render.
func (ci *CacheInvalidator) onPublished(ctx context.Context, _ pgx.Tx, _ events.Event) error {
	ci.Invalidate(ctx)
	return nil
}

// Invalidate drops every "page:" and "sitemap:" entry. Errors are swallowed: a
// failed invalidation must never roll back the content transaction that
// triggered it — a stale cache entry is bounded by the TTL, but a lost publish
// is not acceptable.
func (ci *CacheInvalidator) Invalidate(ctx context.Context) {
	if ci == nil {
		return
	}
	_ = ci.cache.DeleteByPrefix(ctx, "page:")
	_ = ci.cache.DeleteByPrefix(ctx, "sitemap:")
}
