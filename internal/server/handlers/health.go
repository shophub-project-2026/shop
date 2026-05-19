// Package handlers contains HTTP handlers grouped by concern (health,
// articles, orders, ...). Handlers in this package never reach into
// transports or persistence directly; they accept dependencies via a
// small constructor and return *http.HandlerFunc-shaped functions.
package handlers

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
)

// Health exposes liveness and readiness endpoints. The ready flag is
// flipped on by the server once startup completes (e.g. DB pool is
// ready) and flipped off during graceful shutdown so the load balancer
// can drain traffic.
type Health struct {
	ready atomic.Bool
}

// NewHealth returns a Health handler set in the not-ready state.
func NewHealth() *Health {
	return &Health{}
}

// SetReady toggles the readiness state.
func (h *Health) SetReady(ready bool) {
	h.ready.Store(ready)
}

// Live always returns 200. The process being able to respond is the
// signal the kubelet uses to decide whether to restart the container.
func (h *Health) Live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
}

// Ready returns 200 only after SetReady(true) has been called.
// Returns 503 during startup and after graceful shutdown begins.
func (h *Health) Ready(w http.ResponseWriter, _ *http.Request) {
	if !h.ready.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not-ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
