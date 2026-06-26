package logging

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// RequestLogger returns chi-compatible middleware that emits one structured
// slog line per request with method, path, status, byte count, duration and the
// request ID injected by chi's RequestID middleware.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()

			defer func() {
				logger.LogAttrs(
					r.Context(), slog.LevelInfo, "http_request",
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Int("status", ww.Status()),
					slog.Int("bytes", ww.BytesWritten()),
					slog.Duration("duration", time.Since(start)),
					slog.String("request_id", middleware.GetReqID(r.Context())),
					slog.String("remote_ip", r.RemoteAddr),
				)
			}()

			next.ServeHTTP(ww, r)
		})
	}
}
