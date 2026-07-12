package kernel_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
)

func TestSlugify(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Hello, World!", "hello-world"},
		{"  Trim  Me  ", "trim-me"},
		{"Café Déjà Vu", "cafe-deja-vu"},
		{"multiple---hyphens", "multiple-hyphens"},
		{"UPPER Case", "upper-case"},
		{"Numbers 123 ok", "numbers-123-ok"},
		{"!!!", "untitled"},
		{"", "untitled"},
		{"emoji 🎉 here", "emoji-here"},
		{"trailing-punct...", "trailing-punct"},
	}
	for _, c := range cases {
		if got := kernel.Slugify(c.in); got != c.want {
			t.Errorf("Slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSlugifyTruncates(t *testing.T) {
	long := strings.Repeat("a", 500)
	got := kernel.Slugify(long)
	if len(got) > 200 {
		t.Errorf("slug length = %d, want <= 200", len(got))
	}
}

func TestUniqueSlug_FreeReturnsAsIs(t *testing.T) {
	taken := func(_ context.Context, _ string) (bool, error) { return false, nil }
	got, err := kernel.UniqueSlug(context.Background(), "my-post", taken)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "my-post" {
		t.Errorf("got %q, want my-post", got)
	}
}

func TestUniqueSlug_AppendsSuffix(t *testing.T) {
	// "my-post" and "my-post-2" are taken; "my-post-3" is free.
	used := map[string]bool{"my-post": true, "my-post-2": true}
	taken := func(_ context.Context, s string) (bool, error) { return used[s], nil }
	got, err := kernel.UniqueSlug(context.Background(), "my-post", taken)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "my-post-3" {
		t.Errorf("got %q, want my-post-3", got)
	}
}

func TestUniqueSlug_PropagatesError(t *testing.T) {
	boom := errors.New("db down")
	taken := func(_ context.Context, _ string) (bool, error) { return false, boom }
	if _, err := kernel.UniqueSlug(context.Background(), "x", taken); !errors.Is(err, boom) {
		t.Fatalf("got %v, want %v", err, boom)
	}
}
