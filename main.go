package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/shophub-project-2026/shop/internal/config"
	"github.com/shophub-project-2026/shop/internal/server"
)

func main() {
	if err := run(); err != nil {
		// run() handles its own logging where possible. This is the last
		// line of defence -- print to stderr and exit non-zero so the
		// process supervisor (Kubernetes) sees the failure.
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

	srv := server.New(cfg, logger)
	return srv.Run(ctx)
}

// newLogger returns a slog.Logger whose format and level are derived
// from cfg. Text handler in development for human-readable output;
// JSON handler elsewhere so log aggregators can parse fields.
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
