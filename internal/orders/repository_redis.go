package orders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Redis key layout:
//
//	order:<id>                string  JSON-encoded Order (with items)
//	orders:index              zset    score = CreatedAt UnixNano, member = <id>
//	orders:pending:<wallet>   zset    pending orders for a wallet, newest last
//	order:txhash:<hash>       string  <id> — enforces tx_hash uniqueness
//	article:<id>              string  JSON article (shared with the articles repo)
//
// Order creation decrements article stock in the same keyspace as the
// articles repository, so the two backends stay consistent. The decrement
// runs inside an optimistic WATCH/MULTI transaction to avoid overselling
// under concurrent checkouts.
const (
	redisOrderKeyPrefix    = "order:"
	redisOrderIndexKey     = "orders:index"
	redisOrderPendingPfx   = "orders:pending:"
	redisOrderTxHashPrefix = "order:txhash:"
	redisArticleKeyPrefix  = "article:"
	createMaxRetries       = 5
)

type redisRepository struct {
	rdb redis.UniversalClient
}

// NewRedisRepository creates a Redis-backed Repository.
func NewRedisRepository(rdb redis.UniversalClient) Repository {
	return &redisRepository{rdb: rdb}
}

func orderKey(id uuid.UUID) string    { return redisOrderKeyPrefix + id.String() }
func pendingKey(wallet string) string { return redisOrderPendingPfx + wallet }
func txHashKey(hash string) string    { return redisOrderTxHashPrefix + hash }
func articleKey(id uuid.UUID) string  { return redisArticleKeyPrefix + id.String() }

// redisArticle mirrors the JSON written by the articles Redis repository.
// Orders only need to read and adjust the stock quantity, but the full
// document is round-tripped so no fields are lost on write-back.
type redisArticle struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Quantity  int       `json:"quantity"`
	Price     float64   `json:"price"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (r *redisRepository) Create(ctx context.Context, in CreateInput) (*Order, error) {
	// Deduplicate the article keys we WATCH; an order may list the same
	// article more than once (the handler usually collapses these, but the
	// repository must not assume it).
	keySet := make(map[uuid.UUID]struct{}, len(in.Items))
	for _, it := range in.Items {
		keySet[it.ArticleID] = struct{}{}
	}
	watchKeys := make([]string, 0, len(keySet))
	for id := range keySet {
		watchKeys = append(watchKeys, articleKey(id))
	}

	var order *Order
	txf := func(tx *redis.Tx) error {
		articles, err := r.loadArticles(ctx, tx, keySet)
		if err != nil {
			return err
		}

		var total float64
		o := Order{
			ID:            uuid.New(),
			WalletAddress: in.WalletAddress,
			Status:        StatusPending,
			CreatedAt:     time.Now().UTC(),
		}
		for _, item := range in.Items {
			a, ok := articles[item.ArticleID]
			if !ok || a.Quantity < item.Quantity {
				return fmt.Errorf("insufficient stock for article %s", item.ArticleID)
			}
			a.Quantity -= item.Quantity
			a.UpdatedAt = time.Now().UTC()
			articles[item.ArticleID] = a

			total += item.UnitPrice * float64(item.Quantity)
			o.Items = append(o.Items, OrderItem{
				ID:        uuid.New(),
				OrderID:   o.ID,
				ArticleID: item.ArticleID,
				Quantity:  item.Quantity,
				UnitPrice: item.UnitPrice,
			})
		}
		o.TotalAmount = total

		orderJSON, err := json.Marshal(o)
		if err != nil {
			return err
		}

		_, err = tx.TxPipelined(ctx, func(p redis.Pipeliner) error {
			for id, a := range articles {
				payload, mErr := json.Marshal(a)
				if mErr != nil {
					return mErr
				}
				p.Set(ctx, articleKey(id), payload, 0)
			}
			p.Set(ctx, orderKey(o.ID), orderJSON, 0)
			p.ZAdd(ctx, redisOrderIndexKey, redis.Z{Score: float64(o.CreatedAt.UnixNano()), Member: o.ID.String()})
			p.ZAdd(ctx, pendingKey(o.WalletAddress), redis.Z{Score: float64(o.CreatedAt.UnixNano()), Member: o.ID.String()})
			return nil
		})
		if err != nil {
			return err
		}
		order = &o
		return nil
	}

	for attempt := 0; attempt < createMaxRetries; attempt++ {
		err := r.rdb.Watch(ctx, txf, watchKeys...)
		if err == nil {
			return order, nil
		}
		if errors.Is(err, redis.TxFailedErr) {
			continue // a watched article changed; retry the whole checkout
		}
		return nil, fmt.Errorf("create order: %w", err)
	}
	return nil, errors.New("create order: too much contention, please retry")
}

// loadArticles reads the watched article documents via the supplied Tx.
func (r *redisRepository) loadArticles(ctx context.Context, tx *redis.Tx, ids map[uuid.UUID]struct{}) (map[uuid.UUID]redisArticle, error) {
	out := make(map[uuid.UUID]redisArticle, len(ids))
	for id := range ids {
		s, err := tx.Get(ctx, articleKey(id)).Result()
		if errors.Is(err, redis.Nil) {
			continue // treated as out-of-stock by the caller
		}
		if err != nil {
			return nil, fmt.Errorf("load article %s: %w", id, err)
		}
		var a redisArticle
		if err := json.Unmarshal([]byte(s), &a); err != nil {
			return nil, fmt.Errorf("decode article %s: %w", id, err)
		}
		out[id] = a
	}
	return out, nil
}

func (r *redisRepository) List(ctx context.Context, limit, offset int) ([]Order, int, error) {
	total, err := r.rdb.ZCard(ctx, redisOrderIndexKey).Result()
	if err != nil {
		return nil, 0, fmt.Errorf("count orders: %w", err)
	}
	if offset < 0 {
		offset = 0
	}
	stop := int64(offset + limit - 1)
	if limit <= 0 {
		stop = -1
	}
	ids, err := r.rdb.ZRevRange(ctx, redisOrderIndexKey, int64(offset), stop).Result()
	if err != nil {
		return nil, 0, fmt.Errorf("list order index: %w", err)
	}
	orders, err := r.loadOrders(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	return orders, int(total), nil
}

func (r *redisRepository) loadOrders(ctx context.Context, ids []string) ([]Order, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = redisOrderKeyPrefix + id
	}
	vals, err := r.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("mget orders: %w", err)
	}
	result := make([]Order, 0, len(vals))
	for _, v := range vals {
		s, ok := v.(string)
		if !ok {
			continue
		}
		var o Order
		if err := json.Unmarshal([]byte(s), &o); err != nil {
			return nil, fmt.Errorf("decode order: %w", err)
		}
		result = append(result, o)
	}
	return result, nil
}

func (r *redisRepository) Get(ctx context.Context, id uuid.UUID) (*Order, error) {
	s, err := r.rdb.Get(ctx, orderKey(id)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get order: %w", err)
	}
	var o Order
	if err := json.Unmarshal([]byte(s), &o); err != nil {
		return nil, fmt.Errorf("decode order: %w", err)
	}
	return &o, nil
}

func (r *redisRepository) FindPendingByWallet(ctx context.Context, wallet string) (*Order, error) {
	ids, err := r.rdb.ZRevRange(ctx, pendingKey(wallet), 0, 0).Result()
	if err != nil {
		return nil, fmt.Errorf("find pending order: %w", err)
	}
	if len(ids) == 0 {
		return nil, ErrNotFound
	}
	id, err := uuid.Parse(ids[0])
	if err != nil {
		return nil, fmt.Errorf("parse pending order id: %w", err)
	}
	return r.Get(ctx, id)
}

func (r *redisRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string, txHash *string) error {
	o, err := r.Get(ctx, id)
	if err != nil {
		return err
	}

	if txHash != nil && *txHash != "" {
		// SETNX enforces the one-tx-hash-per-order invariant. If the key
		// already maps to a different order, the hash is reused.
		ok, err := r.rdb.SetNX(ctx, txHashKey(*txHash), id.String(), 0).Result()
		if err != nil {
			return fmt.Errorf("reserve tx_hash: %w", err)
		}
		if !ok {
			owner, gErr := r.rdb.Get(ctx, txHashKey(*txHash)).Result()
			if gErr != nil {
				return fmt.Errorf("check tx_hash owner: %w", gErr)
			}
			if owner != id.String() {
				return ErrTxHashReused
			}
		}
	}

	o.Status = status
	o.TxHash = txHash
	payload, err := json.Marshal(o)
	if err != nil {
		return err
	}
	_, err = r.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.Set(ctx, orderKey(id), payload, 0)
		if status != StatusPending {
			// No longer a candidate for FindPendingByWallet.
			p.ZRem(ctx, pendingKey(o.WalletAddress), id.String())
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("update order status: %w", err)
	}
	return nil
}
