package orders

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when an order does not exist.
var ErrNotFound = errors.New("order not found")

type pgRepository struct {
	pool *pgxpool.Pool
}

// NewPGRepository creates a PostgreSQL-backed Repository.
func NewPGRepository(pool *pgxpool.Pool) Repository {
	return &pgRepository{pool: pool}
}

func (r *pgRepository) Create(ctx context.Context, in CreateInput) (*Order, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var total float64
	for _, item := range in.Items {
		total += item.UnitPrice * float64(item.Quantity)
	}

	var o Order
	err = tx.QueryRow(ctx,
		`INSERT INTO orders (wallet_address, total_amount)
		 VALUES ($1, $2)
		 RETURNING id, wallet_address, total_amount, tx_hash, status, created_at`,
		in.WalletAddress, total,
	).Scan(&o.ID, &o.WalletAddress, &o.TotalAmount, &o.TxHash, &o.Status, &o.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert order: %w", err)
	}

	for _, item := range in.Items {
		var oi OrderItem
		err = tx.QueryRow(ctx,
			`INSERT INTO order_items (order_id, article_id, quantity, unit_price)
			 VALUES ($1, $2, $3, $4)
			 RETURNING id, order_id, article_id, quantity, unit_price`,
			o.ID, item.ArticleID, item.Quantity, item.UnitPrice,
		).Scan(&oi.ID, &oi.OrderID, &oi.ArticleID, &oi.Quantity, &oi.UnitPrice)
		if err != nil {
			return nil, fmt.Errorf("insert order_item: %w", err)
		}
		o.Items = append(o.Items, oi)

		tag, err := tx.Exec(ctx,
			`UPDATE articles SET quantity = quantity - $1, updated_at = NOW()
			 WHERE id = $2 AND quantity >= $1`,
			item.Quantity, item.ArticleID,
		)
		if err != nil {
			return nil, fmt.Errorf("decrement stock: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return nil, fmt.Errorf("insufficient stock for article %s", item.ArticleID)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return &o, nil
}

func (r *pgRepository) List(ctx context.Context, limit, offset int) ([]Order, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM orders`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count orders: %w", err)
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, wallet_address, total_amount, tx_hash, status, created_at
		 FROM orders ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list orders: %w", err)
	}
	defer rows.Close()

	var result []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.WalletAddress, &o.TotalAmount, &o.TxHash, &o.Status, &o.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan order: %w", err)
		}
		result = append(result, o)
	}
	return result, total, rows.Err()
}

func (r *pgRepository) Get(ctx context.Context, id uuid.UUID) (*Order, error) {
	var o Order
	err := r.pool.QueryRow(ctx,
		`SELECT id, wallet_address, total_amount, tx_hash, status, created_at
		 FROM orders WHERE id = $1`,
		id,
	).Scan(&o.ID, &o.WalletAddress, &o.TotalAmount, &o.TxHash, &o.Status, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get order: %w", err)
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, order_id, article_id, quantity, unit_price
		 FROM order_items WHERE order_id = $1`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("list order_items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item OrderItem
		if err := rows.Scan(&item.ID, &item.OrderID, &item.ArticleID, &item.Quantity, &item.UnitPrice); err != nil {
			return nil, fmt.Errorf("scan order_item: %w", err)
		}
		o.Items = append(o.Items, item)
	}
	return &o, rows.Err()
}

func (r *pgRepository) FindPendingByWallet(ctx context.Context, wallet string) (*Order, error) {
	var o Order
	err := r.pool.QueryRow(ctx,
		`SELECT id, wallet_address, total_amount, tx_hash, status, created_at
		 FROM orders
		 WHERE wallet_address = $1 AND status = $2
		 ORDER BY created_at DESC
		 LIMIT 1`,
		wallet, StatusPending,
	).Scan(&o.ID, &o.WalletAddress, &o.TotalAmount, &o.TxHash, &o.Status, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find pending order: %w", err)
	}
	return &o, nil
}

func (r *pgRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string, txHash *string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE orders SET status = $1, tx_hash = $2 WHERE id = $3`,
		status, txHash, id,
	)
	if err != nil {
		return fmt.Errorf("update order status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
