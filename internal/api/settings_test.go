package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// fakeSettings is an in-memory SettingsService recording writes.
type fakeSettings struct {
	mu     sync.Mutex
	active string
	kv     map[string]string
}

func newFakeSettings() *fakeSettings { return &fakeSettings{kv: map[string]string{}} }

func (f *fakeSettings) ActiveTheme(context.Context) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active
}

func (f *fakeSettings) SetActiveTheme(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.active = id
	return nil
}

func (f *fakeSettings) Get(_ context.Context, key string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.kv[key]
	return v, ok, nil
}

func (f *fakeSettings) Set(_ context.Context, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kv[key] = value
	return nil
}

func TestGetThemeReturnsActiveAndAvailable(t *testing.T) {
	userID := uuid.New()
	fs := newFakeSettings()
	fs.active = "sepia"
	srv := newServerDeps(t, userID, map[string]bool{"read:setting": true}, Deps{Settings: fs})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/settings/theme"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	data := decode(t, rec)["data"].(map[string]any)
	if data["activeTheme"] != "sepia" {
		t.Errorf("activeTheme = %v, want sepia", data["activeTheme"])
	}
	avail := data["available"].([]any)
	if len(avail) != 3 || avail[0] != "default" {
		t.Errorf("available wrong: %v", avail)
	}
}

func TestGetThemeDefaultsWhenUnset(t *testing.T) {
	userID := uuid.New()
	srv := newServerDeps(t, userID, map[string]bool{"read:setting": true}, Deps{Settings: newFakeSettings()})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/settings/theme"))
	data := decode(t, rec)["data"].(map[string]any)
	if data["activeTheme"] != "default" {
		t.Errorf("activeTheme = %v, want default", data["activeTheme"])
	}
}

func TestUpdateThemeValidAndInvalid(t *testing.T) {
	userID := uuid.New()
	fs := newFakeSettings()
	srv := newServerDeps(t, userID, map[string]bool{"update:setting": true}, Deps{Settings: fs})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPut, "/api/v1/settings/theme", `{"theme":"noir"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if fs.active != "noir" {
		t.Errorf("active = %q, want noir", fs.active)
	}

	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPut, "/api/v1/settings/theme", `{"theme":"bogus"}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if fs.active != "noir" {
		t.Errorf("invalid id must not overwrite; active = %q", fs.active)
	}
}

func TestUpdateThemeForbidden(t *testing.T) {
	userID := uuid.New()
	srv := newServerDeps(t, userID, map[string]bool{"read:setting": true}, Deps{Settings: newFakeSettings()})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPut, "/api/v1/settings/theme", `{"theme":"noir"}`))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
