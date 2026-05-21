package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	shopmetrics "github.com/shophub-project-2026/shop/internal/metrics"
)

func TestMetricsMiddleware_CountsRequests(t *testing.T) {
	before := testutil.ToFloat64(shopmetrics.HTTPRequestsTotal.WithLabelValues("GET", "/healthz", "200"))

	handler := shopmetrics.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	after := testutil.ToFloat64(shopmetrics.HTTPRequestsTotal.WithLabelValues("GET", "/healthz", "200"))
	if after-before != 1 {
		t.Errorf("expected counter increment of 1, got %f", after-before)
	}
}

func TestMetricsMiddleware_TracksBytes(t *testing.T) {
	before := testutil.ToFloat64(shopmetrics.HTTPResponseBytesTotal)

	handler := shopmetrics.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	after := testutil.ToFloat64(shopmetrics.HTTPResponseBytesTotal)
	if after-before < 5 {
		t.Errorf("expected >= 5 bytes counted, got %f", after-before)
	}
}

func TestSanitizePath_KnownRouteWithUUID(t *testing.T) {
	before := testutil.ToFloat64(shopmetrics.HTTPRequestsTotal.WithLabelValues("GET", "/articles/{id}", "200"))

	handler := shopmetrics.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/articles/550e8400-e29b-41d4-a716-446655440000", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	after := testutil.ToFloat64(shopmetrics.HTTPRequestsTotal.WithLabelValues("GET", "/articles/{id}", "200"))
	if after-before != 1 {
		t.Errorf("/articles/{id} counter should have incremented by 1, got %f", after-before)
	}
	_ = strings.Contains("/articles/{id}", "{id}")
}

func TestSanitizePath_UnknownRouteFoldsToUnknown(t *testing.T) {
	before := testutil.ToFloat64(shopmetrics.HTTPRequestsTotal.WithLabelValues("GET", "unknown", "404"))

	handler := shopmetrics.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	// Scanner-style path — must collapse to "unknown" to keep cardinality bounded.
	req := httptest.NewRequest("GET", "/random-path-that-does-not-exist-1234", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	after := testutil.ToFloat64(shopmetrics.HTTPRequestsTotal.WithLabelValues("GET", "unknown", "404"))
	if after-before != 1 {
		t.Errorf("unknown counter should have incremented by 1, got %f", after-before)
	}
}
