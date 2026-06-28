// Package ratelimit provides a per-key in-process token-bucket limiter and HTTP
// middleware. It is the in-proc implementation for M1; a Redis-backed limiter
// for multi-instance deployments is noted for M13.
package ratelimit

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// bucket is a single token bucket: tokens refill continuously at rate per
// second up to burst.
type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// Limiter is a keyed token-bucket limiter safe for concurrent use. Stale
// buckets are pruned opportunistically to bound memory.
type Limiter struct {
	rate  float64 // tokens per second
	burst float64 // bucket capacity
	now   func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
	lastGC  time.Time
}

// New constructs a Limiter allowing burst requests immediately and refilling at
// rate tokens/second.
func New(rate, burst float64) *Limiter {
	return &Limiter{
		rate:    rate,
		burst:   burst,
		now:     time.Now,
		buckets: make(map[string]*bucket),
	}
}

// Allow reports whether a request for key may proceed, consuming a token if so.
func (l *Limiter) Allow(key string) bool {
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.gcLocked(now)

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, lastSeen: now}
		l.buckets[key] = b
	}

	// Refill based on elapsed time.
	elapsed := now.Sub(b.lastSeen).Seconds()
	b.tokens = minFloat(l.burst, b.tokens+elapsed*l.rate)
	b.lastSeen = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// gcLocked prunes buckets unused for a while. Caller holds l.mu.
func (l *Limiter) gcLocked(now time.Time) {
	if now.Sub(l.lastGC) < time.Minute {
		return
	}
	l.lastGC = now
	for k, b := range l.buckets {
		if now.Sub(b.lastSeen) > 10*time.Minute {
			delete(l.buckets, k)
		}
	}
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// Middleware returns HTTP middleware that rate-limits per client IP, returning
// 429 with a Retry-After header when the bucket is empty.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(clientIP(r)) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP extracts the client IP from RemoteAddr. X-Forwarded-For is
// intentionally NOT trusted here (spoofable); real-IP resolution behind a known
// proxy is handled centrally (see router note).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
