package articles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Redis key layout:
//
//	article:<id>      string  JSON-encoded Article
//	articles:index    zset    score = CreatedAt UnixNano, member = <id>
//
// The sorted set gives a stable created_at ordering for listing and
// pagination; individual articles are fetched with MGET. The catalogue of
// a single shop is small, so search falls back to loading the index and
// filtering in process — there is no secondary text index.
const (
	redisArticleKeyPrefix = "article:"
	redisArticleIndexKey  = "articles:index"
)

type redisRepository struct {
	rdb redis.UniversalClient
}

// NewRedisRepository creates a Redis-backed Repository.
func NewRedisRepository(rdb redis.UniversalClient) Repository {
	return &redisRepository{rdb: rdb}
}

func articleKey(id uuid.UUID) string { return redisArticleKeyPrefix + id.String() }

func (r *redisRepository) List(ctx context.Context, search string, limit, offset int) ([]Article, int, error) {
	// Newest first, matching the Postgres ORDER BY created_at DESC.
	ids, err := r.rdb.ZRevRange(ctx, redisArticleIndexKey, 0, -1).Result()
	if err != nil {
		return nil, 0, fmt.Errorf("list article index: %w", err)
	}
	all, err := r.loadByIDs(ctx, ids)
	if err != nil {
		return nil, 0, err
	}

	if search != "" {
		needle := strings.ToLower(search)
		filtered := all[:0]
		for _, a := range all {
			if strings.Contains(strings.ToLower(a.Name), needle) {
				filtered = append(filtered, a)
			}
		}
		all = filtered
	}

	total := len(all)
	if offset >= total {
		return nil, total, nil
	}
	all = all[offset:]
	if limit > 0 && limit < len(all) {
		all = all[:limit]
	}
	return all, total, nil
}

// loadByIDs fetches articles for the given ids preserving order. Missing
// keys (e.g. concurrently deleted) are skipped.
func (r *redisRepository) loadByIDs(ctx context.Context, ids []string) ([]Article, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = redisArticleKeyPrefix + id
	}
	vals, err := r.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("mget articles: %w", err)
	}
	result := make([]Article, 0, len(vals))
	for _, v := range vals {
		s, ok := v.(string)
		if !ok {
			continue // nil — key vanished between ZRange and MGet
		}
		var a Article
		if err := json.Unmarshal([]byte(s), &a); err != nil {
			return nil, fmt.Errorf("decode article: %w", err)
		}
		result = append(result, a)
	}
	return result, nil
}

func (r *redisRepository) Get(ctx context.Context, id uuid.UUID) (*Article, error) {
	s, err := r.rdb.Get(ctx, articleKey(id)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get article: %w", err)
	}
	var a Article
	if err := json.Unmarshal([]byte(s), &a); err != nil {
		return nil, fmt.Errorf("decode article: %w", err)
	}
	return &a, nil
}

func (r *redisRepository) Create(ctx context.Context, in CreateInput) (*Article, error) {
	now := time.Now().UTC()
	a := Article{
		ID:        uuid.New(),
		Name:      in.Name,
		Quantity:  in.Quantity,
		Price:     in.Price,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := r.save(ctx, &a, true); err != nil {
		return nil, fmt.Errorf("create article: %w", err)
	}
	return &a, nil
}

func (r *redisRepository) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*Article, error) {
	a, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	// Mirror the Postgres COALESCE/NULLIF semantics: only non-zero fields
	// overwrite the stored value.
	if in.Name != "" {
		a.Name = in.Name
	}
	if in.Quantity != nil {
		a.Quantity = *in.Quantity
	}
	if in.Price != nil {
		a.Price = *in.Price
	}
	a.UpdatedAt = time.Now().UTC()
	if err := r.save(ctx, a, false); err != nil {
		return nil, fmt.Errorf("update article: %w", err)
	}
	return a, nil
}

// save writes the article JSON and (when indexed) its index entry. The index
// score is the creation time so ordering is stable across updates.
func (r *redisRepository) save(ctx context.Context, a *Article, addIndex bool) error {
	payload, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = r.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.Set(ctx, articleKey(a.ID), payload, 0)
		if addIndex {
			p.ZAdd(ctx, redisArticleIndexKey, redis.Z{
				Score:  float64(a.CreatedAt.UnixNano()),
				Member: a.ID.String(),
			})
		}
		return nil
	})
	return err
}

func (r *redisRepository) Delete(ctx context.Context, id uuid.UUID) error {
	removed, err := r.rdb.Del(ctx, articleKey(id)).Result()
	if err != nil {
		return fmt.Errorf("delete article: %w", err)
	}
	if removed == 0 {
		return ErrNotFound
	}
	if err := r.rdb.ZRem(ctx, redisArticleIndexKey, id.String()).Err(); err != nil {
		return fmt.Errorf("deindex article: %w", err)
	}
	return nil
}
