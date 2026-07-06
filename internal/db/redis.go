package db

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/shophub-project-2026/shop/internal/config"
)

// OpenRedis creates a Redis client from cfg and verifies connectivity with a
// PING. It is used when cfg.DBType == "redis" (shops created with the
// database=light option, backed by the Redis operator).
func OpenRedis(ctx context.Context, cfg *config.Config) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis ping %s: %w", cfg.RedisAddr, err)
	}
	return client, nil
}

// RedisPinger adapts a Redis client to the handlers.Pinger interface used by
// the readiness probe (Ping(ctx) error).
type RedisPinger struct {
	Client redis.UniversalClient
}

// Ping reports whether Redis is reachable.
func (p RedisPinger) Ping(ctx context.Context) error {
	return p.Client.Ping(ctx).Err()
}
