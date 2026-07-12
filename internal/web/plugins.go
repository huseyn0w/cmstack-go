package web

import (
	"context"

	"github.com/huseyn0w/agentic-cms-go/internal/plugin"
	"github.com/huseyn0w/agentic-cms-go/internal/settings"
)

// pluginRegionSource adapts a *plugin.Manager to the templ package's
// pluginSource interface, so the public layout can inject enabled plugins'
// render-region fragments without importing the plugin package (which would
// cycle). It mirrors localeViewSource / themeViewSource. Router registers it via
// webtempl.SetPluginSource when a manager is wired.
type pluginRegionSource struct{ mgr *plugin.Manager }

// RenderRegion returns the trusted HTML fragments contributed by enabled plugins
// for the named region, in registration order.
func (s pluginRegionSource) RenderRegion(ctx context.Context, name string) []string {
	return s.mgr.RenderRegion(ctx, name)
}

// pluginEnabledPrefix namespaces per-plugin enabled state in the settings store:
// the key "plugin:<id>" holds "1" (on) or "0" (off). An absent key means no
// override has been persisted, so the manager falls back to the plugin's
// Meta.DefaultEnabled.
const pluginEnabledPrefix = "plugin:"

// SettingsEnabledStore is the settings-backed adapter satisfying
// plugin.EnabledStore. It persists per-plugin enabled state under the
// "plugin:<id>" settings key, reusing the M9 settings store (cached,
// clear-on-write) — no new table or migration. It lives in the web layer so the
// plugin package stays decoupled from settings.
type SettingsEnabledStore struct{ svc *settings.Service }

// NewSettingsEnabledStore wraps a *settings.Service as a plugin.EnabledStore.
func NewSettingsEnabledStore(svc *settings.Service) SettingsEnabledStore {
	return SettingsEnabledStore{svc: svc}
}

// Enabled reports plugin id's persisted enabled state. found is false when no
// value has been written (or on a store error), so the manager falls back to the
// plugin's DefaultEnabled.
func (s SettingsEnabledStore) Enabled(ctx context.Context, id string) (on bool, found bool) {
	v, ok, err := s.svc.Get(ctx, pluginEnabledPrefix+id)
	if err != nil || !ok {
		return false, false
	}
	return v == "1", true
}

// SetEnabled persists plugin id's enabled state as "1"/"0".
func (s SettingsEnabledStore) SetEnabled(ctx context.Context, id string, on bool) error {
	val := "0"
	if on {
		val = "1"
	}
	return s.svc.Set(ctx, pluginEnabledPrefix+id, val)
}
