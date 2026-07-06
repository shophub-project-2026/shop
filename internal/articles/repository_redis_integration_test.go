//go:build integration

package articles_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/shophub-project-2026/shop/internal/articles"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

func startRedis(t *testing.T) *redis.Client {
	t.Helper()
	ctx := context.Background()

	ctr, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("start redis: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })

	endpoint, err := ctr.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	opts, err := redis.ParseURL(endpoint)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestRedisArticleRepository_CRUD(t *testing.T) {
	client := startRedis(t)
	repo := articles.NewRedisRepository(client)
	ctx := context.Background()

	a, err := repo.Create(ctx, articles.CreateInput{Name: "Widget", Quantity: 10, Price: 9.99})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.Name != "Widget" || a.Quantity != 10 {
		t.Errorf("create mismatch: %+v", a)
	}

	got, err := repo.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != a.ID {
		t.Errorf("get id: want %s got %s", a.ID, got.ID)
	}

	qty := 3
	upd, err := repo.Update(ctx, a.ID, articles.UpdateInput{Quantity: &qty})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if upd.Quantity != 3 || upd.Name != "Widget" {
		t.Errorf("update mismatch: %+v", upd)
	}

	if _, err := repo.Create(ctx, articles.CreateInput{Name: "Gadget", Quantity: 5, Price: 1}); err != nil {
		t.Fatalf("Create 2: %v", err)
	}

	list, total, err := repo.List(ctx, "", 0, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Errorf("list: want total 2, got total=%d len=%d", total, len(list))
	}

	// Search is a case-insensitive substring match.
	found, total, err := repo.List(ctx, "wid", 0, 0)
	if err != nil {
		t.Fatalf("List search: %v", err)
	}
	if total != 1 || len(found) != 1 || found[0].Name != "Widget" {
		t.Errorf("search: want 1 Widget, got total=%d %+v", total, found)
	}

	if err := repo.Delete(ctx, a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(ctx, a.ID); err != articles.ErrNotFound {
		t.Errorf("Get after delete: want ErrNotFound, got %v", err)
	}
	if err := repo.Delete(ctx, uuid.New()); err != articles.ErrNotFound {
		t.Errorf("Delete missing: want ErrNotFound, got %v", err)
	}
}
