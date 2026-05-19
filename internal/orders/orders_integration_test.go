//go:build integration

package orders_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shophub-project-2026/shop/internal/articles"
	"github.com/shophub-project-2026/shop/internal/cart"
	"github.com/shophub-project-2026/shop/internal/db"
	"github.com/shophub-project-2026/shop/internal/orders"
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

func TestOrderRepository_CreateAndList(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()

	articleRepo := articles.NewPGRepository(pool)
	orderRepo := orders.NewPGRepository(pool)
	cartStore := cart.NewStore()

	// seed an article
	a, err := articleRepo.Create(ctx, articles.CreateInput{Name: "Gadget", Quantity: 10, Price: 25.00})
	if err != nil {
		t.Fatalf("create article: %v", err)
	}

	// add to cart
	cartStore.Add("0xWallet1", a.ID, 2)

	// build order input from cart
	cartData := cartStore.Get("0xWallet1")
	input := orders.CreateInput{WalletAddress: "0xWallet1"}
	for _, item := range cartData.Items {
		ar, _ := articleRepo.Get(ctx, item.ArticleID)
		input.Items = append(input.Items, orders.ItemInput{
			ArticleID: item.ArticleID,
			Quantity:  item.Quantity,
			UnitPrice: ar.Price,
		})
	}

	order, err := orderRepo.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create order: %v", err)
	}
	if order.Status != orders.StatusPending {
		t.Errorf("status: want pending, got %s", order.Status)
	}
	if order.TotalAmount != 50.00 {
		t.Errorf("total: want 50.00, got %f", order.TotalAmount)
	}

	// verify stock decremented
	updated, _ := articleRepo.Get(ctx, a.ID)
	if updated.Quantity != 8 {
		t.Errorf("stock: want 8 remaining, got %d", updated.Quantity)
	}

	// list
	list, total, err := orderRepo.List(ctx, 10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 {
		t.Errorf("total: want 1, got %d", total)
	}
	if len(list) != 1 {
		t.Fatalf("list len: want 1, got %d", len(list))
	}

	// update status
	hash := "0xTXHASH"
	if err := orderRepo.UpdateStatus(ctx, order.ID, orders.StatusPaid, &hash); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, _ := orderRepo.Get(ctx, order.ID)
	if got.Status != orders.StatusPaid {
		t.Errorf("status after update: want paid, got %s", got.Status)
	}
}

func TestOrderRepository_InsufficientStock(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()

	articleRepo := articles.NewPGRepository(pool)
	orderRepo := orders.NewPGRepository(pool)

	a, _ := articleRepo.Create(ctx, articles.CreateInput{Name: "Rare", Quantity: 1, Price: 100.00})

	_, err := orderRepo.Create(ctx, orders.CreateInput{
		WalletAddress: "0xWallet2",
		Items: []orders.ItemInput{
			{ArticleID: a.ID, Quantity: 5, UnitPrice: 100.00},
		},
	})
	if err == nil {
		t.Fatal("expected error for insufficient stock, got nil")
	}
}
