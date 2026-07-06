//go:build integration

package orders_test

import (
	"context"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/shophub-project-2026/shop/internal/articles"
	"github.com/shophub-project-2026/shop/internal/orders"
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

func TestRedisOrderRepository_Lifecycle(t *testing.T) {
	client := startRedis(t)
	ctx := context.Background()

	// Seed an article so the order's stock decrement has something to act on.
	articleRepo := articles.NewRedisRepository(client)
	art, err := articleRepo.Create(ctx, articles.CreateInput{Name: "Widget", Quantity: 10, Price: 2.5})
	if err != nil {
		t.Fatalf("seed article: %v", err)
	}

	repo := orders.NewRedisRepository(client)
	o, err := repo.Create(ctx, orders.CreateInput{
		WalletAddress: "0xabc",
		Items: []orders.ItemInput{
			{ArticleID: art.ID, Quantity: 4, UnitPrice: 2.5},
		},
	})
	if err != nil {
		t.Fatalf("Create order: %v", err)
	}
	if o.Status != orders.StatusPending || o.TotalAmount != 10 {
		t.Errorf("order mismatch: status=%s total=%v", o.Status, o.TotalAmount)
	}
	if len(o.Items) != 1 {
		t.Fatalf("want 1 item, got %d", len(o.Items))
	}

	// Stock must have been decremented.
	got, err := articleRepo.Get(ctx, art.ID)
	if err != nil {
		t.Fatalf("get article: %v", err)
	}
	if got.Quantity != 6 {
		t.Errorf("stock: want 6, got %d", got.Quantity)
	}

	// FindPendingByWallet returns the pending order.
	pending, err := repo.FindPendingByWallet(ctx, "0xabc")
	if err != nil {
		t.Fatalf("FindPendingByWallet: %v", err)
	}
	if pending.ID != o.ID {
		t.Errorf("pending: want %s got %s", o.ID, pending.ID)
	}

	// Settle with a tx hash.
	hash := "0xdeadbeef"
	if err := repo.UpdateStatus(ctx, o.ID, orders.StatusPaid, &hash); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	settled, err := repo.Get(ctx, o.ID)
	if err != nil {
		t.Fatalf("Get settled: %v", err)
	}
	if settled.Status != orders.StatusPaid || settled.TxHash == nil || *settled.TxHash != hash {
		t.Errorf("settled mismatch: %+v", settled)
	}

	// Once paid, it is no longer pending.
	if _, err := repo.FindPendingByWallet(ctx, "0xabc"); err != orders.ErrNotFound {
		t.Errorf("FindPending after paid: want ErrNotFound, got %v", err)
	}

	// Reusing the same tx hash on a different order is rejected.
	o2, err := repo.Create(ctx, orders.CreateInput{
		WalletAddress: "0xabc",
		Items:         []orders.ItemInput{{ArticleID: art.ID, Quantity: 1, UnitPrice: 2.5}},
	})
	if err != nil {
		t.Fatalf("Create order 2: %v", err)
	}
	if err := repo.UpdateStatus(ctx, o2.ID, orders.StatusPaid, &hash); err != orders.ErrTxHashReused {
		t.Errorf("reuse tx hash: want ErrTxHashReused, got %v", err)
	}

	// List returns both orders with a correct total.
	all, total, err := repo.List(ctx, 10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 || len(all) != 2 {
		t.Errorf("list: want total 2, got total=%d len=%d", total, len(all))
	}

	// Insufficient stock is rejected.
	if _, err := repo.Create(ctx, orders.CreateInput{
		WalletAddress: "0xdef",
		Items:         []orders.ItemInput{{ArticleID: art.ID, Quantity: 1000, UnitPrice: 2.5}},
	}); err == nil {
		t.Error("want insufficient-stock error, got nil")
	}
}
