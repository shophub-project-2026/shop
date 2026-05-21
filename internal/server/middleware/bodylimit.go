package middleware

import "net/http"

// DefaultMaxBodyBytes is the default request body size limit applied to
// every request by BodyLimit middleware: 1 MiB. Far more than needed for
// any JSON payload in the Shop API.
const DefaultMaxBodyBytes int64 = 1 << 20

// BodyLimit wraps each request's Body with http.MaxBytesReader so
// pathological clients cannot exhaust memory by streaming large payloads.
// A non-positive limit disables the cap (used in tests).
func BodyLimit(limit int64) func(http.Handler) http.Handler {
	if limit <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
