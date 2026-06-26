package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func newTestRouter(db Pinger) http.Handler {
	r := chi.NewRouter()
	NewHandler(NewService(db)).Routes(r)
	return r
}

func TestHandlerLive(t *testing.T) {
	r := newTestRouter(nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var s Status
	if err := json.NewDecoder(rec.Body).Decode(&s); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s.Status != StatusOK {
		t.Errorf("body status = %q, want ok", s.Status)
	}
}

func TestHandlerReady(t *testing.T) {
	tests := []struct {
		name     string
		db       Pinger
		wantCode int
		wantStat string
	}{
		{"healthy", fakePinger{err: nil}, http.StatusOK, StatusOK},
		{"db down", fakePinger{err: errors.New("x")}, http.StatusServiceUnavailable, StatusUnavailable},
		{"no db", nil, http.StatusServiceUnavailable, StatusUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRouter(tt.db)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/health/ready", nil).WithContext(context.Background())
			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			var s Status
			if err := json.NewDecoder(rec.Body).Decode(&s); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if s.Status != tt.wantStat {
				t.Errorf("body status = %q, want %q", s.Status, tt.wantStat)
			}
		})
	}
}
