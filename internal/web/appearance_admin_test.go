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

type fakeAppearance struct {
	active   string
	setID    string
	setCalls int
}

func (f *fakeAppearance) ActiveTheme(context.Context) string { return f.active }

func (f *fakeAppearance) SetActiveTheme(_ context.Context, id string) error {
	f.setID = id
	f.setCalls++
	return nil
}

func appearanceShell() adminShellDeps {
	return adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
}

func TestAppearance_ShowListsThemesAndMarksActive(t *testing.T) {
	h := NewAppearanceHandler(&fakeAppearance{active: "sepia"}, appearanceShell(), security.Token)
	req := httptest.NewRequest(http.MethodGet, "/admin/appearance", nil)
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Show(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Show = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	// Registry themes are all listed.
	for _, id := range []string{"default", "sepia", "noir"} {
		if !strings.Contains(body, "theme-card-"+id) {
			t.Errorf("appearance page missing card for %q", id)
		}
	}
	// The active theme is badged and has no activate button; others do.
	if !strings.Contains(body, "theme-active-sepia") {
		t.Error("active theme (sepia) not badged")
	}
	if strings.Contains(body, "theme-activate-sepia") {
		t.Error("active theme should not offer an activate button")
	}
	if !strings.Contains(body, "theme-activate-noir") {
		t.Error("inactive theme (noir) missing activate button")
	}
}

func TestAppearance_ShowDefaultsWhenUnset(t *testing.T) {
	// An empty stored theme resolves to the default as active.
	h := NewAppearanceHandler(&fakeAppearance{active: ""}, appearanceShell(), security.Token)
	req := httptest.NewRequest(http.MethodGet, "/admin/appearance", nil)
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Show(rec, req)

	if !strings.Contains(rec.Body.String(), "theme-active-default") {
		t.Error("unset theme should mark default as active")
	}
}

func TestAppearance_ActivatePersistsValidTheme(t *testing.T) {
	fake := &fakeAppearance{active: "default"}
	h := NewAppearanceHandler(fake, appearanceShell(), security.Token)

	form := url.Values{"theme": {"noir"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/appearance/activate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Activate(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("Activate = %d, want 303", rec.Code)
	}
	if fake.setID != "noir" || fake.setCalls != 1 {
		t.Fatalf("SetActiveTheme = (%q, %d calls), want (noir, 1)", fake.setID, fake.setCalls)
	}
}

func TestAppearance_ActivateRejectsUnknownTheme(t *testing.T) {
	fake := &fakeAppearance{active: "default"}
	h := NewAppearanceHandler(fake, appearanceShell(), security.Token)

	form := url.Values{"theme": {"bogus"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/appearance/activate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Activate(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("Activate(unknown) = %d, want 303", rec.Code)
	}
	if fake.setCalls != 0 {
		t.Fatalf("unknown theme should not be persisted (setCalls=%d)", fake.setCalls)
	}
}
