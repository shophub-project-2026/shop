//go:build integration

package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shophub-project-2026/shop/internal/db"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestRunMigrations(t *testing.T) {
	ctx := context.Background()

	pgCtr, err := postgres.Run(ctx,
		"postgres:15-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = pgCtr.Terminate(ctx) })

	connStr, err := pgCtr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	if err := db.RunMigrations(ctx, pool, db.Migrations, "migrations"); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	for _, tbl := range []string{"articles", "orders", "order_items"} {
		var exists bool
		err := pool.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name=$1)", tbl,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("query table %s: %v", tbl, err)
		}
		if !exists {
			t.Errorf("table %s does not exist after migration", tbl)
		}
	}

	// idempotency check
	if err := db.RunMigrations(ctx, pool, db.Migrations, "migrations"); err != nil {
		t.Fatalf("RunMigrations idempotency: %v", err)
	}
}
