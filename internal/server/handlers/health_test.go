package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth_Live_AlwaysOK(t *testing.T) {
	h := NewHealth()
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
	h := NewHealth()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	h.Ready(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Ready() status before SetReady = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestHealth_Ready_AfterSetReady(t *testing.T) {
	h := NewHealth()
	h.SetReady(true)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	h.Ready(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Ready() status after SetReady(true) = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestHealth_Ready_AfterSetNotReady(t *testing.T) {
	h := NewHealth()
	h.SetReady(true)
	h.SetReady(false) // shutdown begins

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	h.Ready(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Ready() status after SetReady(false) = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
