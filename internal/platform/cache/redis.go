package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// scanCount is the COUNT hint passed to SCAN so prefix operations page through
// keys in reasonably sized batches instead of one-by-one.
const scanCount = 256

// Redis is a Redis-backed Cache. Every key is namespaced with keyPrefix so a
// single Redis instance/database can be shared between apps without collisions,
// and so Clear/DeleteByPrefix only ever touch this app's keys. It is safe for
// concurrent use (the underlying *redis.Client is).
type Redis struct {
	client    *redis.Client
	keyPrefix string
}

// NewRedis wraps a connected *redis.Client, prefixing every key with keyPrefix
// (e.g. "cmstack:"). The prefix may be empty, though a namespace is recommended
// when the Redis database is shared.
func NewRedis(client *redis.Client, keyPrefix string) *Redis {
	return &Redis{client: client, keyPrefix: keyPrefix}
}

// k returns the fully namespaced Redis key for a caller-supplied key.
func (r *Redis) k(key string) string { return r.keyPrefix + key }

// Get returns the value stored under key. redis.Nil (a genuine miss) yields
// ok=false with no error; any other backend error is returned.
func (r *Redis) Get(ctx context.Context, key string) ([]byte, bool, error) {
	b, err := r.client.Get(ctx, r.k(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

// Set stores val under key. A ttl of zero or less means no expiry (go-redis
// treats a zero expiration as "no expiry").
func (r *Redis) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	if ttl < 0 {
		ttl = 0
	}
	return r.client.Set(ctx, r.k(key), val, ttl).Err()
}

// Delete removes the given keys via a single DEL. Removing missing keys is not
// an error. Calling Delete with no keys is a no-op.
func (r *Redis) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	namespaced := make([]string, len(keys))
	for i, key := range keys {
		namespaced[i] = r.k(key)
	}
	return r.client.Del(ctx, namespaced...).Err()
}

// DeleteByPrefix removes every key beginning with prefix. It uses SCAN (never
// KEYS, which blocks the server) to iterate the cursor in batches, deleting the
// matched keys as it goes.
func (r *Redis) DeleteByPrefix(ctx context.Context, prefix string) error {
	return r.scanDelete(ctx, r.k(prefix)+"*")
}

// Clear removes every key owned by this cache by scan-deleting the app key
// namespace. It deliberately does NOT issue FLUSHDB so a shared Redis database
// is never wiped.
func (r *Redis) Clear(ctx context.Context) error {
	return r.scanDelete(ctx, r.keyPrefix+"*")
}

// scanDelete iterates keys matching pattern via SCAN and deletes them in
// batches. The keys returned by SCAN are already fully namespaced, so they are
// passed to DEL unchanged.
func (r *Redis) scanDelete(ctx context.Context, pattern string) error {
	var cursor uint64
	for {
		keys, next, err := r.client.Scan(ctx, cursor, pattern, scanCount).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := r.client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}
