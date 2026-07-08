package cache

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestMemoryGetSetMiss(t *testing.T) {
	ctx := context.Background()
	c := NewMemory()

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

func TestMemoryTTLExpiry(t *testing.T) {
	ctx := context.Background()
	c := NewMemory()

	if err := c.Set(ctx, "k", []byte("v"), 1*time.Millisecond); err != nil {
		t.Fatalf("set: %v", err)
	}
	time.Sleep(5 * time.Millisecond)

	if _, ok, err := c.Get(ctx, "k"); err != nil || ok {
		t.Fatalf("expected expired miss: ok=%v err=%v", ok, err)
	}
	// Expired entry should have been deleted lazily.
	c.mu.Lock()
	_, present := c.items["k"]
	c.mu.Unlock()
	if present {
		t.Fatal("expired entry was not deleted on Get")
	}
}

func TestMemoryDeleteByPrefix(t *testing.T) {
	ctx := context.Background()
	c := NewMemory()
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

func TestMemoryDelete(t *testing.T) {
	ctx := context.Background()
	c := NewMemory()
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

func TestMemoryClear(t *testing.T) {
	ctx := context.Background()
	c := NewMemory()
	_ = c.Set(ctx, "a", []byte("1"), 0)
	_ = c.Set(ctx, "b", []byte("2"), 0)

	if err := c.Clear(ctx); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, ok, _ := c.Get(ctx, "a"); ok {
		t.Fatal("a should be cleared")
	}
	if _, ok, _ := c.Get(ctx, "b"); ok {
		t.Fatal("b should be cleared")
	}
}

func TestMemoryGetReturnsCopy(t *testing.T) {
	ctx := context.Background()
	c := NewMemory()
	_ = c.Set(ctx, "k", []byte("orig"), 0)

	got, _, _ := c.Get(ctx, "k")
	got[0] = 'X'

	again, _, _ := c.Get(ctx, "k")
	if string(again) != "orig" {
		t.Fatalf("stored bytes were mutated by caller: %q", again)
	}
}

func TestMemoryConcurrent(t *testing.T) {
	ctx := context.Background()
	c := NewMemory()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				key := fmt.Sprintf("k:%d:%d", n, j%8)
				_ = c.Set(ctx, key, []byte("v"), time.Duration(j%3)*time.Millisecond)
				_, _, _ = c.Get(ctx, key)
				if j%5 == 0 {
					_ = c.DeleteByPrefix(ctx, fmt.Sprintf("k:%d:", n))
				}
			}
		}(i)
	}
	wg.Wait()
}
