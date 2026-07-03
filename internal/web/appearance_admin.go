package web

import (
	"context"
	"net/http"

	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	"github.com/huseyn0w/cmstack-go/internal/theme"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// AppearanceSettings is the narrow settings dependency the appearance switcher
// needs: read the active theme id and persist a new one. *settings.Service
// satisfies it. Declaring it here keeps web decoupled from the settings package
// and trivially fakeable in tests.
type AppearanceSettings interface {
	ActiveTheme(ctx context.Context) string
	SetActiveTheme(ctx context.Context, id string) error
}

// AppearanceHandler is the thin HTTP boundary for the admin appearance area: it
// lists the registered themes and activates one. It touches no data directly —
// the theme catalogue is the in-code registry and persistence is the settings
// service.
type AppearanceHandler struct {
	svc   AppearanceSettings
	shell adminShellDeps
	csrf  func(*http.Request) string
}

// NewAppearanceHandler constructs the admin appearance handler.
func NewAppearanceHandler(svc AppearanceSettings, shell adminShellDeps, csrf func(*http.Request) string) *AppearanceHandler {
	return &AppearanceHandler{svc: svc, shell: shell, csrf: csrf}
}

// Show renders the theme switcher: every registered theme with a live palette
// preview, the active one badged. The active id is resolved through the registry
// so an unset/stale stored value shows the default as active.
func (h *AppearanceHandler) Show(w http.ResponseWriter, r *http.Request) {
	active := theme.Resolve(h.svc.ActiveTheme(r.Context())).ID

	all := theme.All()
	choices := make([]webtempl.ThemeChoice, 0, len(all))
	for _, t := range all {
		choices = append(choices, webtempl.ThemeChoice{
			ID:          t.ID,
			Label:       t.Label,
			Description: t.Description,
			Active:      t.ID == active,
		})
	}

	view := webtempl.AppearanceView{
		Shell:       h.shell.buildShell(r, "Appearance"),
		Themes:      choices,
		ActivateURL: "/admin/appearance/activate",
		CSRFToken:   h.csrf(r),
	}
	if err := render.Component(r.Context(), w, http.StatusOK, webtempl.AppearancePage(view)); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// Activate persists the submitted theme id as the active site theme, then
// redirects back to the switcher. An unknown id is rejected (the registry is the
// allow-list) so the stored value is always a real theme.
func (h *AppearanceHandler) Activate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id := r.PostFormValue("theme")
	if !theme.IsValid(id) {
		http.Redirect(w, r, "/admin/appearance", http.StatusSeeOther)
		return
	}
	if err := h.svc.SetActiveTheme(r.Context(), id); err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/appearance", http.StatusSeeOther)
}
