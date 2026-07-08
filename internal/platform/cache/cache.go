// Package cache provides a small key/value cache abstraction with an in-memory
// default and a Redis backend, selected at startup via configuration. Values
// are opaque byte slices: callers are responsible for (un)marshalling. Keys are
// opaque strings; DeleteByPrefix enables tag-style group invalidation (for
// example deleting every "page:" entry at once).
package cache

import (
	"context"
	"time"
)

// Cache is a byte-oriented key/value store. Implementations must be safe for
// concurrent use by multiple goroutines.
type Cache interface {
	// Get returns the value stored under key. The boolean is false on a miss
	// (including an expired entry); err is non-nil only on a backend failure.
	Get(ctx context.Context, key string) ([]byte, bool, error)

	// Set stores val under key. A ttl of zero or less means the entry never
	// expires. Setting an existing key overwrites its value and expiry.
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error

	// Delete removes the given keys. Removing a missing key is not an error.
	// Calling Delete with no keys is a no-op.
	Delete(ctx context.Context, keys ...string) error

	// DeleteByPrefix removes every key whose name begins with prefix. It powers
	// tag-style group invalidation (e.g. DeleteByPrefix(ctx, "page:")).
	DeleteByPrefix(ctx context.Context, prefix string) error

	// Clear removes every entry owned by this cache.
	Clear(ctx context.Context) error
}
