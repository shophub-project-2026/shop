package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/shophub-project-2026/shop/internal/articles"
	"github.com/shophub-project-2026/shop/internal/config"
	"github.com/shophub-project-2026/shop/internal/db"
	"github.com/shophub-project-2026/shop/internal/orders"
	"github.com/shophub-project-2026/shop/internal/payment"
	"github.com/shophub-project-2026/shop/internal/server"
	"github.com/shophub-project-2026/shop/internal/server/handlers"
	"github.com/shophub-project-2026/shop/internal/tracing"
)

func main() {
	if err := run(); err != nil {
		_, _ = os.Stderr.WriteString("fatal: " + err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg)
	logger.Info("shop service starting",
		"env", cfg.Environment,
		"addr", cfg.HTTPAddr,
		"log_level", cfg.LogLevel,
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tracerShutdown, err := tracing.Init(ctx, cfg.OTLPEndpoint, "shop")
	if err != nil {
		return err
	}
	defer func() { _ = tracerShutdown(context.Background()) }()
	if cfg.OTLPEndpoint != "" {
		logger.Info("tracing enabled", "otlp_endpoint", cfg.OTLPEndpoint)
	}

	// Select the persistence backend. PostgreSQL is the default; Redis is
	// used for shops created with database=light (deployed by the Redis
	// operator), where SHOP_DB_TYPE=redis is injected by the shop-operator.
	var (
		articleRepo articles.Repository
		orderRepo   orders.Repository
		pinger      handlers.Pinger
	)

	switch cfg.DBType {
	case "redis":
		rdb, err := db.OpenRedis(ctx, cfg)
		if err != nil {
			return err
		}
		defer func() { _ = rdb.Close() }()
		logger.Info("connected to redis", "addr", cfg.RedisAddr)
		articleRepo = articles.NewRedisRepository(rdb)
		orderRepo = orders.NewRedisRepository(rdb)
		pinger = db.RedisPinger{Client: rdb}
	default: // "postgres"
		pool, err := db.Open(ctx, cfg)
		if err != nil {
			return err
		}
		defer pool.Close()

		logger.Info("running database migrations")
		if err := db.RunMigrations(ctx, pool, db.Migrations, "migrations"); err != nil {
			return err
		}
		logger.Info("migrations applied")

		articleRepo = articles.NewPGRepository(pool)
		orderRepo = orders.NewPGRepository(pool)
		pinger = pool
	}

	// Ethereum client is optional — payment endpoints are disabled when
	// SHOP_ETH_RPC_URL is not configured (e.g. local dev without testnet).
	var ethClient payment.EthClient
	if cfg.EthRPCURL != "" {
		c, err := payment.NewEthClient(ctx, cfg.EthRPCURL)
		if err != nil {
			return err
		}
		ethClient = c
		logger.Info("ethereum client connected", "rpc", cfg.EthRPCURL)
	} else {
		logger.Info("SHOP_ETH_RPC_URL not set, payment endpoints disabled")
	}

	srv := server.New(cfg, logger, articleRepo, orderRepo, pinger, ethClient)
	return srv.Run(ctx)
}

func newLogger(cfg *config.Config) *slog.Logger {
	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.Environment == "development" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}
