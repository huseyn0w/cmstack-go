package cache

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Config selects and configures a cache backend. It is intentionally a small
// local struct rather than a dependency on internal/platform/config, so this
// package stays import-cycle free; the caller in cmd/server maps its
// config.Config onto this.
type Config struct {
	// Driver selects the backend: "memory" (default), "redis" or "noop".
	Driver string
	// RedisURL is the connection URL when Driver is "redis"
	// (e.g. redis://localhost:6379/0).
	RedisURL string
	// KeyPrefix namespaces every Redis key (e.g. "agentic-cms:").
	KeyPrefix string
}

// New constructs the Cache selected by cfg.Driver:
//
//   - "" or "memory" -> *Memory (the default in-process backend)
//   - "noop"         -> *Noop (caching disabled)
//   - "redis"        -> *Redis over a client parsed from RedisURL; the
//     connection is verified with a PING before returning
//
// An unknown driver returns an error.
func New(ctx context.Context, cfg Config) (Cache, error) {
	switch cfg.Driver {
	case "", "memory":
		return NewMemory(), nil
	case "noop":
		return NewNoop(), nil
	case "redis":
		opt, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			return nil, fmt.Errorf("cache: parse REDIS_URL: %w", err)
		}
		client := redis.NewClient(opt)
		if err := client.Ping(ctx).Err(); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("cache: ping redis: %w", err)
		}
		return NewRedis(client, cfg.KeyPrefix), nil
	default:
		return nil, fmt.Errorf("cache: unknown driver %q", cfg.Driver)
	}
}
