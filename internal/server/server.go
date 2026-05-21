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
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/shophub-project-2026/shop/internal/articles"
	"github.com/shophub-project-2026/shop/internal/cart"
	"github.com/shophub-project-2026/shop/internal/config"
	shopmetrics "github.com/shophub-project-2026/shop/internal/metrics"
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
	janitorStop chan struct{}
}

// New constructs a Server bound to cfg.HTTPAddr with all routes wired.
// ethClient may be nil — payment endpoints are disabled when no RPC is configured.
func New(cfg *config.Config, logger *slog.Logger, pool *pgxpool.Pool, ethClient payment.EthClient) *Server {
	health := handlers.NewHealth(pool)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Live)
	mux.HandleFunc("GET /readyz", health.Ready)
	mux.Handle("GET /metrics", promhttp.Handler())

	adminMW := middleware.Admin(cfg.AdminKey)

	// Wrap repos with instrumentation so business metrics stay in sync.
	baseArticleRepo := articles.NewPGRepository(pool)
	articleRepo := shopmetrics.NewInstrumentedArticleRepo(context.Background(), baseArticleRepo)

	articles.NewHandler(articleRepo, logger).RegisterRoutes(mux, adminMW)

	cartStore := cart.NewStore(cart.WithTTL(30 * time.Minute))
	cartStore.SetSizeObserver(func(activeCarts int) {
		shopmetrics.ActiveCarts.Set(float64(activeCarts))
	})
	janitorStop := make(chan struct{})
	cartStore.StartJanitor(5*time.Minute, janitorStop)
	cart.NewHandler(cartStore, logger).RegisterRoutes(mux)

	baseOrderRepo := orders.NewPGRepository(pool)
	orderRepo := shopmetrics.NewInstrumentedOrderRepo(baseOrderRepo)
	orders.NewHandler(orderRepo, cartStore, articleRepo, logger).RegisterRoutes(mux, adminMW)

	if ethClient != nil {
		// Limit /payment/verify to ~1 req/s per client with a small burst.
		// On-chain verification is expensive (RPC call + receipt fetch); we do
		// not want a single buggy or malicious wallet to drain the connection
		// pool to the upstream Ethereum node.
		paymentLimiter := middleware.NewRateLimiter(1, 5, 10*time.Minute)
		payment.NewHandler(orderRepo, cartStore, ethClient, cfg.EthWallet, cfg.EthPriceUSD, logger).
			RegisterRoutes(mux, paymentLimiter.Middleware)
	}

	ui.NewHandler(articleRepo, orderRepo, cartStore, cfg.AdminKey, cfg.EthWallet, cfg.EthPriceUSD, logger).
		RegisterRoutes(mux)

	// Outer-to-inner: security-headers → metrics → body-limit → csrf → logging → mux.
	// CSRF wraps after body-limit so r.ParseForm can read a capped body.
	// Skip CSRF for non-browser endpoints: /payment/verify (JSON API hit from
	// the checkout JS with no cookie context), /cart and /articles JSON API
	// routes.
	csrf := middleware.CSRF(func(r *http.Request) bool {
		switch r.URL.Path {
		case "/payment/verify":
			return true
		}
		// JSON API routes on /articles use X-Admin-Key auth and are usually
		// hit from cURL or the CLI, not from a browser form.
		if strings.HasPrefix(r.URL.Path, "/articles") && r.Header.Get("Content-Type") == "application/json" {
			return true
		}
		// Cart JSON API likewise.
		if r.URL.Path == "/cart" && r.Header.Get("Content-Type") == "application/json" {
			return true
		}
		// Orders JSON API (POST /orders).
		if r.URL.Path == "/orders" && r.Header.Get("Content-Type") == "application/json" {
			return true
		}
		return false
	})

	handler := middleware.SecurityHeaders(
		shopmetrics.Middleware(
			middleware.BodyLimit(middleware.DefaultMaxBodyBytes)(
				csrf(
					middleware.Logging(logger)(mux),
				),
			),
		),
	)

	return &Server{
		httpServer: &http.Server{
			Addr:              cfg.HTTPAddr,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
		},
		health:      health,
		logger:      logger,
		shutdownTO:  cfg.ShutdownTimeout,
		janitorStop: janitorStop,
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
		if s.janitorStop != nil {
			close(s.janitorStop)
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTO)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown failed: %w", err)
		}
		s.logger.Info("http server stopped cleanly")
		return nil
	}
}
