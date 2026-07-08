package cache

import (
	"context"
	"time"
)

// Noop is a Cache that stores nothing: every Get is a miss and every mutating
// operation is a no-op. It is used to disable caching (CACHE_DRIVER=noop) while
// keeping call sites unchanged. It is trivially safe for concurrent use.
type Noop struct{}

// NewNoop constructs a disabled cache.
func NewNoop() *Noop { return &Noop{} }

// Get always reports a miss.
func (Noop) Get(_ context.Context, _ string) ([]byte, bool, error) { return nil, false, nil }

// Set discards the value.
func (Noop) Set(_ context.Context, _ string, _ []byte, _ time.Duration) error { return nil }

// Delete does nothing.
func (Noop) Delete(_ context.Context, _ ...string) error { return nil }

// DeleteByPrefix does nothing.
func (Noop) DeleteByPrefix(_ context.Context, _ string) error { return nil }

// Clear does nothing.
func (Noop) Clear(_ context.Context) error { return nil }
