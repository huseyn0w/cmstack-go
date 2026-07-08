package plugin

import (
	"context"
	"errors"
	"testing"
)

// fakeStore is an in-memory EnabledStore for tests. A missing key reports
// found=false so the manager falls back to DefaultEnabled.
type fakeStore struct {
	state map[string]bool
}

func newFakeStore() *fakeStore { return &fakeStore{state: map[string]bool{}} }

func (s *fakeStore) Enabled(_ context.Context, id string) (bool, bool) {
	on, ok := s.state[id]
	return on, ok
}

func (s *fakeStore) SetEnabled(_ context.Context, id string, on bool) error {
	s.state[id] = on
	return nil
}

// recorder captures action fires and filter/region calls for assertions.
type recorder struct {
	actions []string
}

// fakePlugin is a configurable test plugin.
type fakePlugin struct {
	meta     Meta
	register func(h *Hooks)
}

func (p fakePlugin) Meta() Meta        { return p.meta }
func (p fakePlugin) Register(h *Hooks) { p.register(h) }

func TestDoActionOnlyFiresEnabledInOrder(t *testing.T) {
	rec := &recorder{}
	store := newFakeStore()

	pA := fakePlugin{
		meta: Meta{ID: "a", DefaultEnabled: true},
		register: func(h *Hooks) {
			h.AddAction("ping", func(_ context.Context, _ any) { rec.actions = append(rec.actions, "a") })
		},
	}
	pB := fakePlugin{
		meta: Meta{ID: "b", DefaultEnabled: true},
		register: func(h *Hooks) {
			h.AddAction("ping", func(_ context.Context, _ any) { rec.actions = append(rec.actions, "b") })
		},
	}
	m := NewManager(store, pA, pB)

	// Disable b: only a should fire.
	if err := m.SetEnabled(context.Background(), "b", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	m.DoAction(context.Background(), "ping", nil)
	if len(rec.actions) != 1 || rec.actions[0] != "a" {
		t.Fatalf("expected only [a], got %v", rec.actions)
	}

	// Re-enable b: both fire in registration order.
	rec.actions = nil
	if err := m.SetEnabled(context.Background(), "b", true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	m.DoAction(context.Background(), "ping", nil)
	if len(rec.actions) != 2 || rec.actions[0] != "a" || rec.actions[1] != "b" {
		t.Fatalf("expected [a b], got %v", rec.actions)
	}
}

func TestApplyFilterThreadsValueInOrderAndSkipsDisabled(t *testing.T) {
	store := newFakeStore()
	pA := fakePlugin{
		meta: Meta{ID: "a", DefaultEnabled: true},
		register: func(h *Hooks) {
			h.AddFilter("f", func(_ context.Context, v any) any { return v.(string) + "A" })
		},
	}
	pB := fakePlugin{
		meta: Meta{ID: "b", DefaultEnabled: true},
		register: func(h *Hooks) {
			h.AddFilter("f", func(_ context.Context, v any) any { return v.(string) + "B" })
		},
	}
	m := NewManager(store, pA, pB)

	got := m.ApplyFilter(context.Background(), "f", "x").(string)
	if got != "xAB" {
		t.Fatalf("expected threaded xAB, got %q", got)
	}

	// Disable a: only b applies.
	if err := m.SetEnabled(context.Background(), "a", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	got = m.ApplyFilter(context.Background(), "f", "x").(string)
	if got != "xB" {
		t.Fatalf("expected xB after disabling a, got %q", got)
	}
}

func TestRenderRegionCollectsEnabledFragments(t *testing.T) {
	store := newFakeStore()
	pA := fakePlugin{
		meta: Meta{ID: "a", DefaultEnabled: true},
		register: func(h *Hooks) {
			h.AddRegion("head", func(_ context.Context) string { return "<a>" })
		},
	}
	pB := fakePlugin{
		meta: Meta{ID: "b", DefaultEnabled: true},
		register: func(h *Hooks) {
			h.AddRegion("head", func(_ context.Context) string { return "" }) // empty contributes nothing
		},
	}
	pC := fakePlugin{
		meta: Meta{ID: "c", DefaultEnabled: true},
		register: func(h *Hooks) {
			h.AddRegion("head", func(_ context.Context) string { return "<c>" })
		},
	}
	m := NewManager(store, pA, pB, pC)

	frags := m.RenderRegion(context.Background(), "head")
	if len(frags) != 2 || frags[0] != "<a>" || frags[1] != "<c>" {
		t.Fatalf("expected [<a> <c>], got %v", frags)
	}

	// Disable c: only a's fragment remains.
	if err := m.SetEnabled(context.Background(), "c", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	frags = m.RenderRegion(context.Background(), "head")
	if len(frags) != 1 || frags[0] != "<a>" {
		t.Fatalf("expected [<a>], got %v", frags)
	}
}

func TestIsEnabledFallsBackToDefault(t *testing.T) {
	store := newFakeStore()
	on := fakePlugin{meta: Meta{ID: "on", DefaultEnabled: true}, register: func(*Hooks) {}}
	off := fakePlugin{meta: Meta{ID: "off", DefaultEnabled: false}, register: func(*Hooks) {}}
	m := NewManager(store, on, off)

	if !m.IsEnabled(context.Background(), "on") {
		t.Fatal("expected 'on' to default enabled")
	}
	if m.IsEnabled(context.Background(), "off") {
		t.Fatal("expected 'off' to default disabled")
	}
	// Explicit override wins over default.
	if err := m.SetEnabled(context.Background(), "off", true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if !m.IsEnabled(context.Background(), "off") {
		t.Fatal("expected 'off' enabled after override")
	}
	// Unknown id is never enabled.
	if m.IsEnabled(context.Background(), "nope") {
		t.Fatal("unknown id should not be enabled")
	}
}

func TestSetEnabledRejectsUnknownID(t *testing.T) {
	m := NewManager(newFakeStore(), fakePlugin{meta: Meta{ID: "a"}, register: func(*Hooks) {}})
	if err := m.SetEnabled(context.Background(), "ghost", true); !errors.Is(err, ErrUnknownPlugin) {
		t.Fatalf("expected ErrUnknownPlugin, got %v", err)
	}
}

func TestCatalogueOrder(t *testing.T) {
	m := NewManager(
		newFakeStore(),
		fakePlugin{meta: Meta{ID: "first"}, register: func(*Hooks) {}},
		fakePlugin{meta: Meta{ID: "second"}, register: func(*Hooks) {}},
	)
	cat := m.Catalogue()
	if len(cat) != 2 || cat[0].ID != "first" || cat[1].ID != "second" {
		t.Fatalf("catalogue out of order: %v", cat)
	}
}

func TestPanickingCallbackIsContained(t *testing.T) {
	store := newFakeStore()
	bad := fakePlugin{
		meta: Meta{ID: "bad", DefaultEnabled: true},
		register: func(h *Hooks) {
			h.AddAction("boom", func(_ context.Context, _ any) { panic("kaboom") })
			h.AddFilter("f", func(_ context.Context, _ any) any { panic("kaboom") })
			h.AddRegion("r", func(_ context.Context) string { panic("kaboom") })
		},
	}
	good := fakePlugin{
		meta: Meta{ID: "good", DefaultEnabled: true},
		register: func(h *Hooks) {
			h.AddFilter("f", func(_ context.Context, v any) any { return v.(string) + "G" })
		},
	}
	m := NewManager(store, bad, good)

	// DoAction must not propagate the panic.
	m.DoAction(context.Background(), "boom", nil)

	// ApplyFilter: bad panics (value preserved), good still applies.
	got := m.ApplyFilter(context.Background(), "f", "x").(string)
	if got != "xG" {
		t.Fatalf("expected panicking filter skipped, value threaded to good: got %q", got)
	}

	// RenderRegion: panicking region contributes nothing, dispatch returns.
	frags := m.RenderRegion(context.Background(), "r")
	if len(frags) != 0 {
		t.Fatalf("expected no fragments from panicking region, got %v", frags)
	}
}
