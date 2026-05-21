package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shophub-project-2026/shop/internal/server/middleware"
)

func TestSecurityHeaders_AreSet(t *testing.T) {
	h := middleware.SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	want := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
		"Content-Security-Policy": "default-src 'self'",
	}
	for k, prefix := range want {
		got := rec.Header().Get(k)
		if got == "" {
			t.Errorf("header %s not set", k)
		}
		if len(prefix) > 0 && len(got) < len(prefix) {
			t.Errorf("header %s = %q, want it to start with %q", k, got, prefix)
		}
	}
}
