package web

import (
	"bytes"
	"encoding/gob"
	"net/http"
	"strings"
	"time"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/cache"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/session"
)

// PageCache is an anonymous, full-response cache for the PUBLIC content group.
// It stores only complete, safe-to-share 200 text/html renderings and NEVER
// serves a cached body to a possibly-authenticated or personalized request:
// correctness beats hit-rate. Invalidation is eager (on content publish, keyed
// by the "page:" prefix) with the TTL as a staleness backstop.
type PageCache struct {
	cache cache.Cache
	ttl   time.Duration
}

// NewPageCache builds a PageCache over c with the given TTL. A nil cache yields
// a nil *PageCache; its Middleware is then a pass-through (caching disabled),
// so reduced-Deps wiring keeps working with zero configuration.
func NewPageCache(c cache.Cache, ttl time.Duration) *PageCache {
	if c == nil {
		return nil
	}
	return &PageCache{cache: c, ttl: ttl}
}

// pageEntry is the stored, replayable response: status + Content-Type + body.
// It is gob-encoded so the cache stays a byte store.
type pageEntry struct {
	Status      int
	ContentType string
	Body        []byte
}

// Middleware wraps next with the anonymous page cache. When PageCache is nil it
// returns next unchanged. It MUST run AFTER the locale + theme middleware so the
// active locale/theme are in context for the cache key, and OUTERMOST on the
// public content routes so a hit short-circuits before any rendering work.
func (pc *PageCache) Middleware(next http.Handler) http.Handler {
	if pc == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if pc.bypass(r) {
			next.ServeHTTP(w, r)
			return
		}

		ctx := r.Context()
		key := pc.key(r)

		if raw, ok, err := pc.cache.Get(ctx, key); err == nil && ok {
			if ent, derr := decodePageEntry(raw); derr == nil {
				pc.replay(w, ent)
				return
			}
		}

		rec := &pageRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		if pc.storable(rec) {
			ent := pageEntry{
				Status:      rec.status,
				ContentType: rec.Header().Get("Content-Type"),
				Body:        rec.body.Bytes(),
			}
			if enc, err := encodePageEntry(ent); err == nil {
				_ = pc.cache.Set(ctx, key, enc, pc.ttl)
			}
		}
	})
}

// bypass reports whether the request must be served fresh and NOT stored. Any of
// the following forces a bypass:
//   - non-GET (mutations, and only GET is idempotent/cacheable);
//   - the session cookie is present (the request may be authenticated — we never
//     serve a shared body to a logged-in user);
//   - an HX-Request header (htmx partial: a fragment, not a full page);
//   - any query string (query-driven responses are one-off/personalized; the
//     conservative rule is to cache only bare paths).
func (pc *PageCache) bypass(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return true
	}
	if _, err := r.Cookie(session.CookieName); err == nil {
		return true
	}
	if r.Header.Get("HX-Request") != "" {
		return true
	}
	if r.URL.RawQuery != "" {
		return true
	}
	return false
}

// key is "page:" + locale + ":" + theme + ":" + path. Locale and theme come from
// context (populated by the upstream locale/theme middleware); including both
// means a per-locale, per-theme cache so a de/theme-b render never leaks into an
// en/theme-a request.
func (pc *PageCache) key(r *http.Request) string {
	locale := LocaleFromContext(r.Context()).String()
	theme := ActiveThemeFromContext(r.Context())
	return "page:" + locale + ":" + theme + ":" + r.URL.Path
}

// storable reports whether a recorded response may be cached: a 200, an HTML
// content type, and no Set-Cookie header (a response that sets a cookie is
// per-user state and must never be shared). Redirects, errors and non-HTML are
// all excluded.
func (pc *PageCache) storable(rec *pageRecorder) bool {
	if rec.status != http.StatusOK {
		return false
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		return false
	}
	if rec.Header().Get("Set-Cookie") != "" {
		return false
	}
	return true
}

// replay writes a cached entry back to the client verbatim.
func (pc *PageCache) replay(w http.ResponseWriter, ent pageEntry) {
	if ent.ContentType != "" {
		w.Header().Set("Content-Type", ent.ContentType)
	}
	status := ent.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(ent.Body)
}

// pageRecorder buffers the downstream handler's response so the middleware can
// inspect it (status/Content-Type/Set-Cookie) and, when storable, cache the body
// while still forwarding everything to the real client.
type pageRecorder struct {
	http.ResponseWriter
	status      int
	body        bytes.Buffer
	wroteHeader bool
}

func (rec *pageRecorder) WriteHeader(status int) {
	if rec.wroteHeader {
		return
	}
	rec.status = status
	rec.wroteHeader = true
	rec.ResponseWriter.WriteHeader(status)
}

func (rec *pageRecorder) Write(b []byte) (int, error) {
	if !rec.wroteHeader {
		rec.WriteHeader(http.StatusOK)
	}
	rec.body.Write(b)
	return rec.ResponseWriter.Write(b)
}

func encodePageEntry(ent pageEntry) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(ent); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodePageEntry(raw []byte) (pageEntry, error) {
	var ent pageEntry
	err := gob.NewDecoder(bytes.NewReader(raw)).Decode(&ent)
	return ent, err
}
