package web

import (
	"context"
	"net/http"
	"net/mail"
	"strings"

	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// SettingsStore is the narrow settings dependency the admin settings dashboards
// need: read a raw value by key (to pre-fill the effective value) and upsert a
// value (an empty string clears the override so the config default applies).
// *settings.Service satisfies it. Declaring it here keeps web decoupled from the
// settings package and trivially fakeable in tests.
type SettingsStore interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string) error
}

// SettingsGeneralHandler is the thin HTTP boundary for the General settings
// dashboard: it renders a form pre-filled with each field's effective value
// (override || config default) and persists submitted overrides. It holds no
// business logic beyond form parsing/validation; the effective-value resolution
// lives on SiteConfig.
type SettingsGeneralHandler struct {
	store SettingsStore
	site  SiteConfig
	shell adminShellDeps
	csrf  func(*http.Request) string
}

// NewSettingsGeneralHandler constructs the General settings handler. The site
// config supplies the config defaults shown when a key has no override.
func NewSettingsGeneralHandler(store SettingsStore, site SiteConfig, shell adminShellDeps, csrf func(*http.Request) string) *SettingsGeneralHandler {
	return &SettingsGeneralHandler{store: store, site: site, shell: shell, csrf: csrf}
}

// Show renders the General settings form, pre-filling each input with the
// current effective value (override || config default). A ?saved=1 query renders
// a success banner after a redirect from Save.
func (h *SettingsGeneralHandler) Show(w http.ResponseWriter, r *http.Request) {
	view := h.buildView(r, h.currentValues(r.Context()), "", r.URL.Query().Get("saved") == "1")
	h.renderView(w, r, view)
}

// Save persists each submitted field as a settings override (an empty field
// clears the override → config default). The contact recipient, when non-empty,
// must be a valid email; an invalid value re-renders the form with an error and
// persists nothing. On success it redirects to ?saved=1.
func (h *SettingsGeneralHandler) Save(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()

	vals := generalFormValues{
		siteName:     strings.TrimSpace(r.PostFormValue("site_name")),
		siteDesc:     strings.TrimSpace(r.PostFormValue("site_description")),
		ogImage:      strings.TrimSpace(r.PostFormValue("default_og_image")),
		twitter:      strings.TrimSpace(r.PostFormValue("twitter_handle")),
		contactEmail: strings.TrimSpace(r.PostFormValue("contact_recipient")),
	}

	if vals.contactEmail != "" {
		if _, err := mail.ParseAddress(vals.contactEmail); err != nil {
			h.renderView(w, r, h.buildView(r, vals, "Contact recipient must be a valid email address.", false))
			return
		}
	}

	writes := map[string]string{
		keySiteName:                vals.siteName,
		keySiteDescription:         vals.siteDesc,
		keySiteDefaultOGImage:      vals.ogImage,
		keySiteTwitterHandle:       vals.twitter,
		contactRecipientSettingKey: vals.contactEmail,
	}
	for k, v := range writes {
		if err := h.store.Set(r.Context(), k, v); err != nil {
			http.Error(w, "error", http.StatusInternalServerError)
			return
		}
	}

	http.Redirect(w, r, "/admin/settings/general?saved=1", http.StatusSeeOther)
}

// generalFormValues holds the trimmed General form field values.
type generalFormValues struct {
	siteName     string
	siteDesc     string
	ogImage      string
	twitter      string
	contactEmail string
}

// currentValues reads each field's current effective value (override || config
// default) for pre-filling the form.
func (h *SettingsGeneralHandler) currentValues(ctx context.Context) generalFormValues {
	return generalFormValues{
		siteName:     h.site.resolveSiteName(ctx),
		siteDesc:     h.site.resolveSiteDescription(ctx),
		ogImage:      h.site.resolveDefaultOGImage(ctx),
		twitter:      h.site.resolveTwitterHandle(ctx),
		contactEmail: h.contactRecipient(ctx),
	}
}

// contactRecipient reads the effective contact recipient override (the config
// default lives in the contact wiring, not SiteConfig, so only the override is
// pre-filled here — an empty value means "using the config default").
func (h *SettingsGeneralHandler) contactRecipient(ctx context.Context) string {
	if v, ok, err := h.store.Get(ctx, contactRecipientSettingKey); err == nil && ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// buildView assembles the General settings view-model.
func (h *SettingsGeneralHandler) buildView(r *http.Request, vals generalFormValues, errMsg string, saved bool) webtempl.SettingsGeneralView {
	return webtempl.SettingsGeneralView{
		Shell:           h.shell.buildShell(r, "General settings"),
		SaveURL:         "/admin/settings/general",
		CSRFToken:       h.csrf(r),
		SiteName:        vals.siteName,
		SiteDescription: vals.siteDesc,
		DefaultOGImage:  vals.ogImage,
		TwitterHandle:   vals.twitter,
		ContactEmail:    vals.contactEmail,
		Error:           errMsg,
		Saved:           saved,
	}
}

// renderView renders the General settings page (400 when an error banner is set,
// else 200).
func (h *SettingsGeneralHandler) renderView(w http.ResponseWriter, r *http.Request, view webtempl.SettingsGeneralView) {
	status := http.StatusOK
	if view.Error != "" {
		status = http.StatusBadRequest
	}
	if err := render.Component(r.Context(), w, status, webtempl.SettingsGeneralPage(view)); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
