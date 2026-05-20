package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(_ context.Context) error { return f.err }

func TestHealth_Live_AlwaysOK(t *testing.T) {
	h := NewHealth(nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	h.Live(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Live() status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Live() Content-Type = %q, want application/json", ct)
	}
}

func TestHealth_Ready_NotReadyByDefault(t *testing.T) {
	h := NewHealth(nil)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	h.Ready(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Ready() status before SetReady = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHealth_Ready_AfterSetReady_NoDB(t *testing.T) {
	h := NewHealth(nil)
	h.SetReady(true)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	h.Ready(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Ready() status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestHealth_Ready_AfterSetNotReady(t *testing.T) {
	h := NewHealth(nil)
	h.SetReady(true)
	h.SetReady(false) // shutdown begins

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	h.Ready(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Ready() status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHealth_Ready_DBHealthy(t *testing.T) {
	h := NewHealth(fakePinger{})
	h.SetReady(true)
	rec := httptest.NewRecorder()
	h.Ready(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("Ready() with healthy DB = %d, want 200", rec.Code)
	}
}

func TestHealth_Ready_DBUnreachable(t *testing.T) {
	h := NewHealth(fakePinger{err: errors.New("connection refused")})
	h.SetReady(true)
	rec := httptest.NewRecorder()
	h.Ready(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Ready() with broken DB = %d, want 503", rec.Code)
	}
}
