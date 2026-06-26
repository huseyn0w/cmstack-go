package security

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHeaders(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("development omits HSTS", func(t *testing.T) {
		rec := httptest.NewRecorder()
		Headers(false)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Error("missing nosniff")
		}
		if rec.Header().Get("X-Frame-Options") != "DENY" {
			t.Error("missing frame-options")
		}
		if rec.Header().Get("Strict-Transport-Security") != "" {
			t.Error("HSTS should not be set in development")
		}
	})

	t.Run("CSP default is present", func(t *testing.T) {
		rec := httptest.NewRecorder()
		Headers(false)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		got := rec.Header().Get("Content-Security-Policy")
		if got == "" {
			t.Fatal("missing Content-Security-Policy header")
		}
		for _, want := range []string{
			"default-src 'self'",
			"script-src 'self'",
			"style-src 'self' 'unsafe-inline'",
			"img-src 'self' data:",
			"object-src 'none'",
			"base-uri 'self'",
			"frame-ancestors 'none'",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("CSP %q missing directive %q", got, want)
			}
		}
	})

	t.Run("CSP override is honored", func(t *testing.T) {
		rec := httptest.NewRecorder()
		const custom = "default-src 'none'"
		Headers(false, custom)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if got := rec.Header().Get("Content-Security-Policy"); got != custom {
			t.Errorf("CSP = %q, want %q", got, custom)
		}
	})

	t.Run("production sets HSTS", func(t *testing.T) {
		rec := httptest.NewRecorder()
		Headers(true)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Header().Get("Strict-Transport-Security") == "" {
			t.Error("HSTS should be set in production")
		}
	})
}

func TestCSRFRejectsUnsafeWithoutToken(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	CSRF(false)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for tokenless POST, got %d", rec.Code)
	}
}
