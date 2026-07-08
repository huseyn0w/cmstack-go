package cache

import (
	"context"
	"strings"
	"sync"
	"time"
)

// entry is a single stored value with an optional absolute expiry. A zero
// expiresAt means the entry never expires.
type entry struct {
	val       []byte
	expiresAt time.Time
}

// Memory is an in-process, mutex-guarded cache. It is the default backend and
// is safe for concurrent use. Expiry is evaluated lazily on Get: an expired
// entry is treated as a miss and deleted, so no background janitor is required.
type Memory struct {
	now func() time.Time

	mu    sync.Mutex
	items map[string]entry
}

// NewMemory constructs an empty in-memory cache.
func NewMemory() *Memory {
	return &Memory{
		now:   time.Now,
		items: make(map[string]entry),
	}
}

// Get returns the value stored under key, or ok=false on a miss or an expired
// entry (which is deleted as a side effect). It never returns an error.
func (m *Memory) Get(_ context.Context, key string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.items[key]
	if !ok {
		return nil, false, nil
	}
	if !e.expiresAt.IsZero() && !m.now().Before(e.expiresAt) {
		delete(m.items, key)
		return nil, false, nil
	}
	// Return a copy so callers cannot mutate the stored bytes.
	out := make([]byte, len(e.val))
	copy(out, e.val)
	return out, true, nil
}

// Set stores val under key. A ttl of zero or less means the entry never
// expires.
func (m *Memory) Set(_ context.Context, key string, val []byte, ttl time.Duration) error {
	cp := make([]byte, len(val))
	copy(cp, val)

	var exp time.Time
	if ttl > 0 {
		exp = m.now().Add(ttl)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[key] = entry{val: cp, expiresAt: exp}
	return nil
}

// Delete removes the given keys. It never returns an error.
func (m *Memory) Delete(_ context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.items, k)
	}
	return nil
}

// DeleteByPrefix removes every key whose name begins with prefix by iterating
// the map with strings.HasPrefix. It never returns an error.
func (m *Memory) DeleteByPrefix(_ context.Context, prefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.items {
		if strings.HasPrefix(k, prefix) {
			delete(m.items, k)
		}
	}
	return nil
}

// Clear resets the cache to empty. It never returns an error.
func (m *Memory) Clear(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = make(map[string]entry)
	return nil
}
