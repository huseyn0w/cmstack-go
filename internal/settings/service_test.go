package settings_test

import (
	"context"
	"testing"

	"github.com/huseyn0w/agentic-cms-go/internal/settings"
)

// fakeRepo is an in-memory settings.Repo that counts All() loads so the test can
// assert the service's cache behavior (load-once + invalidate-on-write).
type fakeRepo struct {
	data     map[string]string
	allCalls int
}

func newFakeRepo() *fakeRepo { return &fakeRepo{data: map[string]string{}} }

func (r *fakeRepo) Get(_ context.Context, key string) (string, error) {
	v, ok := r.data[key]
	if !ok {
		return "", settings.ErrNotFound
	}
	return v, nil
}

func (r *fakeRepo) Set(_ context.Context, key, value string) error {
	r.data[key] = value
	return nil
}

func (r *fakeRepo) All(_ context.Context) (map[string]string, error) {
	r.allCalls++
	out := make(map[string]string, len(r.data))
	for k, v := range r.data {
		out[k] = v
	}
	return out, nil
}

// TestGetSetRoundTrip asserts a Set is visible to a subsequent Get, and an unset
// key reports found=false without error.
func TestGetSetRoundTrip(t *testing.T) {
	svc := settings.NewService(newFakeRepo())
	ctx := context.Background()

	if _, ok, err := svc.Get(ctx, "missing"); err != nil || ok {
		t.Fatalf("Get(missing) = (_, %v, %v), want (_, false, nil)", ok, err)
	}

	if err := svc.Set(ctx, "active_theme", "sepia"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, ok, err := svc.Get(ctx, "active_theme")
	if err != nil || !ok || v != "sepia" {
		t.Fatalf("Get(active_theme) = (%q, %v, %v), want (sepia, true, nil)", v, ok, err)
	}
}

// TestCacheInvalidatesOnSet asserts hot reads are cached (repo.All loaded once)
// and that a Set busts the cache so the next read reflects the new value.
func TestCacheInvalidatesOnSet(t *testing.T) {
	repo := newFakeRepo()
	repo.data["active_theme"] = "sepia"
	svc := settings.NewService(repo)
	ctx := context.Background()

	// Two reads should hit the repo only once (cache load-once).
	for i := 0; i < 3; i++ {
		if got := svc.ActiveTheme(ctx); got != "sepia" {
			t.Fatalf("ActiveTheme = %q, want sepia", got)
		}
	}
	if repo.allCalls != 1 {
		t.Fatalf("repo.All called %d times, want 1 (cache should serve hot reads)", repo.allCalls)
	}

	// A write invalidates the cache; the next read reloads and sees the change.
	if err := svc.SetActiveTheme(ctx, "noir"); err != nil {
		t.Fatalf("SetActiveTheme: %v", err)
	}
	if got := svc.ActiveTheme(ctx); got != "noir" {
		t.Fatalf("after Set, ActiveTheme = %q, want noir", got)
	}
	if repo.allCalls != 2 {
		t.Fatalf("repo.All called %d times, want 2 (one reload after invalidation)", repo.allCalls)
	}
}

// TestActiveThemeUnset asserts an unset active_theme yields "" (the web layer
// then defaults via the registry).
func TestActiveThemeUnset(t *testing.T) {
	svc := settings.NewService(newFakeRepo())
	if got := svc.ActiveTheme(context.Background()); got != "" {
		t.Fatalf("ActiveTheme (unset) = %q, want empty", got)
	}
}

// errRepo always fails, so ActiveTheme must degrade to "" rather than surface an
// error on every public page.
type errRepo struct{}

func (errRepo) Get(context.Context, string) (string, error) {
	return "", context.DeadlineExceeded
}
func (errRepo) Set(context.Context, string, string) error { return context.DeadlineExceeded }
func (errRepo) All(context.Context) (map[string]string, error) {
	return nil, context.DeadlineExceeded
}

// TestActiveThemeErrorDegrades asserts a store error resolves to "" (graceful
// fallback to the base palette).
func TestActiveThemeErrorDegrades(t *testing.T) {
	svc := settings.NewService(errRepo{})
	if got := svc.ActiveTheme(context.Background()); got != "" {
		t.Fatalf("ActiveTheme (errored repo) = %q, want empty", got)
	}
}
