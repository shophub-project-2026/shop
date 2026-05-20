package articles

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// escapeLike escapes the SQL LIKE/ILIKE wildcard chars %, _ and \
// so user input is treated as a literal substring, not a pattern.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// ErrNotFound is returned when an article does not exist.
var ErrNotFound = errors.New("article not found")

type pgRepository struct {
	pool *pgxpool.Pool
}

// NewPGRepository creates a PostgreSQL-backed Repository.
func NewPGRepository(pool *pgxpool.Pool) Repository {
	return &pgRepository{pool: pool}
}

func (r *pgRepository) List(ctx context.Context, search string) ([]Article, error) {
	var rows pgx.Rows
	var err error

	if search != "" {
		rows, err = r.pool.Query(ctx,
			`SELECT id, name, quantity, price, created_at, updated_at
			 FROM articles
			 WHERE name ILIKE $1 ESCAPE '\'
			 ORDER BY created_at DESC`,
			"%"+escapeLike(search)+"%",
		)
	} else {
		rows, err = r.pool.Query(ctx,
			`SELECT id, name, quantity, price, created_at, updated_at
			 FROM articles
			 ORDER BY created_at DESC`,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("query articles: %w", err)
	}
	defer rows.Close()

	var result []Article
	for rows.Next() {
		var a Article
		if err := rows.Scan(&a.ID, &a.Name, &a.Quantity, &a.Price, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan article: %w", err)
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func (r *pgRepository) Get(ctx context.Context, id uuid.UUID) (*Article, error) {
	var a Article
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, quantity, price, created_at, updated_at
		 FROM articles WHERE id = $1`,
		id,
	).Scan(&a.ID, &a.Name, &a.Quantity, &a.Price, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get article: %w", err)
	}
	return &a, nil
}

func (r *pgRepository) Create(ctx context.Context, in CreateInput) (*Article, error) {
	var a Article
	err := r.pool.QueryRow(ctx,
		`INSERT INTO articles (name, quantity, price)
		 VALUES ($1, $2, $3)
		 RETURNING id, name, quantity, price, created_at, updated_at`,
		in.Name, in.Quantity, in.Price,
	).Scan(&a.ID, &a.Name, &a.Quantity, &a.Price, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create article: %w", err)
	}
	return &a, nil
}

func (r *pgRepository) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Article, error) {
	var a Article
	err := r.pool.QueryRow(ctx,
		`UPDATE articles
		 SET name      = COALESCE(NULLIF($1, ''), name),
		     quantity  = COALESCE($2, quantity),
		     price     = COALESCE($3, price),
		     updated_at = NOW()
		 WHERE id = $4
		 RETURNING id, name, quantity, price, created_at, updated_at`,
		in.Name, in.Quantity, in.Price, id,
	).Scan(&a.ID, &a.Name, &a.Quantity, &a.Price, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update article: %w", err)
	}
	return &a, nil
}

func (r *pgRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM articles WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete article: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
