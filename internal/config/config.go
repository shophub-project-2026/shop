// Package config loads and validates the Shop application configuration
// from environment variables. All values come from the process environment
// so the same binary can run in dev, CI and production without rebuilds.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all runtime configuration for the Shop service.
type Config struct {
	// HTTP server bind address (host:port).
	HTTPAddr string

	// Shutdown grace period: how long to wait for in-flight requests to
	// finish after SIGTERM/SIGINT before forcefully closing connections.
	ShutdownTimeout time.Duration

	// Environment name -- "development", "staging", "production".
	// Drives logger formatting (text in dev, JSON elsewhere) and log level.
	Environment string

	// LogLevel: "debug", "info", "warn", "error".
	LogLevel string

	// Database connection parameters.
	DBHost     string
	DBPort     string
	DBName     string
	DBUser     string
	DBPassword string

	// Admin API key — required in X-Admin-Key header for write endpoints.
	AdminKey string

	// Ethereum / Sepolia testnet configuration.
	EthRPCURL   string  // SHOP_ETH_RPC_URL
	EthWallet   string  // SHOP_ETH_WALLET — recipient address for payments
	EthPriceUSD float64 // SHOP_ETH_PRICE_USD — mock ETH/USD rate
}

// Load reads the configuration from environment variables, applies sane
// defaults and validates required fields.
func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:        getEnv("SHOP_HTTP_ADDR", ":8080"),
		Environment:     getEnv("SHOP_ENV", "development"),
		LogLevel:        getEnv("SHOP_LOG_LEVEL", "info"),
		ShutdownTimeout: 15 * time.Second,
		DBHost:          getEnv("SHOP_DB_HOST", "localhost"),
		DBPort:          getEnv("SHOP_DB_PORT", "5432"),
		DBName:          getEnv("SHOP_DB_NAME", "shop_db"),
		DBUser:          getEnv("SHOP_DB_USER", "shop_user"),
		DBPassword:      getEnv("SHOP_DB_PASSWORD", "shop_password"),
		AdminKey:        getEnv("SHOP_ADMIN_KEY", ""),
		EthRPCURL:       getEnv("SHOP_ETH_RPC_URL", ""),
		EthWallet:       getEnv("SHOP_ETH_WALLET", ""),
		EthPriceUSD:     3000.0, // default mock rate; override with SHOP_ETH_PRICE_USD
	}

	if v := os.Getenv("SHOP_ETH_PRICE_USD"); v != "" {
		price, err := strconv.ParseFloat(v, 64)
		if err != nil || price <= 0 {
			return nil, fmt.Errorf("SHOP_ETH_PRICE_USD must be a positive number")
		}
		cfg.EthPriceUSD = price
	}

	if v := os.Getenv("SHOP_SHUTDOWN_TIMEOUT_SECONDS"); v != "" {
		secs, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("SHOP_SHUTDOWN_TIMEOUT_SECONDS: %w", err)
		}
		cfg.ShutdownTimeout = time.Duration(secs) * time.Second
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.HTTPAddr == "" {
		return errors.New("http address must not be empty")
	}
	switch c.Environment {
	case "development", "staging", "production":
	default:
		return fmt.Errorf("environment %q must be one of development|staging|production", c.Environment)
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log level %q must be one of debug|info|warn|error", c.LogLevel)
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown timeout must be positive, got %s", c.ShutdownTimeout)
	}
	return nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
