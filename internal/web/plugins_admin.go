package web

import (
	"context"
	"net/http"

	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	"github.com/huseyn0w/cmstack-go/internal/plugin"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// PluginCatalogue is the narrow plugin-manager surface the admin handler needs:
// list the registered plugins, report each one's enabled state, and toggle it.
// *plugin.Manager satisfies it.
type PluginCatalogue interface {
	Catalogue() []plugin.Meta
	IsEnabled(ctx context.Context, id string) bool
	SetEnabled(ctx context.Context, id string, on bool) error
}

// PluginAdminHandler is the thin HTTP boundary for the admin plugin manager. The
// catalogue is the in-code plugin registry; enabled state is persisted by the
// manager (settings-backed) — the handler owns no data access.
type PluginAdminHandler struct {
	mgr   PluginCatalogue
	shell adminShellDeps
	csrf  func(*http.Request) string
}

// NewPluginAdminHandler constructs the admin plugin manager handler.
func NewPluginAdminHandler(mgr PluginCatalogue, shell adminShellDeps, csrf func(*http.Request) string) *PluginAdminHandler {
	return &PluginAdminHandler{mgr: mgr, shell: shell, csrf: csrf}
}

// Show renders the plugin manager: every registered plugin with its current
// enabled state and a toggle action.
func (h *PluginAdminHandler) Show(w http.ResponseWriter, r *http.Request) {
	cat := h.mgr.Catalogue()
	rows := make([]webtempl.PluginRow, 0, len(cat))
	for _, m := range cat {
		rows = append(rows, webtempl.PluginRow{
			ID:          m.ID,
			Name:        m.Name,
			Description: m.Description,
			Enabled:     h.mgr.IsEnabled(r.Context(), m.ID),
		})
	}

	view := webtempl.PluginsView{
		Shell:     h.shell.buildShell(r, "Plugins"),
		Plugins:   rows,
		ToggleURL: "/admin/plugins/toggle",
		CSRFToken: h.csrf(r),
	}
	if err := render.Component(r.Context(), w, http.StatusOK, webtempl.PluginsPage(view)); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// Toggle enables or disables the submitted plugin, then redirects back to the
// manager. The desired state is the `enable` field ("1" enables). An unknown
// plugin id is rejected by the manager and surfaces as a redirect (no change).
func (h *PluginAdminHandler) Toggle(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id := r.PostFormValue("plugin")
	on := r.PostFormValue("enable") == "1"
	if err := h.mgr.SetEnabled(r.Context(), id, on); err != nil {
		// Unknown id / no store: nothing to change — fall through to the redirect
		// so the manager simply re-renders the current state.
		http.Redirect(w, r, "/admin/plugins", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/plugins", http.StatusSeeOther)
}
