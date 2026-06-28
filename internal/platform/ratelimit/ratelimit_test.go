package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiterAllowsBurstThenBlocks(t *testing.T) {
	l := New(1, 3) // 3 burst, 1/s refill
	for i := 0; i < 3; i++ {
		if !l.Allow("ip-a") {
			t.Fatalf("request %d within burst should be allowed", i)
		}
	}
	if l.Allow("ip-a") {
		t.Error("4th request beyond burst should be blocked")
	}
}

func TestLimiterIsPerKey(t *testing.T) {
	l := New(1, 1)
	if !l.Allow("ip-a") {
		t.Fatal("ip-a first request allowed")
	}
	if !l.Allow("ip-b") {
		t.Fatal("ip-b has its own bucket and should be allowed")
	}
	if l.Allow("ip-a") {
		t.Error("ip-a second request should be blocked")
	}
}

func TestLimiterRefillsOverTime(t *testing.T) {
	now := time.Now()
	l := New(1, 1)
	l.now = func() time.Time { return now }

	if !l.Allow("ip") {
		t.Fatal("first allowed")
	}
	if l.Allow("ip") {
		t.Fatal("second blocked (no refill yet)")
	}
	now = now.Add(2 * time.Second) // refill
	if !l.Allow("ip") {
		t.Error("should be allowed after refill")
	}
}

func TestMiddlewareReturns429(t *testing.T) {
	l := New(0.0001, 1)
	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec1 := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	h.ServeHTTP(rec1, req)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request code = %d, want 200", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request code = %d, want 429", rec2.Code)
	}
	if rec2.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header on 429")
	}
}
