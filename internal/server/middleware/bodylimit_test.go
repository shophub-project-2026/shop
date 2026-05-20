package middleware_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shophub-project-2026/shop/internal/server/middleware"
)

func TestBodyLimit_AllowsSmall(t *testing.T) {
	h := middleware.BodyLimit(100)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		_, _ = w.Write(b)
	}))

	req := httptest.NewRequest("POST", "/", strings.NewReader("hello"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Body.String() != "hello" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "hello")
	}
}

func TestBodyLimit_RejectsLarge(t *testing.T) {
	h := middleware.BodyLimit(10)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err == nil {
			t.Error("expected read error for oversized body")
		}
	}))
	req := httptest.NewRequest("POST", "/", bytes.NewReader(make([]byte, 1024)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
}
