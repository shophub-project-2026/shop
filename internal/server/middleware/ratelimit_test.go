package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shophub-project-2026/shop/internal/server/middleware"
)

func TestRateLimiter_AllowsBurstThenBlocks(t *testing.T) {
	rl := middleware.NewRateLimiter(1, 3, time.Minute)

	for i := 0; i < 3; i++ {
		if !rl.Allow("1.2.3.4") {
			t.Errorf("burst[%d] should have been allowed", i)
		}
	}
	if rl.Allow("1.2.3.4") {
		t.Error("fourth request in burst window should be blocked")
	}
	// Other client should be unaffected.
	if !rl.Allow("5.6.7.8") {
		t.Error("other client should be allowed")
	}
}

func TestRateLimiter_Middleware_429(t *testing.T) {
	rl := middleware.NewRateLimiter(0.0001, 1, time.Minute) // effectively 1 req then block
	h := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/", nil)
	req.RemoteAddr = "1.2.3.4:5555"

	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req)
	if rec1.Code != http.StatusOK {
		t.Errorf("first request status = %d, want 200", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second request status = %d, want 429", rec2.Code)
	}
}
