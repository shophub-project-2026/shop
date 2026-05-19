// Package server wires the HTTP handlers, middleware and lifecycle of
// the Shop service. It is consumed by main and by integration tests via
// the New constructor.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/shophub-project-2026/shop/internal/config"
	"github.com/shophub-project-2026/shop/internal/server/handlers"
)

// Server is the top-level HTTP server for the Shop service. It owns the
// http.Server, the route mux and the readiness handler so the lifecycle
// (mark ready, mark not-ready, shut down) is coordinated in one place.
type Server struct {
	httpServer *http.Server
	health     *handlers.Health
	logger     *slog.Logger
	shutdownTO time.Duration
}

// New constructs a Server bound to cfg.HTTPAddr with all routes wired.
// It does not start the listener -- call Run to do that.
func New(cfg *config.Config, logger *slog.Logger) *Server {
	health := handlers.NewHealth()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Live)
	mux.HandleFunc("GET /readyz", health.Ready)

	return &Server{
		httpServer: &http.Server{
			Addr:              cfg.HTTPAddr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
		health:     health,
		logger:     logger,
		shutdownTO: cfg.ShutdownTimeout,
	}
}

// Run starts the HTTP listener and blocks until ctx is cancelled (e.g.
// by a SIGTERM handler in main), then performs a graceful shutdown
// bounded by cfg.ShutdownTimeout. The readiness probe is flipped to
// ready right before serving so the load balancer can route to us, and
// flipped to not-ready as soon as ctx is cancelled so it can drain.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("http server starting", "addr", s.httpServer.Addr)
		s.health.SetReady(true)
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("listen: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.logger.Info("shutdown signal received, draining...")
		s.health.SetReady(false)

		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTO)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown failed: %w", err)
		}
		s.logger.Info("http server stopped cleanly")
		return nil
	}
}
