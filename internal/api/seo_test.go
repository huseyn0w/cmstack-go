package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/web"
)

func TestGetSEOProfileParsesBoolsAndSameAs(t *testing.T) {
	userID := uuid.New()
	fs := newFakeSettings()
	k := web.ProfileKeys()
	fs.kv[k.SiteName] = "Acme"
	fs.kv[k.SEOGlobalNoindex] = "1"
	fs.kv[k.SEOAllowAICrawlers] = "false"
	fs.kv[k.OrgSameAs] = "https://x.com/a\n\nhttps://y.com/b"
	fs.kv[k.AnalyticsGA4ID] = "G-ABCD1234"

	srv := newServerDeps(t, userID, map[string]bool{"read:setting": true}, Deps{Settings: fs})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/seo/profile"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	data := decode(t, rec)["data"].(map[string]any)
	site := data["site"].(map[string]any)
	if site["name"] != "Acme" {
		t.Errorf("site.name = %v, want Acme", site["name"])
	}
	idx := data["indexing"].(map[string]any)
	if idx["globalNoindex"] != true || idx["allowAiCrawlers"] != false {
		t.Errorf("indexing bools wrong: %v", idx)
	}
	org := data["organization"].(map[string]any)
	same := org["sameAs"].([]any)
	if len(same) != 2 || same[0] != "https://x.com/a" {
		t.Errorf("sameAs split wrong: %v", same)
	}
	an := data["analytics"].(map[string]any)
	if an["ga4Id"] != "G-ABCD1234" {
		t.Errorf("ga4Id = %v", an["ga4Id"])
	}
}

func TestUpdateSEOProfilePartialWritesAndClears(t *testing.T) {
	userID := uuid.New()
	fs := newFakeSettings()
	k := web.ProfileKeys()
	fs.kv[k.SiteName] = "Old"
	fs.kv[k.OrgEmail] = "keep@me.com"

	srv := newServerDeps(t, userID, map[string]bool{"update:setting": true}, Deps{Settings: fs})
	// Set a new name, and CLEAR the twitter handle (provided empty string).
	body := `{"site":{"name":"New","twitterHandle":""},"indexing":{"globalNoindex":true}}`
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPut, "/api/v1/seo/profile", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if fs.kv[k.SiteName] != "New" {
		t.Errorf("site name not written: %q", fs.kv[k.SiteName])
	}
	if v, ok := fs.kv[k.SiteTwitterHandle]; !ok || v != "" {
		t.Errorf("twitter handle should be cleared to empty, got %q ok=%v", v, ok)
	}
	if fs.kv[k.SEOGlobalNoindex] != "1" {
		t.Errorf("noindex not written as 1: %q", fs.kv[k.SEOGlobalNoindex])
	}
	// Omitted org email must remain untouched.
	if fs.kv[k.OrgEmail] != "keep@me.com" {
		t.Errorf("omitted org email was modified: %q", fs.kv[k.OrgEmail])
	}
}

func TestUpdateSEOProfileRejectsBadAnalyticsID(t *testing.T) {
	userID := uuid.New()
	fs := newFakeSettings()
	srv := newServerDeps(t, userID, map[string]bool{"update:setting": true}, Deps{Settings: fs})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPut, "/api/v1/seo/profile", `{"analytics":{"ga4Id":"not-valid"}}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	k := web.ProfileKeys()
	if _, ok := fs.kv[k.AnalyticsGA4ID]; ok {
		t.Error("invalid GA4 id must not be persisted")
	}
}

func TestUpdateSEOProfileForbidden(t *testing.T) {
	userID := uuid.New()
	srv := newServerDeps(t, userID, map[string]bool{"read:setting": true}, Deps{Settings: newFakeSettings()})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPut, "/api/v1/seo/profile", `{"site":{"name":"X"}}`))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
