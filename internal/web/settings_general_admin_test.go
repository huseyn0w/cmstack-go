package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
)

// fakeSettingsStore is an in-memory SettingsStore capturing writes.
type fakeSettingsStore struct {
	values map[string]string
	writes map[string]string // captured Set calls (last write wins)
}

func newFakeSettingsStore(seed map[string]string) *fakeSettingsStore {
	if seed == nil {
		seed = map[string]string{}
	}
	return &fakeSettingsStore{values: seed, writes: map[string]string{}}
}

func (f *fakeSettingsStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := f.values[key]
	return v, ok, nil
}

func (f *fakeSettingsStore) Set(_ context.Context, key, value string) error {
	if f.writes == nil {
		f.writes = map[string]string{}
	}
	f.writes[key] = value
	f.values[key] = value
	return nil
}

func settingsShell() adminShellDeps {
	return adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
}

func generalReq(method, target string, form url.Values) *http.Request {
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

func TestSettingsGeneral_ShowRendersEffectiveValues(t *testing.T) {
	store := newFakeSettingsStore(map[string]string{keySiteName: "Overridden Site"})
	site := baseSite().WithOverrides(store) // config SiteName=CMStack, description=A server-rendered CMS
	h := NewSettingsGeneralHandler(store, site, settingsShell(), security.Token)

	rec := httptest.NewRecorder()
	h.Show(rec, generalReq(http.MethodGet, "/admin/settings/general", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("Show = %d, want 200\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `value="Overridden Site"`) {
		t.Error("site name override not pre-filled")
	}
	// Config default shown when no override.
	if !strings.Contains(body, "A server-rendered CMS") {
		t.Error("config default description not pre-filled")
	}
	if !strings.Contains(body, "settings-general-form") {
		t.Error("form not rendered")
	}
}

func TestSettingsGeneral_SaveWritesAllKeys(t *testing.T) {
	store := newFakeSettingsStore(nil)
	h := NewSettingsGeneralHandler(store, baseSite().WithOverrides(store), settingsShell(), security.Token)

	form := url.Values{
		"site_name":         {"New Name"},
		"site_description":  {"New Desc"},
		"default_og_image":  {"/og.png"},
		"twitter_handle":    {"@new"},
		"contact_recipient": {"ops@site.test"},
	}
	rec := httptest.NewRecorder()
	h.Save(rec, generalReq(http.MethodPost, "/admin/settings/general", form))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("Save = %d, want 303\n%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/settings/general?saved=1" {
		t.Errorf("redirect = %q, want ?saved=1", loc)
	}
	want := map[string]string{
		keySiteName:                "New Name",
		keySiteDescription:         "New Desc",
		keySiteDefaultOGImage:      "/og.png",
		keySiteTwitterHandle:       "@new",
		contactRecipientSettingKey: "ops@site.test",
	}
	for k, v := range want {
		if store.writes[k] != v {
			t.Errorf("write[%q] = %q, want %q", k, store.writes[k], v)
		}
	}
}

func TestSettingsGeneral_SaveEmptyClearsOverride(t *testing.T) {
	store := newFakeSettingsStore(map[string]string{keySiteName: "Old"})
	h := NewSettingsGeneralHandler(store, baseSite().WithOverrides(store), settingsShell(), security.Token)

	form := url.Values{"site_name": {""}} // empty -> clear
	rec := httptest.NewRecorder()
	h.Save(rec, generalReq(http.MethodPost, "/admin/settings/general", form))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("Save = %d, want 303", rec.Code)
	}
	if v, ok := store.writes[keySiteName]; !ok || v != "" {
		t.Errorf("site_name write = %q (present=%v), want cleared to \"\"", v, ok)
	}
}

func TestSettingsGeneral_SaveInvalidEmailRejects(t *testing.T) {
	store := newFakeSettingsStore(nil)
	h := NewSettingsGeneralHandler(store, baseSite().WithOverrides(store), settingsShell(), security.Token)

	form := url.Values{
		"site_name":         {"Keep"},
		"contact_recipient": {"not-an-email"},
	}
	rec := httptest.NewRecorder()
	h.Save(rec, generalReq(http.MethodPost, "/admin/settings/general", form))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Save(invalid email) = %d, want 400", rec.Code)
	}
	if len(store.writes) != 0 {
		t.Errorf("invalid submit must not persist anything, got %v", store.writes)
	}
	if !strings.Contains(rec.Body.String(), "settings-error") {
		t.Error("error banner not rendered")
	}
}
