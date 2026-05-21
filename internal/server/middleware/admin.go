package middleware

import (
	"net/http"
)

// Admin returns a middleware that checks for the X-Admin-Key header.
// Requests without the correct key receive 401. An empty adminKey
// disables the check (dev/test convenience).
func Admin(adminKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if adminKey != "" && r.Header.Get("X-Admin-Key") != adminKey {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
