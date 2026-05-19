package config

import (
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	// All SHOP_* env vars are unset for this test (the test runner clears
	// them via t.Setenv where needed; here we rely on the default empty).
	t.Setenv("SHOP_HTTP_ADDR", "")
	t.Setenv("SHOP_ENV", "")
	t.Setenv("SHOP_LOG_LEVEL", "")
	t.Setenv("SHOP_SHUTDOWN_TIMEOUT_SECONDS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error with all defaults: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr default = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.Environment != "development" {
		t.Errorf("Environment default = %q, want development", cfg.Environment)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel default = %q, want info", cfg.LogLevel)
	}
	if cfg.ShutdownTimeout != 15*time.Second {
		t.Errorf("ShutdownTimeout default = %s, want 15s", cfg.ShutdownTimeout)
	}
}

func TestLoad_OverrideFromEnv(t *testing.T) {
	t.Setenv("SHOP_HTTP_ADDR", ":9090")
	t.Setenv("SHOP_ENV", "production")
	t.Setenv("SHOP_LOG_LEVEL", "warn")
	t.Setenv("SHOP_SHUTDOWN_TIMEOUT_SECONDS", "30")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.HTTPAddr != ":9090" {
		t.Errorf("HTTPAddr = %q, want :9090", cfg.HTTPAddr)
	}
	if cfg.Environment != "production" {
		t.Errorf("Environment = %q, want production", cfg.Environment)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("LogLevel = %q, want warn", cfg.LogLevel)
	}
	if cfg.ShutdownTimeout != 30*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 30s", cfg.ShutdownTimeout)
	}
}

func TestLoad_InvalidEnvironment(t *testing.T) {
	t.Setenv("SHOP_ENV", "qa")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid environment, got nil")
	}
}

func TestLoad_InvalidLogLevel(t *testing.T) {
	t.Setenv("SHOP_LOG_LEVEL", "verbose")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid log level, got nil")
	}
}

func TestLoad_InvalidShutdownTimeout(t *testing.T) {
	t.Setenv("SHOP_SHUTDOWN_TIMEOUT_SECONDS", "not-a-number")
	if _, err := Load(); err == nil {
		t.Fatal("expected parse error for non-numeric shutdown timeout, got nil")
	}
}
