package settings

import (
	"context"
	"sync"
)

// keyActiveTheme is the settings key holding the active public theme id. An
// absent key resolves to the registry default in the web layer.
const keyActiveTheme = "active_theme"

// Service wraps the settings Repo with an in-process cache for hot reads. The
// active theme is read on every public request, so the cache avoids a DB round
// trip per page render. The cache mirrors the authz cache pattern used
// elsewhere in the repo: a mutex-guarded map populated lazily from All() and
// fully cleared on any write, so a Set is immediately consistent for the next
// read.
type Service struct {
	repo Repo

	mu     sync.RWMutex
	cache  map[string]string // full snapshot; nil until first load
	loaded bool
}

// NewService constructs a Service over repo.
func NewService(repo Repo) *Service {
	return &Service{repo: repo}
}

// Get returns the value stored under key. The boolean is false when the key is
// unset (no value has been written). A non-nil error signals a store failure
// (distinct from an unset key, which returns ("", false, nil)).
func (s *Service) Get(ctx context.Context, key string) (string, bool, error) {
	snap, err := s.snapshot(ctx)
	if err != nil {
		return "", false, err
	}
	v, ok := snap[key]
	return v, ok, nil
}

// Set upserts value under key and invalidates the cache so the next read
// reflects the write.
func (s *Service) Set(ctx context.Context, key, value string) error {
	if err := s.repo.Set(ctx, key, value); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// ActiveTheme returns the stored active theme id, or "" when it is unset or the
// store errors. The web layer validates the id against the theme registry and
// falls back to the default, so a transient store error degrades gracefully to
// the base palette rather than surfacing an error on every public page.
func (s *Service) ActiveTheme(ctx context.Context) string {
	v, ok, err := s.Get(ctx, keyActiveTheme)
	if err != nil || !ok {
		return ""
	}
	return v
}

// SetActiveTheme persists id as the active public theme.
func (s *Service) SetActiveTheme(ctx context.Context, id string) error {
	return s.Set(ctx, keyActiveTheme, id)
}

// snapshot returns the cached full settings map, loading it once from the repo
// on first use. Concurrent first-loads are harmless (the last write wins; both
// reflect the same store state).
func (s *Service) snapshot(ctx context.Context) (map[string]string, error) {
	s.mu.RLock()
	if s.loaded {
		snap := s.cache
		s.mu.RUnlock()
		return snap, nil
	}
	s.mu.RUnlock()

	all, err := s.repo.All(ctx)
	if err != nil {
		return nil, err
	}
	if all == nil {
		all = map[string]string{}
	}

	s.mu.Lock()
	s.cache = all
	s.loaded = true
	s.mu.Unlock()
	return all, nil
}

// invalidate clears the cached snapshot so the next read reloads from the repo.
func (s *Service) invalidate() {
	s.mu.Lock()
	s.cache = nil
	s.loaded = false
	s.mu.Unlock()
}
