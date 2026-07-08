package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestRedis starts a miniredis instance and returns a *Redis wired to it.
// It skips the test gracefully if miniredis cannot start.
func newTestRedis(t *testing.T) (*Redis, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Skipf("miniredis unavailable: %v", err)
	}
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedis(client, "cmstack:"), mr
}

func TestRedisGetSetMiss(t *testing.T) {
	ctx := context.Background()
	c, _ := newTestRedis(t)

	if _, ok, err := c.Get(ctx, "missing"); err != nil || ok {
		t.Fatalf("miss: ok=%v err=%v", ok, err)
	}
	if err := c.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, ok, err := c.Get(ctx, "k")
	if err != nil || !ok || string(got) != "v" {
		t.Fatalf("get: got=%q ok=%v err=%v", got, ok, err)
	}
}

func TestRedisKeyPrefix(t *testing.T) {
	ctx := context.Background()
	c, mr := newTestRedis(t)

	if err := c.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatalf("set: %v", err)
	}
	if !mr.Exists("cmstack:k") {
		t.Fatal("expected key to be stored namespaced as cmstack:k")
	}
}

func TestRedisTTLExpiry(t *testing.T) {
	ctx := context.Background()
	c, mr := newTestRedis(t)

	if err := c.Set(ctx, "k", []byte("v"), 50*time.Millisecond); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, ok, _ := c.Get(ctx, "k"); !ok {
		t.Fatal("expected hit before expiry")
	}
	mr.FastForward(100 * time.Millisecond)
	if _, ok, _ := c.Get(ctx, "k"); ok {
		t.Fatal("expected miss after FastForward past ttl")
	}
}

func TestRedisDeleteByPrefix(t *testing.T) {
	ctx := context.Background()
	c, _ := newTestRedis(t)
	_ = c.Set(ctx, "page:1", []byte("a"), 0)
	_ = c.Set(ctx, "page:2", []byte("b"), 0)
	_ = c.Set(ctx, "user:1", []byte("c"), 0)

	if err := c.DeleteByPrefix(ctx, "page:"); err != nil {
		t.Fatalf("prefix: %v", err)
	}
	if _, ok, _ := c.Get(ctx, "page:1"); ok {
		t.Fatal("page:1 should be gone")
	}
	if _, ok, _ := c.Get(ctx, "page:2"); ok {
		t.Fatal("page:2 should be gone")
	}
	if _, ok, _ := c.Get(ctx, "user:1"); !ok {
		t.Fatal("user:1 should remain")
	}
}

func TestRedisDelete(t *testing.T) {
	ctx := context.Background()
	c, _ := newTestRedis(t)
	_ = c.Set(ctx, "a", []byte("1"), 0)
	_ = c.Set(ctx, "b", []byte("2"), 0)

	if err := c.Delete(ctx, "a", "missing"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, _ := c.Get(ctx, "a"); ok {
		t.Fatal("a should be gone")
	}
	if _, ok, _ := c.Get(ctx, "b"); !ok {
		t.Fatal("b should remain")
	}
	if err := c.Delete(ctx); err != nil {
		t.Fatalf("empty delete: %v", err)
	}
}

func TestRedisClearScopedToPrefix(t *testing.T) {
	ctx := context.Background()
	c, mr := newTestRedis(t)
	_ = c.Set(ctx, "a", []byte("1"), 0)
	_ = c.Set(ctx, "b", []byte("2"), 0)
	// A foreign key outside the app namespace must survive Clear.
	if err := mr.Set("other:x", "keep"); err != nil {
		t.Fatalf("seed foreign key: %v", err)
	}

	if err := c.Clear(ctx); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, ok, _ := c.Get(ctx, "a"); ok {
		t.Fatal("a should be cleared")
	}
	if !mr.Exists("other:x") {
		t.Fatal("Clear must not touch keys outside the app prefix")
	}
}
