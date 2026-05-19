//go:build integration

package articles_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shophub-project-2026/shop/internal/articles"
	"github.com/shophub-project-2026/shop/internal/db"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func startPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	ctr, err := tcpostgres.Run(ctx,
		"postgres:15-alpine",
		tcpostgres.WithDatabase("testdb"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })

	connStr, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := db.RunMigrations(ctx, pool, db.Migrations, "migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

func TestArticleRepository_CRUD(t *testing.T) {
	pool := startPostgres(t)
	repo := articles.NewPGRepository(pool)
	ctx := context.Background()

	// create
	a, err := repo.Create(ctx, articles.CreateInput{Name: "Widget", Quantity: 10, Price: 9.99})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.Name != "Widget" {
		t.Errorf("name: want Widget, got %s", a.Name)
	}

	// list
	list, err := repo.List(ctx, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len: want 1, got %d", len(list))
	}

	// list with search
	found, err := repo.List(ctx, "idg")
	if err != nil {
		t.Fatalf("List search: %v", err)
	}
	if len(found) != 1 {
		t.Errorf("search 'idg': want 1 result, got %d", len(found))
	}

	// get
	got, err := repo.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != a.ID {
		t.Errorf("id mismatch")
	}

	// update
	qty := 5
	updated, err := repo.Update(ctx, a.ID, articles.UpdateInput{Quantity: &qty})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Quantity != 5 {
		t.Errorf("quantity: want 5, got %d", updated.Quantity)
	}

	// delete
	if err := repo.Delete(ctx, a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// get after delete → not found
	if _, err := repo.Get(ctx, a.ID); err != articles.ErrNotFound {
		t.Errorf("want ErrNotFound after delete, got %v", err)
	}
}
