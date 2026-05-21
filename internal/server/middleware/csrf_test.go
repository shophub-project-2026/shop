package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/shophub-project-2026/shop/internal/server/middleware"
)

func TestCSRF_AllowsSafeMethodsAndIssuesCookie(t *testing.T) {
	h := middleware.CSRF(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("GET status = %d, want 200", rec.Code)
	}
	var got string
	for _, c := range rec.Result().Cookies() {
		if c.Name == middleware.CSRFCookieName {
			got = c.Value
		}
	}
	if len(got) != 64 {
		t.Errorf("csrf cookie value len = %d, want 64", len(got))
	}
}

func TestCSRF_RejectsPostWithoutToken(t *testing.T) {
	h := middleware.CSRF(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("downstream handler must not be reached without a valid token")
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", nil))

	if rec.Code != http.StatusForbidden {
		t.Errorf("POST without token = %d, want 403", rec.Code)
	}
}

func TestCSRF_AcceptsPostWithMatchingToken(t *testing.T) {
	h := middleware.CSRF(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	token := strings.Repeat("a", 64)
	form := url.Values{middleware.CSRFFieldName: {token}}

	req := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: middleware.CSRFCookieName, Value: token})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("POST with matching token = %d, want 200", rec.Code)
	}
}

func TestCSRF_SkipFuncBypasses(t *testing.T) {
	h := middleware.CSRF(func(r *http.Request) bool {
		return r.URL.Path == "/payment/verify"
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/payment/verify", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("Skip() should bypass CSRF for /payment/verify, got %d", rec.Code)
	}
}
