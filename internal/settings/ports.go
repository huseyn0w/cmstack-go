// Package settings is a generic key/value site-settings store — the first
// DB-backed settings surface (M9-1). It holds a single flat namespace of string
// values keyed by a stable string (e.g. "active_theme"). The store is
// deliberately schema-light so later milestones (M15 admin settings) extend it
// without a migration per toggle. The layering mirrors the rest of the repo:
// handler -> service -> repo, and the repo is the ONLY layer touching sqlc/pgx.
package settings

import (
	"context"
	"errors"
)

// ErrNotFound is the sentinel every repository returns when a key is absent. The
// service maps it to a "not present" outcome and never leaks it to callers.
var ErrNotFound = errors.New("settings: not found")

// Repo is the data-access contract for the site-settings store. It is the ONLY
// layer permitted to touch sqlc/pgx; the service depends solely on this
// interface, which keeps it trivially fakeable in tests.
type Repo interface {
	// Get returns the stored value for key, or ErrNotFound when the key is absent.
	Get(ctx context.Context, key string) (string, error)
	// Set upserts value under key (insert or overwrite).
	Set(ctx context.Context, key, value string) error
	// All returns every stored key/value pair.
	All(ctx context.Context) (map[string]string, error)
}
