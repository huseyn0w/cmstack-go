package api

import (
	"net/http"

	"github.com/huseyn0w/agentic-cms-go/internal/theme"
)

// themeDTO is the stable JSON shape of the active-theme setting: the active id
// plus the registered ids a client may switch to.
type themeDTO struct {
	ActiveTheme string   `json:"activeTheme"`
	Available   []string `json:"available"`
}

// availableThemes returns every registered theme id in display order (default
// first) for the themeDTO.available list.
func availableThemes() []string {
	all := theme.All()
	out := make([]string, 0, len(all))
	for _, t := range all {
		out = append(out, t.ID)
	}
	return out
}

// getTheme serves GET /api/v1/settings/theme: the active theme id + the
// available ids.
func (h *handler) getTheme(w http.ResponseWriter, r *http.Request) {
	active := h.settings.ActiveTheme(r.Context())
	if active == "" {
		active = theme.Default().ID
	}
	OK(w, http.StatusOK, themeDTO{ActiveTheme: active, Available: availableThemes()})
}

// updateThemeRequest is the JSON body for PUT /api/v1/settings/theme.
type updateThemeRequest struct {
	Theme string `json:"theme"`
}

// updateTheme serves PUT /api/v1/settings/theme: validates the id against the
// registry and persists it, returning the same shape as getTheme.
func (h *handler) updateTheme(w http.ResponseWriter, r *http.Request) {
	var req updateThemeRequest
	if err := DecodeJSON(r, &req); err != nil {
		failBadJSON(w, err)
		return
	}
	if !theme.IsValid(req.Theme) {
		FailValidation(w, map[string]string{"theme": "unknown theme id"})
		return
	}
	if err := h.settings.SetActiveTheme(r.Context(), req.Theme); err != nil {
		Fail(w, http.StatusInternalServerError, "internal", "failed to save theme")
		return
	}
	OK(w, http.StatusOK, themeDTO{ActiveTheme: req.Theme, Available: availableThemes()})
}
