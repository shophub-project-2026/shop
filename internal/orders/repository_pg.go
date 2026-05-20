package orders

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when an order does not exist.
var ErrNotFound = errors.New("order not found")

// ErrTxHashReused is returned when UpdateStatus would assign a tx_hash that
// is already linked to another order — i.e. someone is trying to settle a
// second order with the same on-chain payment.
var ErrTxHashReused = errors.New("tx_hash already used by another order")

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
	// Use a snapshot transaction so the count and the page belong to the
	// same point-in-time view of the orders table.
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var total int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM orders`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count orders: %w", err)
	}

	rows, err := tx.Query(ctx,
		`SELECT id, wallet_address, total_amount, tx_hash, status, created_at
		 FROM orders ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list orders: %w", err)
	}

	var (
		result []Order
		ids    []uuid.UUID
	)
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.WalletAddress, &o.TotalAmount, &o.TxHash, &o.Status, &o.CreatedAt); err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("scan order: %w", err)
		}
		result = append(result, o)
		ids = append(ids, o.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	if len(ids) > 0 {
		itemRows, err := tx.Query(ctx,
			`SELECT id, order_id, article_id, quantity, unit_price
			 FROM order_items WHERE order_id = ANY($1)`,
			ids,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("list order_items: %w", err)
		}
		byOrder := make(map[uuid.UUID][]OrderItem, len(result))
		for itemRows.Next() {
			var item OrderItem
			if err := itemRows.Scan(&item.ID, &item.OrderID, &item.ArticleID, &item.Quantity, &item.UnitPrice); err != nil {
				itemRows.Close()
				return nil, 0, fmt.Errorf("scan order_item: %w", err)
			}
			byOrder[item.OrderID] = append(byOrder[item.OrderID], item)
		}
		itemRows.Close()
		if err := itemRows.Err(); err != nil {
			return nil, 0, err
		}
		for i := range result {
			result[i].Items = byOrder[result[i].ID]
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("commit list tx: %w", err)
	}
	return result, total, nil
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
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return ErrTxHashReused
		}
		return fmt.Errorf("update order status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
