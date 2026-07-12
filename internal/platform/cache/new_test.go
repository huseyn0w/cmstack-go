package cache

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

func TestNewDriverSelection(t *testing.T) {
	ctx := context.Background()

	mr, err := miniredis.Run()
	if err != nil {
		t.Skipf("miniredis unavailable: %v", err)
	}
	t.Cleanup(mr.Close)
	redisURL := "redis://" + mr.Addr() + "/0"

	tests := []struct {
		name    string
		cfg     Config
		want    string // %T of the concrete type
		wantErr bool
	}{
		{"empty defaults to memory", Config{Driver: ""}, "*cache.Memory", false},
		{"memory", Config{Driver: "memory"}, "*cache.Memory", false},
		{"noop", Config{Driver: "noop"}, "*cache.Noop", false},
		{"redis", Config{Driver: "redis", RedisURL: redisURL, KeyPrefix: "agentic-cms:"}, "*cache.Redis", false},
		{"unknown", Config{Driver: "bogus"}, "", true},
		{"redis bad url", Config{Driver: "redis", RedisURL: "not-a-url"}, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New(ctx, tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %T", c)
				}
				return
			}
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if got := typeName(c); got != tt.want {
				t.Fatalf("type = %s, want %s", got, tt.want)
			}
		})
	}
}

func typeName(c Cache) string {
	switch c.(type) {
	case *Memory:
		return "*cache.Memory"
	case *Noop:
		return "*cache.Noop"
	case *Redis:
		return "*cache.Redis"
	default:
		return "unknown"
	}
}
