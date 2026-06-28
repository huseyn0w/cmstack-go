package posts

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/huseyn0w/cmstack-go/internal/platform/events"
)

// PublishListener consumes the async content.published events drained from the
// outbox and runs the post-publish side effects: cache invalidation and search
// reindex. Both are SEAMS for now — the concrete cache/search backends arrive in
// later milestones. The listener is the honest async handler the relay invokes
// after commit; it logs what it would do so the wiring is real even while the
// backends are stubs.
type PublishListener struct {
	log    *slog.Logger
	cache  CacheInvalidator
	search SearchReindexer
}

// CacheInvalidator drops cached renderings of a published post. The no-op
// implementation is wired now; a Redis/in-memory backend is the M13 upgrade.
type CacheInvalidator interface {
	InvalidatePost(ctx context.Context, slug string) error
}

// SearchReindexer (re)indexes a published post for full-text search. The no-op
// implementation is wired now; Postgres FTS / external search is M-later.
type SearchReindexer interface {
	ReindexPost(ctx context.Context, postID, slug string) error
}

// NewPublishListener constructs the listener. nil cache/search default to no-op
// seam implementations so the listener is always safe to register.
func NewPublishListener(log *slog.Logger, cache CacheInvalidator, search SearchReindexer) *PublishListener {
	if cache == nil {
		cache = noopCache{log: log}
	}
	if search == nil {
		search = noopSearch{log: log}
	}
	return &PublishListener{log: log, cache: cache, search: search}
}

// Register subscribes the async content.published handler. Call in BOTH the
// server (so the event is marked async + enqueued) and the worker (so the relay
// dispatches it).
func (l *PublishListener) Register(bus *events.Bus) {
	bus.SubscribeAsyncHandler(EventContentPublished, l.onContentPublished)
}

func (l *PublishListener) onContentPublished(ctx context.Context, payload []byte) error {
	var ev ContentPublishedEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return fmt.Errorf("unmarshal %s: %w", EventContentPublished, err)
	}
	if err := l.cache.InvalidatePost(ctx, ev.Slug); err != nil {
		return fmt.Errorf("invalidate cache for %q: %w", ev.Slug, err)
	}
	if err := l.search.ReindexPost(ctx, ev.PostID.String(), ev.Slug); err != nil {
		return fmt.Errorf("reindex %q: %w", ev.Slug, err)
	}
	return nil
}

// noopCache / noopSearch are the documented seam implementations: they log the
// intent so the async path is observable, and do nothing else.
type noopCache struct{ log *slog.Logger }

func (n noopCache) InvalidatePost(_ context.Context, slug string) error {
	if n.log != nil {
		n.log.Debug("cache invalidation seam (no-op)", "slug", slug)
	}
	return nil
}

type noopSearch struct{ log *slog.Logger }

func (n noopSearch) ReindexPost(_ context.Context, postID, slug string) error {
	if n.log != nil {
		n.log.Debug("search reindex seam (no-op)", "post_id", postID, "slug", slug)
	}
	return nil
}
