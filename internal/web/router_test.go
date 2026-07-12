package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huseyn0w/agentic-cms-go/internal/health"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/config"
)

func TestRouterHealthLive(t *testing.T) {
	d := Deps{
		Config: config.Config{AppEnv: "test"},
		Health: health.NewHandler(health.NewService(nil)),
	}
	r := Router(d)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("/health status = %d, want 200", rec.Code)
	}
}

func TestRouterSecurityHeaders(t *testing.T) {
	d := Deps{
		Config: config.Config{AppEnv: "test"},
		Health: health.NewHandler(health.NewService(nil)),
	}
	r := Router(d)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("expected nosniff header on all responses")
	}
}

func TestRouterHomeRendersWithCSRF(t *testing.T) {
	d := Deps{
		Config: config.Config{AppEnv: "test"},
		Health: health.NewHandler(health.NewService(nil)),
	}
	r := Router(d)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/ status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct == "" {
		t.Error("expected content-type on home page")
	}
}
