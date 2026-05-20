package metrics

import (
	"context"

	"github.com/google/uuid"
	"github.com/shophub-project-2026/shop/internal/articles"
	"github.com/shophub-project-2026/shop/internal/orders"
)

// InstrumentedArticleRepo wraps articles.Repository and keeps
// ArticlesTotal gauge in sync.
type InstrumentedArticleRepo struct {
	articles.Repository
}

// NewInstrumentedArticleRepo wraps repo and sets the initial gauge value.
func NewInstrumentedArticleRepo(ctx context.Context, repo articles.Repository) *InstrumentedArticleRepo {
	// seed current count (best-effort — ignore error at startup)
	if list, err := repo.List(ctx, ""); err == nil {
		ArticlesTotal.Set(float64(len(list)))
	}
	return &InstrumentedArticleRepo{Repository: repo}
}

func (r *InstrumentedArticleRepo) Create(ctx context.Context, in articles.CreateInput) (*articles.Article, error) {
	a, err := r.Repository.Create(ctx, in)
	if err == nil {
		ArticlesTotal.Inc()
	}
	return a, err
}

func (r *InstrumentedArticleRepo) Delete(ctx context.Context, id uuid.UUID) error {
	err := r.Repository.Delete(ctx, id)
	if err == nil {
		ArticlesTotal.Dec()
	}
	return err
}

// InstrumentedOrderRepo wraps orders.Repository and keeps
// OrdersTotal counter in sync.
type InstrumentedOrderRepo struct {
	orders.Repository
}

// NewInstrumentedOrderRepo wraps repo.
func NewInstrumentedOrderRepo(repo orders.Repository) *InstrumentedOrderRepo {
	return &InstrumentedOrderRepo{Repository: repo}
}

func (r *InstrumentedOrderRepo) Create(ctx context.Context, in orders.CreateInput) (*orders.Order, error) {
	o, err := r.Repository.Create(ctx, in)
	if err == nil {
		OrdersTotal.WithLabelValues(orders.StatusPending).Inc()
	}
	return o, err
}

func (r *InstrumentedOrderRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status string, txHash *string) error {
	err := r.Repository.UpdateStatus(ctx, id, status, txHash)
	if err == nil {
		OrdersTotal.WithLabelValues(status).Inc()
	}
	return err
}
