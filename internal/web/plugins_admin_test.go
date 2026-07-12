package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/security"
	"github.com/huseyn0w/agentic-cms-go/internal/plugin"
)

type fakePluginCatalogue struct {
	metas   []plugin.Meta
	enabled map[string]bool
	setID   string
	setOn   bool
	setErr  error
	setCall int
}

func (f *fakePluginCatalogue) Catalogue() []plugin.Meta { return f.metas }

func (f *fakePluginCatalogue) IsEnabled(_ context.Context, id string) bool { return f.enabled[id] }

func (f *fakePluginCatalogue) SetEnabled(_ context.Context, id string, on bool) error {
	f.setID, f.setOn, f.setCall = id, on, f.setCall+1
	return f.setErr
}

func pluginsCatalogue() *fakePluginCatalogue {
	return &fakePluginCatalogue{
		metas: []plugin.Meta{
			{ID: "reading-time", Name: "Reading Time", Description: "Prepends read time."},
			{ID: "seo-boost", Name: "SEO Boost", Description: "Extra meta."},
		},
		enabled: map[string]bool{"reading-time": true},
	}
}

func TestPlugins_ShowListsCatalogueWithState(t *testing.T) {
	h := NewPluginAdminHandler(pluginsCatalogue(), appearanceShell(), security.Token)
	req := httptest.NewRequest(http.MethodGet, "/admin/plugins", nil)
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Show(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Show = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, id := range []string{"reading-time", "seo-boost"} {
		if !strings.Contains(body, "plugin-row-"+id) {
			t.Errorf("missing plugin row %q", id)
		}
		if !strings.Contains(body, "plugin-toggle-"+id) {
			t.Errorf("missing toggle for %q", id)
		}
	}
	// Enabled plugin offers "Deactivate" (enable=0); disabled offers "Activate".
	if !strings.Contains(body, `name="enable" value="0"`) {
		t.Error("enabled plugin should submit enable=0 (deactivate)")
	}
	if !strings.Contains(body, `name="enable" value="1"`) {
		t.Error("disabled plugin should submit enable=1 (activate)")
	}
}

func TestPlugins_ToggleEnables(t *testing.T) {
	cat := pluginsCatalogue()
	h := NewPluginAdminHandler(cat, appearanceShell(), security.Token)

	form := url.Values{"plugin": {"seo-boost"}, "enable": {"1"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/plugins/toggle", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Toggle(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("Toggle = %d, want 303", rec.Code)
	}
	if cat.setCall != 1 || cat.setID != "seo-boost" || cat.setOn != true {
		t.Fatalf("SetEnabled = (%q, %v, %d calls), want (seo-boost, true, 1)", cat.setID, cat.setOn, cat.setCall)
	}
}

func TestPlugins_ToggleDisables(t *testing.T) {
	cat := pluginsCatalogue()
	h := NewPluginAdminHandler(cat, appearanceShell(), security.Token)

	form := url.Values{"plugin": {"reading-time"}, "enable": {"0"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/plugins/toggle", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Toggle(rec, req)

	if cat.setOn != false || cat.setID != "reading-time" {
		t.Fatalf("SetEnabled = (%q, %v), want (reading-time, false)", cat.setID, cat.setOn)
	}
}
