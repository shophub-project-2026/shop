// Package middleware contains HTTP middleware applied across all routes
// of the Shop service.
package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// statusRecorder is a thin http.ResponseWriter wrapper that remembers
// the status code and number of bytes written so the logging middleware
// can report them after the inner handler returns.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

// WriteHeader captures the status code, then delegates.
func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Write counts the number of bytes the handler emitted.
func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		// Handler called Write without WriteHeader -- Go writes 200 OK.
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Logging returns middleware that emits one structured log line per
// HTTP request. The line includes method, path, status, duration and
// response size -- the basis for SLOs and the metrics required by
// requirement 4.1 of the specification.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}

			next.ServeHTTP(rec, r)

			logger.LogAttrs(r.Context(), slog.LevelInfo, "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int("bytes", rec.bytes),
				slog.Duration("duration", time.Since(start)),
				slog.String("remote", r.RemoteAddr),
				slog.String("user_agent", r.UserAgent()),
			)
		})
	}
}
