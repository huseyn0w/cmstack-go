package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/render"
	webtempl "github.com/huseyn0w/agentic-cms-go/web/templ"
)

// SettingsSEOHandler is the thin HTTP boundary for the SEO & GEO settings
// dashboard: indexing gates, search-engine verification tokens, the analytics
// container ids (reusing the M15-1 keys + validators), and the Organization
// JSON-LD / GEO identity. It pre-fills each field with its effective value
// (override || config default) and persists submitted overrides.
type SettingsSEOHandler struct {
	store SettingsStore
	site  SiteConfig
	shell adminShellDeps
	csrf  func(*http.Request) string
}

// NewSettingsSEOHandler constructs the SEO & GEO settings handler.
func NewSettingsSEOHandler(store SettingsStore, site SiteConfig, shell adminShellDeps, csrf func(*http.Request) string) *SettingsSEOHandler {
	return &SettingsSEOHandler{store: store, site: site, shell: shell, csrf: csrf}
}

// Show renders the SEO & GEO form pre-filled with the current effective values.
// A ?saved=1 query renders a success banner after a redirect from Save.
func (h *SettingsSEOHandler) Show(w http.ResponseWriter, r *http.Request) {
	h.renderView(w, r, h.buildView(r, h.currentValues(r.Context()), "", r.URL.Query().Get("saved") == "1"))
}

// Save persists every SEO/GEO/analytics/org override. Booleans write "1"/"0"
// (checked/unchecked). The analytics ids are validated against the M15-1
// patterns; an invalid non-empty id re-renders the form with an error and
// persists nothing. On success it redirects to ?saved=1.
func (h *SettingsSEOHandler) Save(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()

	vals := h.readForm(r)

	if vals.ga4ID != "" && validateGA4ID(vals.ga4ID) == "" {
		h.renderView(w, r, h.buildView(r, vals, "GA4 Measurement ID is not a valid id (e.g. G-XXXXXXX).", false))
		return
	}
	if vals.gtmID != "" && validateGTMID(vals.gtmID) == "" {
		h.renderView(w, r, h.buildView(r, vals, "GTM Container ID is not a valid id (e.g. GTM-XXXXXX).", false))
		return
	}

	writes := map[string]string{
		keySEOGlobalNoindex:         boolToSetting(vals.globalNoindex),
		keySEOAllowAICrawlers:       boolToSetting(vals.allowAICrawlers),
		keySEOGoogleVerification:    vals.googleVerification,
		keySEOBingVerification:      vals.bingVerification,
		keySEOYandexVerification:    vals.yandexVerification,
		keySEOPinterestVerification: vals.pinterestVerification,
		keyAnalyticsGA4ID:           vals.ga4ID,
		keyAnalyticsGTMID:           vals.gtmID,
		keyOrgName:                  vals.orgName,
		keyOrgLegalName:             vals.orgLegalName,
		keyOrgLogo:                  vals.orgLogo,
		keyOrgEmail:                 vals.orgEmail,
		keyOrgPhone:                 vals.orgPhone,
		keyOrgStreet:                vals.orgStreet,
		keyOrgLocality:              vals.orgLocality,
		keyOrgRegion:                vals.orgRegion,
		keyOrgPostalCode:            vals.orgPostalCode,
		keyOrgCountry:               vals.orgCountry,
		keyOrgSameAs:                vals.orgSameAs,
		keyOrgGeoStatement:          vals.orgGeoStatement,
	}
	for k, v := range writes {
		if err := h.store.Set(r.Context(), k, v); err != nil {
			http.Error(w, "error", http.StatusInternalServerError)
			return
		}
	}

	http.Redirect(w, r, "/admin/settings/seo?saved=1", http.StatusSeeOther)
}

// seoFormValues holds the trimmed SEO & GEO form field values.
type seoFormValues struct {
	globalNoindex         bool
	allowAICrawlers       bool
	googleVerification    string
	bingVerification      string
	yandexVerification    string
	pinterestVerification string
	ga4ID                 string
	gtmID                 string
	orgName               string
	orgLegalName          string
	orgLogo               string
	orgEmail              string
	orgPhone              string
	orgStreet             string
	orgLocality           string
	orgRegion             string
	orgPostalCode         string
	orgCountry            string
	orgSameAs             string
	orgGeoStatement       string
}

// readForm parses the submitted SEO & GEO form.
func (h *SettingsSEOHandler) readForm(r *http.Request) seoFormValues {
	t := func(name string) string { return strings.TrimSpace(r.PostFormValue(name)) }
	return seoFormValues{
		globalNoindex:         checkboxChecked(r, "global_noindex"),
		allowAICrawlers:       checkboxChecked(r, "allow_ai_crawlers"),
		googleVerification:    t("google_verification"),
		bingVerification:      t("bing_verification"),
		yandexVerification:    t("yandex_verification"),
		pinterestVerification: t("pinterest_verification"),
		ga4ID:                 t("ga4_id"),
		gtmID:                 t("gtm_id"),
		orgName:               t("org_name"),
		orgLegalName:          t("org_legal_name"),
		orgLogo:               t("org_logo"),
		orgEmail:              t("org_email"),
		orgPhone:              t("org_phone"),
		orgStreet:             t("org_street"),
		orgLocality:           t("org_locality"),
		orgRegion:             t("org_region"),
		orgPostalCode:         t("org_postal_code"),
		orgCountry:            t("org_country"),
		orgSameAs:             normalizeSameAs(r.PostFormValue("org_same_as")),
		orgGeoStatement:       t("org_geo_statement"),
	}
}

// currentValues reads each field's current effective value for the form.
func (h *SettingsSEOHandler) currentValues(ctx context.Context) seoFormValues {
	org := h.site.resolveOrg(ctx)
	verif := verifMap(h.site.resolveVerifications(ctx))
	return seoFormValues{
		globalNoindex:         h.site.resolveGlobalNoindex(ctx),
		allowAICrawlers:       h.site.resolveAllowAICrawlers(ctx),
		googleVerification:    verif["google-site-verification"],
		bingVerification:      verif["msvalidate.01"],
		yandexVerification:    verif["yandex-verification"],
		pinterestVerification: verif["p:domain_verify"],
		ga4ID:                 h.rawSetting(ctx, keyAnalyticsGA4ID),
		gtmID:                 h.rawSetting(ctx, keyAnalyticsGTMID),
		orgName:               org.Name,
		orgLegalName:          org.LegalName,
		orgLogo:               org.LogoURL,
		orgEmail:              org.Email,
		orgPhone:              org.Phone,
		orgStreet:             org.Street,
		orgLocality:           org.Locality,
		orgRegion:             org.Region,
		orgPostalCode:         org.PostalCode,
		orgCountry:            org.Country,
		orgSameAs:             strings.Join(org.SameAs, "\n"),
		orgGeoStatement:       org.GeoStatement,
	}
}

// rawSetting reads a raw settings value (no config fallback), or "" when unset.
func (h *SettingsSEOHandler) rawSetting(ctx context.Context, key string) string {
	if v, ok, err := h.store.Get(ctx, key); err == nil && ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// buildView assembles the SEO & GEO settings view-model.
func (h *SettingsSEOHandler) buildView(r *http.Request, v seoFormValues, errMsg string, saved bool) webtempl.SettingsSEOView {
	return webtempl.SettingsSEOView{
		Shell:                 h.shell.buildShell(r, "SEO & GEO settings"),
		SaveURL:               "/admin/settings/seo",
		CSRFToken:             h.csrf(r),
		GlobalNoindex:         v.globalNoindex,
		AllowAICrawlers:       v.allowAICrawlers,
		GoogleVerification:    v.googleVerification,
		BingVerification:      v.bingVerification,
		YandexVerification:    v.yandexVerification,
		PinterestVerification: v.pinterestVerification,
		GA4ID:                 v.ga4ID,
		GTMID:                 v.gtmID,
		OrgName:               v.orgName,
		OrgLegalName:          v.orgLegalName,
		OrgLogo:               v.orgLogo,
		OrgEmail:              v.orgEmail,
		OrgPhone:              v.orgPhone,
		OrgStreet:             v.orgStreet,
		OrgLocality:           v.orgLocality,
		OrgRegion:             v.orgRegion,
		OrgPostalCode:         v.orgPostalCode,
		OrgCountry:            v.orgCountry,
		OrgSameAs:             v.orgSameAs,
		OrgGeoStatement:       v.orgGeoStatement,
		Error:                 errMsg,
		Saved:                 saved,
	}
}

// renderView renders the SEO & GEO page (400 when an error banner is set).
func (h *SettingsSEOHandler) renderView(w http.ResponseWriter, r *http.Request, view webtempl.SettingsSEOView) {
	status := http.StatusOK
	if view.Error != "" {
		status = http.StatusBadRequest
	}
	if err := render.Component(r.Context(), w, status, webtempl.SettingsSEOPage(view)); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// checkboxChecked reports whether an HTML checkbox named field was submitted
// checked (a checked box posts a value; an unchecked one posts nothing).
func checkboxChecked(r *http.Request, field string) bool {
	return r.PostFormValue(field) != ""
}

// boolToSetting maps a bool onto the "1"/"0" settings encoding.
func boolToSetting(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// verifMap indexes verification tags by name for form pre-fill.
func verifMap(tags []webtempl.MetaTag) map[string]string {
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		m[t.Name] = t.Content
	}
	return m
}

// normalizeSameAs trims each sameAs line and drops blanks, re-joining with "\n"
// so the stored override round-trips cleanly through splitSameAs.
func normalizeSameAs(v string) string {
	return strings.Join(splitSameAs(v), "\n")
}
