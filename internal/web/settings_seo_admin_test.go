package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/security"
)

func seoReqForm(method, target string, form url.Values) *http.Request {
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
}

func TestSettingsSEO_ShowRendersEffectiveValues(t *testing.T) {
	store := newFakeSettingsStore(map[string]string{
		keySEOGlobalNoindex: "1",
		keyOrgName:          "Acme Co",
		keyOrgSameAs:        "https://a.example\nhttps://b.example",
		keyAnalyticsGA4ID:   "G-ABCD12",
	})
	site := baseSite().WithOverrides(store)
	h := NewSettingsSEOHandler(store, site, settingsShell(), security.Token)

	rec := httptest.NewRecorder()
	h.Show(rec, seoReqForm(http.MethodGet, "/admin/settings/seo", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("Show = %d, want 200\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "settings-seo-form") {
		t.Error("form not rendered")
	}
	if !strings.Contains(body, `value="Acme Co"`) {
		t.Error("org name override not pre-filled")
	}
	// global noindex checkbox checked.
	if !strings.Contains(body, `name="global_noindex"`) || !strings.Contains(body, "checked") {
		t.Error("global_noindex should be checked")
	}
	// sameAs textarea round-trips both URLs.
	if !strings.Contains(body, "https://a.example") || !strings.Contains(body, "https://b.example") {
		t.Error("sameAs textarea should render both URLs")
	}
	if !strings.Contains(body, `value="G-ABCD12"`) {
		t.Error("GA4 id not pre-filled")
	}
}

func TestSettingsSEO_SaveWritesAllKeys(t *testing.T) {
	store := newFakeSettingsStore(nil)
	h := NewSettingsSEOHandler(store, baseSite().WithOverrides(store), settingsShell(), security.Token)

	form := url.Values{
		"global_noindex":      {"1"}, // checked
		"google_verification": {"gtok"},
		"ga4_id":              {"G-ABCD12"},
		"gtm_id":              {"GTM-ABCD12"},
		"org_name":            {"Acme"},
		"org_email":           {"hi@acme.test"},
		"org_same_as":         {" https://a.example \n\n https://b.example \n"},
		"org_geo_statement":   {"Serving Berlin."},
		// allow_ai_crawlers left OUT (unchecked)
	}
	rec := httptest.NewRecorder()
	h.Save(rec, seoReqForm(http.MethodPost, "/admin/settings/seo", form))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("Save = %d, want 303\n%s", rec.Code, rec.Body.String())
	}
	if store.writes[keySEOGlobalNoindex] != "1" {
		t.Errorf("global_noindex = %q, want 1", store.writes[keySEOGlobalNoindex])
	}
	if store.writes[keySEOAllowAICrawlers] != "0" {
		t.Errorf("allow_ai_crawlers (unchecked) = %q, want 0", store.writes[keySEOAllowAICrawlers])
	}
	if store.writes[keySEOGoogleVerification] != "gtok" {
		t.Errorf("google = %q, want gtok", store.writes[keySEOGoogleVerification])
	}
	if store.writes[keyAnalyticsGA4ID] != "G-ABCD12" || store.writes[keyAnalyticsGTMID] != "GTM-ABCD12" {
		t.Errorf("analytics ids = %q / %q", store.writes[keyAnalyticsGA4ID], store.writes[keyAnalyticsGTMID])
	}
	if store.writes[keyOrgName] != "Acme" || store.writes[keyOrgEmail] != "hi@acme.test" {
		t.Errorf("org = %q / %q", store.writes[keyOrgName], store.writes[keyOrgEmail])
	}
	// sameAs normalized (trimmed, blank-free).
	if store.writes[keyOrgSameAs] != "https://a.example\nhttps://b.example" {
		t.Errorf("org_same_as = %q, want normalized", store.writes[keyOrgSameAs])
	}
	if store.writes[keyOrgGeoStatement] != "Serving Berlin." {
		t.Errorf("geo statement = %q", store.writes[keyOrgGeoStatement])
	}
}

func TestSettingsSEO_SaveInvalidGA4Rejects(t *testing.T) {
	store := newFakeSettingsStore(nil)
	h := NewSettingsSEOHandler(store, baseSite().WithOverrides(store), settingsShell(), security.Token)

	form := url.Values{"ga4_id": {"nope"}}
	rec := httptest.NewRecorder()
	h.Save(rec, seoReqForm(http.MethodPost, "/admin/settings/seo", form))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Save(bad GA4) = %d, want 400", rec.Code)
	}
	if len(store.writes) != 0 {
		t.Errorf("invalid GA4 must not persist, got %v", store.writes)
	}
	if !strings.Contains(rec.Body.String(), "settings-error") {
		t.Error("error banner not rendered")
	}
}

func TestSettingsSEO_SaveInvalidGTMRejects(t *testing.T) {
	store := newFakeSettingsStore(nil)
	h := NewSettingsSEOHandler(store, baseSite().WithOverrides(store), settingsShell(), security.Token)

	form := url.Values{"gtm_id": {"bogus"}}
	rec := httptest.NewRecorder()
	h.Save(rec, seoReqForm(http.MethodPost, "/admin/settings/seo", form))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Save(bad GTM) = %d, want 400", rec.Code)
	}
	if len(store.writes) != 0 {
		t.Errorf("invalid GTM must not persist, got %v", store.writes)
	}
}
