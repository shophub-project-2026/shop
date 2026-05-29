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

	// Database pool tuning. Defaults are conservative; override per-env.
	DBMaxConns           int32
	DBMinConns           int32
	DBMaxConnLifetime    time.Duration
	DBMaxConnIdleTime    time.Duration
	DBHealthCheckPeriod  time.Duration

	// Admin API key — required in X-Admin-Key header for write endpoints.
	AdminKey string

	// DBType selects the persistence backend: "postgres" (default) or
	// "redis". The shop-operator sets this to "redis" for shops created
	// with database=light, where a Redis instance is deployed by the
	// Redis operator instead of a CloudNativePG PostgreSQL cluster.
	DBType string // SHOP_DB_TYPE

	// Redis connection parameters (used when DBType == "redis").
	RedisAddr     string // SHOP_REDIS_ADDR — host:port
	RedisPassword string // SHOP_REDIS_PASSWORD
	RedisDB       int    // SHOP_REDIS_DB — logical DB index

	// Ethereum / Sepolia testnet configuration.
	EthRPCURL   string  // SHOP_ETH_RPC_URL
	EthWallet   string  // SHOP_ETH_WALLET — recipient address for payments
	EthPriceUSD float64 // SHOP_ETH_PRICE_USD — mock ETH/USD rate

	// OTLPEndpoint is the full URL of the OTLP/HTTP trace collector,
	// e.g. "http://otel-collector:4318". Leave empty to disable tracing.
	OTLPEndpoint string // SHOP_OTLP_ENDPOINT
}

// Load reads the configuration from environment variables, applies sane
// defaults and validates required fields.
func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:        getEnv("SHOP_HTTP_ADDR", ":8080"),
		Environment:     getEnv("SHOP_ENV", "development"),
		LogLevel:        getEnv("SHOP_LOG_LEVEL", "info"),
		ShutdownTimeout: 15 * time.Second,
		DBType:          getEnv("SHOP_DB_TYPE", "postgres"),
		DBHost:          getEnv("SHOP_DB_HOST", "localhost"),
		DBPort:          getEnv("SHOP_DB_PORT", "5432"),
		DBName:          getEnv("SHOP_DB_NAME", "shop_db"),
		DBUser:          getEnv("SHOP_DB_USER", "shop_user"),
		DBPassword:      getEnv("SHOP_DB_PASSWORD", "shop_password"),
		RedisAddr:       getEnv("SHOP_REDIS_ADDR", "localhost:6379"),
		RedisPassword:   getEnv("SHOP_REDIS_PASSWORD", ""),
		AdminKey:        getEnv("SHOP_ADMIN_KEY", ""),
		EthRPCURL:    getEnv("SHOP_ETH_RPC_URL", ""),
		EthWallet:    getEnv("SHOP_ETH_WALLET", ""),
		EthPriceUSD:  3000.0, // default mock rate; override with SHOP_ETH_PRICE_USD
		OTLPEndpoint: getEnv("SHOP_OTLP_ENDPOINT", ""),

		DBMaxConns:          25,
		DBMinConns:          2,
		DBMaxConnLifetime:   30 * time.Minute,
		DBMaxConnIdleTime:   5 * time.Minute,
		DBHealthCheckPeriod: 1 * time.Minute,
	}

	for _, spec := range []struct {
		name string
		set  func(int) error
	}{
		{"SHOP_DB_MAX_CONNS", func(n int) error {
			if n <= 0 {
				return fmt.Errorf("SHOP_DB_MAX_CONNS must be > 0")
			}
			cfg.DBMaxConns = int32(n)
			return nil
		}},
		{"SHOP_DB_MIN_CONNS", func(n int) error {
			if n < 0 {
				return fmt.Errorf("SHOP_DB_MIN_CONNS must be >= 0")
			}
			cfg.DBMinConns = int32(n)
			return nil
		}},
		{"SHOP_REDIS_DB", func(n int) error {
			if n < 0 {
				return fmt.Errorf("SHOP_REDIS_DB must be >= 0")
			}
			cfg.RedisDB = n
			return nil
		}},
	} {
		if v := os.Getenv(spec.name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", spec.name, err)
			}
			if err := spec.set(n); err != nil {
				return nil, err
			}
		}
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
	switch c.DBType {
	case "postgres", "redis":
	default:
		return fmt.Errorf("database type %q must be one of postgres|redis", c.DBType)
	}
	if c.DBType == "redis" && c.RedisAddr == "" {
		return errors.New("SHOP_REDIS_ADDR must be set when SHOP_DB_TYPE=redis")
	}
	return nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
