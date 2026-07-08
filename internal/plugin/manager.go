package plugin

import (
	"context"
	"log/slog"
)

// registered pairs a plugin's Meta with its captured Hooks. Both are immutable
// after NewManager, so dispatch reads them without locking.
type registered struct {
	meta  Meta
	hooks Hooks
}

// Manager is the plugin registry and dispatcher. It captures each plugin's
// static registrations once at construction and dispatches actions/filters/
// regions to the ENABLED subset per request. Enabled state is read through the
// injected EnabledStore (which owns its own caching/locking). The catalogue and
// hooks are immutable after construction, so DoAction/ApplyFilter/RenderRegion
// are safe for concurrent requests.
type Manager struct {
	store  EnabledStore
	plugs  []registered
	byID   map[string]struct{}
	logger *slog.Logger
}

// NewManager builds a Manager over store, registering each plugin's hooks once
// in the given order. A nil logger is tolerated (panics recovered during
// dispatch are then silently contained rather than logged).
func NewManager(store EnabledStore, plugins ...Plugin) *Manager {
	m := &Manager{
		store: store,
		plugs: make([]registered, 0, len(plugins)),
		byID:  make(map[string]struct{}, len(plugins)),
	}
	for _, p := range plugins {
		if p == nil {
			continue
		}
		var h Hooks
		p.Register(&h)
		meta := p.Meta()
		m.plugs = append(m.plugs, registered{meta: meta, hooks: h})
		m.byID[meta.ID] = struct{}{}
	}
	return m
}

// WithLogger returns m with logger attached for recovered-panic diagnostics.
func (m *Manager) WithLogger(logger *slog.Logger) *Manager {
	m.logger = logger
	return m
}

// Catalogue returns every registered plugin's Meta in registration order.
func (m *Manager) Catalogue() []Meta {
	out := make([]Meta, 0, len(m.plugs))
	for _, p := range m.plugs {
		out = append(out, p.meta)
	}
	return out
}

// IsEnabled reports whether plugin id is active for the request. It consults the
// store; when no explicit value is persisted it falls back to the plugin's
// Meta.DefaultEnabled. An unknown id reports false.
func (m *Manager) IsEnabled(ctx context.Context, id string) bool {
	if m.store != nil {
		if on, found := m.store.Enabled(ctx, id); found {
			return on
		}
	}
	for _, p := range m.plugs {
		if p.meta.ID == id {
			return p.meta.DefaultEnabled
		}
	}
	return false
}

// SetEnabled validates id against the catalogue and persists its enabled state.
// It returns ErrUnknownPlugin for an id not in the catalogue.
func (m *Manager) SetEnabled(ctx context.Context, id string, on bool) error {
	if _, ok := m.byID[id]; !ok {
		return ErrUnknownPlugin
	}
	if m.store == nil {
		return ErrNoStore
	}
	return m.store.SetEnabled(ctx, id, on)
}

// DoAction invokes every ENABLED plugin's action callbacks for name, in
// registration order. Each callback is wrapped in a recover so a panicking
// plugin is contained (logged and skipped) rather than crashing the request.
func (m *Manager) DoAction(ctx context.Context, name string, payload any) {
	for _, p := range m.plugs {
		fns := p.hooks.actions[name]
		if len(fns) == 0 || !m.IsEnabled(ctx, p.meta.ID) {
			continue
		}
		for _, fn := range fns {
			m.safeAction(ctx, p.meta.ID, name, fn, payload)
		}
	}
}

// ApplyFilter threads value through every ENABLED plugin's filter callbacks for
// name, in registration order; each callback receives the previous result. A
// panicking callback is skipped and the pre-callback value is preserved.
func (m *Manager) ApplyFilter(ctx context.Context, name string, value any) any {
	for _, p := range m.plugs {
		fns := p.hooks.filters[name]
		if len(fns) == 0 || !m.IsEnabled(ctx, p.meta.ID) {
			continue
		}
		for _, fn := range fns {
			value = m.safeFilter(ctx, p.meta.ID, name, fn, value)
		}
	}
	return value
}

// RenderRegion collects the non-empty fragments produced by every ENABLED
// plugin's region callbacks for name, in registration order. A panicking
// callback contributes nothing.
func (m *Manager) RenderRegion(ctx context.Context, name string) []string {
	var out []string
	for _, p := range m.plugs {
		fns := p.hooks.regions[name]
		if len(fns) == 0 || !m.IsEnabled(ctx, p.meta.ID) {
			continue
		}
		for _, fn := range fns {
			if frag := m.safeRegion(ctx, p.meta.ID, name, fn); frag != "" {
				out = append(out, frag)
			}
		}
	}
	return out
}

func (m *Manager) safeAction(ctx context.Context, id, name string, fn ActionFunc, payload any) {
	defer m.recoverHook(id, name)
	fn(ctx, payload)
}

func (m *Manager) safeFilter(ctx context.Context, id, name string, fn FilterFunc, value any) (out any) {
	// Preserve the pre-callback value on panic.
	out = value
	defer m.recoverHook(id, name)
	out = fn(ctx, value)
	return out
}

func (m *Manager) safeRegion(ctx context.Context, id, name string, fn RegionFunc) (frag string) {
	defer m.recoverHook(id, name)
	frag = fn(ctx)
	return frag
}

// recoverHook contains a panicking plugin callback: it recovers, logs when a
// logger is attached, and lets dispatch continue with the next callback.
func (m *Manager) recoverHook(id, name string) {
	if rec := recover(); rec != nil && m.logger != nil {
		m.logger.Error(
			"plugin hook panicked",
			slog.String("plugin", id),
			slog.String("hook", name),
			slog.Any("recovered", rec),
		)
	}
}
