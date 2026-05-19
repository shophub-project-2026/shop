// Package orders provides the domain model, repository and HTTP handlers
// for purchase orders in the Shop service.
package orders

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Status values for an order.
const (
	StatusPending = "pending"
	StatusPaid    = "paid"
	StatusFailed  = "failed"
)

// Order is a purchase record created from a customer's cart.
type Order struct {
	ID            uuid.UUID   `json:"id"`
	WalletAddress string      `json:"wallet_address"`
	TotalAmount   float64     `json:"total_amount"`
	TxHash        *string     `json:"tx_hash"`
	Status        string      `json:"status"`
	Items         []OrderItem `json:"items"`
	CreatedAt     time.Time   `json:"created_at"`
}

// OrderItem is one line of an Order.
type OrderItem struct {
	ID        uuid.UUID `json:"id"`
	OrderID   uuid.UUID `json:"order_id"`
	ArticleID uuid.UUID `json:"article_id"`
	Quantity  int       `json:"quantity"`
	UnitPrice float64   `json:"unit_price"`
}

// CreateInput carries the wallet address whose cart is checked out.
type CreateInput struct {
	WalletAddress string
	Items         []ItemInput
}

// ItemInput is one cart line resolved to a price.
type ItemInput struct {
	ArticleID uuid.UUID
	Quantity  int
	UnitPrice float64
}

// Repository is the data-access contract for orders.
type Repository interface {
	Create(ctx context.Context, in CreateInput) (*Order, error)
	List(ctx context.Context, limit, offset int) ([]Order, int, error)
	Get(ctx context.Context, id uuid.UUID) (*Order, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string, txHash *string) error
}
