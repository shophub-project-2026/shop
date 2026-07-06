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
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Server is the top-level HTTP server for the Shop service.
type Server struct {
	httpServer  *http.Server
	health      *handlers.Health
	logger      *slog.Logger
	shutdownTO  time.Duration
	janitorStop chan struct{}
}

// New constructs a Server from the chosen persistence backend. articleRepo
// and orderRepo are backend-agnostic (PostgreSQL or Redis); pinger drives the
// readiness probe and may be nil. ethClient may be nil — payment endpoints
// are disabled when no RPC is configured.
func New(cfg *config.Config, logger *slog.Logger, articleRepo articles.Repository, orderRepo orders.Repository, pinger handlers.Pinger, ethClient payment.EthClient) *Server {
	health := handlers.NewHealth(pinger)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Live)
	mux.HandleFunc("GET /readyz", health.Ready)
	mux.Handle("GET /metrics", promhttp.Handler())

	adminMW := middleware.Admin(cfg.AdminKey)

	// Wrap repos with instrumentation so business metrics stay in sync.
	articleRepo = shopmetrics.NewInstrumentedArticleRepo(context.Background(), articleRepo)

	cartStore := cart.NewStore(cart.WithTTL(30 * time.Minute))
	cartStore.SetSizeObserver(func(activeCarts int) {
		shopmetrics.ActiveCarts.Set(float64(activeCarts))
	})
	janitorStop := make(chan struct{})
	cartStore.StartJanitor(5*time.Minute, janitorStop)

	orderRepo = shopmetrics.NewInstrumentedOrderRepo(orderRepo)

	// JSON API mounted under /api/v1/ so the same path roots (articles, cart,
	// orders) can also be used by the SSR UI without ServeMux pattern collisions.
	// Each handler still registers its routes unprefixed (kept for existing
	// unit tests); the prefix is stripped here at mount time.
	apiMux := http.NewServeMux()
	articles.NewHandler(articleRepo, logger).RegisterRoutes(apiMux, adminMW)
	cart.NewHandler(cartStore, logger).RegisterRoutes(apiMux)
	orders.NewHandler(orderRepo, cartStore, articleRepo, logger).RegisterRoutes(apiMux, adminMW)

	if ethClient != nil {
		// Limit /payment/verify to ~1 req/s per client with a small burst.
		// On-chain verification is expensive (RPC call + receipt fetch); we do
		// not want a single buggy or malicious wallet to drain the connection
		// pool to the upstream Ethereum node.
		paymentLimiter := middleware.NewRateLimiter(1, 5, 10*time.Minute)
		payment.NewHandler(orderRepo, cartStore, ethClient, cfg.EthWallet, cfg.EthPriceUSD, logger).
			RegisterRoutes(apiMux, paymentLimiter.Middleware)
	}

	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", apiMux))

	ui.NewHandler(articleRepo, orderRepo, cartStore, cfg.AdminKey, cfg.EthWallet, cfg.EthPriceUSD, logger).
		RegisterRoutes(mux)

	// Outer-to-inner: otelhttp → security-headers → metrics → body-limit → csrf → logging → mux.
	// otelhttp is outermost to capture W3C trace-context from upstream callers.
	// CSRF wraps after body-limit so r.ParseForm can read a capped body.
	// Skip CSRF for JSON API endpoints hit from cURL or the checkout JS.
	csrf := middleware.CSRF(func(r *http.Request) bool {
		// All JSON API endpoints live under /api/v1/* and are exempt
		// from CSRF: they don't use form posts and clients (browser JS,
		// curl, integration tests) authenticate per request.
		return strings.HasPrefix(r.URL.Path, "/api/v1/")
	})

	handler := otelhttp.NewHandler(
		middleware.SecurityHeaders(
			shopmetrics.Middleware(
				middleware.BodyLimit(middleware.DefaultMaxBodyBytes)(
					csrf(
						middleware.Logging(logger)(mux),
					),
				),
			),
		),
		"shop",
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
