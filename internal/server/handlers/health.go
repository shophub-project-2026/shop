// Package handlers contains HTTP handlers grouped by concern (health,
// articles, orders, ...). Handlers in this package never reach into
// transports or persistence directly; they accept dependencies via a
// small constructor and return *http.HandlerFunc-shaped functions.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"
)

// Pinger is anything that can be pinged. *pgxpool.Pool satisfies this.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Health exposes liveness and readiness endpoints. The ready flag is
// flipped on by the server once startup completes (e.g. DB pool is
// ready) and flipped off during graceful shutdown so the load balancer
// can drain traffic.
type Health struct {
	ready atomic.Bool
	db    Pinger
}

// NewHealth returns a Health handler set in the not-ready state.
// db may be nil; when nil, readiness depends only on the flag.
func NewHealth(db Pinger) *Health {
	return &Health{db: db}
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

// Ready returns 200 only when the server is marked ready AND, if a DB
// pinger is configured, the database responds within 1s.
// Returns 503 during startup, after graceful shutdown begins, or when
// the database is unreachable.
func (h *Health) Ready(w http.ResponseWriter, r *http.Request) {
	if !h.ready.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not-ready",
			"reason": "starting or draining",
		})
		return
	}
	if h.db != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
		defer cancel()
		if err := h.db.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "not-ready",
				"reason": "database unreachable",
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
