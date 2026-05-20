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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shophub-project-2026/shop/internal/articles"
	"github.com/shophub-project-2026/shop/internal/cart"
	"github.com/shophub-project-2026/shop/internal/config"
	"github.com/shophub-project-2026/shop/internal/orders"
	"github.com/shophub-project-2026/shop/internal/payment"
	"github.com/shophub-project-2026/shop/internal/server/handlers"
	"github.com/shophub-project-2026/shop/internal/server/middleware"
	"github.com/shophub-project-2026/shop/internal/ui"
)

// Server is the top-level HTTP server for the Shop service.
type Server struct {
	httpServer *http.Server
	health     *handlers.Health
	logger     *slog.Logger
	shutdownTO time.Duration
}

// New constructs a Server bound to cfg.HTTPAddr with all routes wired.
// ethClient may be nil — payment endpoints are disabled when no RPC is configured.
func New(cfg *config.Config, logger *slog.Logger, pool *pgxpool.Pool, ethClient payment.EthClient) *Server {
	health := handlers.NewHealth()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Live)
	mux.HandleFunc("GET /readyz", health.Ready)

	adminMW := middleware.Admin(cfg.AdminKey)

	articleRepo := articles.NewPGRepository(pool)
	articles.NewHandler(articleRepo, logger).RegisterRoutes(mux, adminMW)

	cartStore := cart.NewStore()
	cart.NewHandler(cartStore, logger).RegisterRoutes(mux)

	orderRepo := orders.NewPGRepository(pool)
	orders.NewHandler(orderRepo, cartStore, articleRepo, logger).RegisterRoutes(mux, adminMW)

	if ethClient != nil {
		payment.NewHandler(orderRepo, cartStore, ethClient, cfg.EthWallet, cfg.EthPriceUSD, logger).
			RegisterRoutes(mux)
	}

	ui.NewHandler(articleRepo, orderRepo, cartStore, cfg.AdminKey, cfg.EthWallet, cfg.EthPriceUSD, logger).
		RegisterRoutes(mux)

	handler := middleware.Logging(logger)(mux)

	return &Server{
		httpServer: &http.Server{
			Addr:              cfg.HTTPAddr,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
		},
		health:     health,
		logger:     logger,
		shutdownTO: cfg.ShutdownTimeout,
	}
}

// Run starts the HTTP listener and blocks until ctx is cancelled.
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
