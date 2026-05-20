// Package articles provides the domain model, repository interface and
// HTTP handlers for the article catalogue of the Shop service.
package articles

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Limits enforced by the handlers on user input. These are conservative
// values chosen to prevent obviously abusive payloads — the DB itself has
// no schema-level length cap because that would require recreating tables.
const (
	MaxNameLength   = 200
	MaxSearchLength = 200
	MaxPrice        = 1e9
	MaxQuantity     = 1_000_000
)

// ErrInvalidInput is returned by Validate methods when a CreateInput or
// UpdateInput violates one of the limits above.
var ErrInvalidInput = errors.New("invalid input")

// Validate normalises and validates a CreateInput. It trims whitespace
// from the Name in place.
func (in *CreateInput) Validate() error {
	in.Name = strings.TrimSpace(in.Name)
	switch {
	case in.Name == "":
		return errInvalid("name is required")
	case len(in.Name) > MaxNameLength:
		return errInvalid("name too long")
	case in.Quantity < 0 || in.Quantity > MaxQuantity:
		return errInvalid("quantity out of range")
	case in.Price <= 0 || in.Price > MaxPrice:
		return errInvalid("price out of range")
	}
	return nil
}

// Validate normalises and validates an UpdateInput. Empty / nil fields
// are treated as "unchanged" so partial updates are allowed.
func (in *UpdateInput) Validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if len(in.Name) > MaxNameLength {
		return errInvalid("name too long")
	}
	if in.Quantity != nil && (*in.Quantity < 0 || *in.Quantity > MaxQuantity) {
		return errInvalid("quantity out of range")
	}
	if in.Price != nil && (*in.Price <= 0 || *in.Price > MaxPrice) {
		return errInvalid("price out of range")
	}
	return nil
}

func errInvalid(msg string) error {
	return errInvalidInputf{msg: msg}
}

type errInvalidInputf struct{ msg string }

func (e errInvalidInputf) Error() string { return e.msg }
func (errInvalidInputf) Is(target error) bool {
	return target == ErrInvalidInput
}

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
