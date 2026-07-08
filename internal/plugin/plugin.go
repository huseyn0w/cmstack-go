// Package plugin provides an in-process hook registry for first-party
// extensions: actions (fire-and-forget side effects), filters (value
// transformers threaded in order), and render-regions (trusted HTML fragments
// injected into the public layout). Plugins are Go code compiled into the
// binary; their per-plugin enabled state is dynamic (persisted via an
// EnabledStore seam) while their registrations are static (captured once at
// construction). Dispatch is read-only over the immutable registry, so it is
// safe for concurrent requests without locks.
package plugin

import "context"

// Meta describes a plugin for the catalogue and admin manager. ID is the stable
// identifier used as the settings key and enable/disable handle.
type Meta struct {
	// ID is the stable, unique plugin identifier (e.g. "reading-time").
	ID string
	// Name is the human-facing display name.
	Name string
	// Description is a short summary of what the plugin does.
	Description string
	// DefaultEnabled is the enabled state used when no explicit override has
	// been persisted for the plugin.
	DefaultEnabled bool
}

// ActionFunc is a fire-and-forget hook callback. It receives an arbitrary
// payload and performs side effects; its return value (if any) is ignored.
type ActionFunc func(ctx context.Context, payload any)

// FilterFunc is a value-transforming hook callback. It receives the current
// value and returns a (possibly modified) value; the manager threads the result
// into the next filter for the same hook name.
type FilterFunc func(ctx context.Context, value any) any

// RegionFunc produces a trusted HTML fragment for a named render-region. An
// empty string contributes nothing to the region.
type RegionFunc func(ctx context.Context) string

// Hooks accumulates a single plugin's registrations, keyed by hook name. A
// plugin populates it inside Register; the manager captures the populated Hooks
// once and never mutates it afterwards.
type Hooks struct {
	actions map[string][]ActionFunc
	filters map[string][]FilterFunc
	regions map[string][]RegionFunc
}

// AddAction registers an action callback under name.
func (h *Hooks) AddAction(name string, fn ActionFunc) {
	if fn == nil {
		return
	}
	if h.actions == nil {
		h.actions = make(map[string][]ActionFunc)
	}
	h.actions[name] = append(h.actions[name], fn)
}

// AddFilter registers a filter callback under name.
func (h *Hooks) AddFilter(name string, fn FilterFunc) {
	if fn == nil {
		return
	}
	if h.filters == nil {
		h.filters = make(map[string][]FilterFunc)
	}
	h.filters[name] = append(h.filters[name], fn)
}

// AddRegion registers a render-region callback under name.
func (h *Hooks) AddRegion(name string, fn RegionFunc) {
	if fn == nil {
		return
	}
	if h.regions == nil {
		h.regions = make(map[string][]RegionFunc)
	}
	h.regions[name] = append(h.regions[name], fn)
}

// Plugin is the interface every first-party extension implements. Meta is read
// once for the catalogue; Register is called once at manager construction to
// capture the plugin's hook registrations.
type Plugin interface {
	Meta() Meta
	Register(h *Hooks)
}

// EnabledStore is the persistence seam for per-plugin enabled state. An adapter
// over the settings store lives in the web/server layer so this package stays
// decoupled from settings.
type EnabledStore interface {
	// Enabled reports whether plugin id is on. found is false when no explicit
	// value has been persisted, letting the manager fall back to DefaultEnabled.
	Enabled(ctx context.Context, id string) (on bool, found bool)
	// SetEnabled persists the enabled state for plugin id.
	SetEnabled(ctx context.Context, id string, on bool) error
}
