// Package articles provides the domain model, repository interface and
// HTTP handlers for the article catalogue of the Shop service.
package articles

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Article represents a product available for purchase.
type Article struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Quantity  int       `json:"quantity"`
	Price     float64   `json:"price"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateInput carries the fields required to create a new article.
type CreateInput struct {
	Name     string  `json:"name"`
	Quantity int     `json:"quantity"`
	Price    float64 `json:"price"`
}

// UpdateInput carries the fields that may be changed on an existing article.
// Zero values are ignored — only non-zero fields are applied.
type UpdateInput struct {
	Name     string  `json:"name"`
	Quantity *int    `json:"quantity"`
	Price    *float64 `json:"price"`
}

// Repository is the data-access contract for articles.
type Repository interface {
	List(ctx context.Context, search string) ([]Article, error)
	Get(ctx context.Context, id uuid.UUID) (*Article, error)
	Create(ctx context.Context, in CreateInput) (*Article, error)
	Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Article, error)
	Delete(ctx context.Context, id uuid.UUID) error
}
