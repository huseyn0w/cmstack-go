package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/cache"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/session"
)

// countingHandler renders a fixed 200 text/html body and counts invocations, so
// a test can prove whether a second request was served from cache (count stays
// 1) or re-rendered (count increments).
type countingHandler struct {
	calls int
	body  string
}

func (h *countingHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.calls++
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(h.body))
}

func newTestPageCache(t *testing.T) (*PageCache, cache.Cache) {
	t.Helper()
	c := cache.NewMemory()
	return NewPageCache(c, time.Minute), c
}

func TestPageCache_CachesAnonymousGet(t *testing.T) {
	pc, _ := newTestPageCache(t)
	next := &countingHandler{body: "<html>hi</html>"}
	h := pc.Middleware(next)

	// First request: miss -> handler runs, response stored.
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/blog", nil))
	if next.calls != 1 {
		t.Fatalf("handler calls after first request = %d, want 1", next.calls)
	}
	if rec1.Code != http.StatusOK || rec1.Body.String() != "<html>hi</html>" {
		t.Fatalf("first response = %d %q", rec1.Code, rec1.Body.String())
	}

	// Second request: hit -> handler NOT invoked, body replayed.
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/blog", nil))
	if next.calls != 1 {
		t.Fatalf("handler calls after cached request = %d, want 1 (served from cache)", next.calls)
	}
	if rec2.Code != http.StatusOK || rec2.Body.String() != "<html>hi</html>" {
		t.Fatalf("cached response = %d %q", rec2.Code, rec2.Body.String())
	}
	if ct := rec2.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("cached content-type = %q", ct)
	}
}

func TestPageCache_BypassesWithSessionCookie(t *testing.T) {
	pc, _ := newTestPageCache(t)
	next := &countingHandler{body: "<html>hi</html>"}
	h := pc.Middleware(next)

	req := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/blog", nil)
		r.AddCookie(&http.Cookie{Name: session.CookieName, Value: "abc"})
		return r
	}

	h.ServeHTTP(httptest.NewRecorder(), req())
	h.ServeHTTP(httptest.NewRecorder(), req())
	if next.calls != 2 {
		t.Fatalf("handler calls = %d, want 2 (session cookie must bypass the cache)", next.calls)
	}
}

func TestPageCache_BypassesHXRequest(t *testing.T) {
	pc, _ := newTestPageCache(t)
	next := &countingHandler{body: "<html>partial</html>"}
	h := pc.Middleware(next)

	req := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/blog", nil)
		r.Header.Set("HX-Request", "true")
		return r
	}

	h.ServeHTTP(httptest.NewRecorder(), req())
	h.ServeHTTP(httptest.NewRecorder(), req())
	if next.calls != 2 {
		t.Fatalf("handler calls = %d, want 2 (htmx partials must bypass the cache)", next.calls)
	}
}

func TestPageCache_BypassesQueryString(t *testing.T) {
	pc, _ := newTestPageCache(t)
	next := &countingHandler{body: "<html>hi</html>"}
	h := pc.Middleware(next)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/blog?sent=1", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/blog?sent=1", nil))
	if next.calls != 2 {
		t.Fatalf("handler calls = %d, want 2 (query strings must bypass the cache)", next.calls)
	}
}

func TestPageCache_DoesNotCacheNon200(t *testing.T) {
	pc, _ := newTestPageCache(t)
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	h := pc.Middleware(next)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/blog", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/blog", nil))
	if calls != 2 {
		t.Fatalf("handler calls = %d, want 2 (non-200 must not be cached)", calls)
	}
}

func TestPageCache_DoesNotCacheResponseSettingCookie(t *testing.T) {
	pc, _ := newTestPageCache(t)
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		http.SetCookie(w, &http.Cookie{Name: "flash", Value: "x"})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>hi</html>"))
	})
	h := pc.Middleware(next)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/blog", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/blog", nil))
	if calls != 2 {
		t.Fatalf("handler calls = %d, want 2 (a Set-Cookie response must never be cached)", calls)
	}
}

func TestPageCache_InvalidationReRenders(t *testing.T) {
	pc, c := newTestPageCache(t)
	next := &countingHandler{body: "<html>v1</html>"}
	h := pc.Middleware(next)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/blog", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/blog", nil))
	if next.calls != 1 {
		t.Fatalf("handler calls before invalidation = %d, want 1", next.calls)
	}

	// Invalidate the page prefix (what the publish listener does).
	if err := c.DeleteByPrefix(context.Background(), "page:"); err != nil {
		t.Fatalf("DeleteByPrefix: %v", err)
	}

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/blog", nil))
	if next.calls != 2 {
		t.Fatalf("handler calls after invalidation = %d, want 2 (must re-render)", next.calls)
	}
}

func TestPageCache_NilIsPassThrough(t *testing.T) {
	var pc *PageCache // nil cache -> caching disabled
	next := &countingHandler{body: "<html>hi</html>"}
	h := pc.Middleware(next)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/blog", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/blog", nil))
	if next.calls != 2 {
		t.Fatalf("handler calls = %d, want 2 (nil PageCache must be a pass-through)", next.calls)
	}
}
