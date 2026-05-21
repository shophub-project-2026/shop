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

func (r *pgRepository) List(ctx context.Context, search string, limit, offset int) ([]Article, int, error) {
	var total int
	if search != "" {
		if err := r.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM articles WHERE name ILIKE $1 ESCAPE '\'`,
			"%"+escapeLike(search)+"%",
		).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count articles: %w", err)
		}
	} else {
		if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM articles`).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count articles: %w", err)
		}
	}

	query, args := buildListQuery(search, limit, offset)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query articles: %w", err)
	}
	defer rows.Close()

	var result []Article
	for rows.Next() {
		var a Article
		if err := rows.Scan(&a.ID, &a.Name, &a.Quantity, &a.Price, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan article: %w", err)
		}
		result = append(result, a)
	}
	return result, total, rows.Err()
}

// buildListQuery constructs the SELECT query and args for List.
// limit=0 means no LIMIT clause; otherwise LIMIT and OFFSET are appended.
func buildListQuery(search string, limit, offset int) (string, []any) {
	const cols = `SELECT id, name, quantity, price, created_at, updated_at FROM articles`
	if search != "" {
		base := cols + ` WHERE name ILIKE $1 ESCAPE '\' ORDER BY created_at DESC`
		if limit > 0 {
			return base + ` LIMIT $2 OFFSET $3`, []any{"%" + escapeLike(search) + "%", limit, offset}
		}
		return base, []any{"%" + escapeLike(search) + "%"}
	}
	base := cols + ` ORDER BY created_at DESC`
	if limit > 0 {
		return base + ` LIMIT $1 OFFSET $2`, []any{limit, offset}
	}
	return base, nil
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
