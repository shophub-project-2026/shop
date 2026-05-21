// Package cart provides an in-memory shopping cart keyed by wallet address.
package cart

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// Item represents a single line in a cart.
type Item struct {
	ArticleID uuid.UUID `json:"article_id"`
	Quantity  int       `json:"quantity"`
}

// Cart holds the items for one wallet address.
type Cart struct {
	WalletAddress string `json:"wallet_address"`
	Items         []Item `json:"items"`
}

// SizeObserver is called whenever the number of non-empty carts changes.
// Used to drive Prometheus gauges without cart depending on the metrics package.
type SizeObserver func(activeCarts int)

// entry holds a wallet's items together with a last-touched timestamp so the
// janitor can expire abandoned carts.
type entry struct {
	items     map[uuid.UUID]int
	updatedAt time.Time
}

// Store is a thread-safe in-memory cart store with TTL-based eviction.
type Store struct {
	mu       sync.Mutex
	data     map[string]*entry
	observer SizeObserver
	now      func() time.Time
	ttl      time.Duration
}

// Option configures a Store. See WithTTL / WithClock.
type Option func(*Store)

// WithTTL sets how long a wallet's cart survives without modification.
// A non-positive value disables TTL eviction.
func WithTTL(ttl time.Duration) Option { return func(s *Store) { s.ttl = ttl } }

// WithClock injects a clock function (for deterministic tests).
func WithClock(now func() time.Time) Option { return func(s *Store) { s.now = now } }

// NewStore creates an empty cart Store.
func NewStore(opts ...Option) *Store {
	s := &Store{
		data: make(map[string]*entry),
		now:  time.Now,
		ttl:  30 * time.Minute,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// SetSizeObserver registers a callback fired on every change in the number
// of non-empty carts. Passing nil disables notifications.
func (s *Store) SetSizeObserver(observer SizeObserver) {
	s.observer = observer
}

func (s *Store) notify() {
	if s.observer == nil {
		return
	}
	s.observer(len(s.data))
}

// Add adds quantity units of articleID to the cart for wallet.
func (s *Store) Add(wallet string, articleID uuid.UUID, quantity int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, exists := s.data[wallet]
	if !exists {
		e = &entry{items: make(map[uuid.UUID]int)}
		s.data[wallet] = e
	}
	e.items[articleID] += quantity
	e.updatedAt = s.now()
	if !exists {
		s.notify()
	}
}

// Len returns the number of non-empty carts currently in the store.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.data)
}

// Get returns the current cart for wallet.
func (s *Store) Get(wallet string) Cart {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.data[wallet]
	if e == nil {
		return Cart{WalletAddress: wallet}
	}
	items := make([]Item, 0, len(e.items))
	for id, qty := range e.items {
		items = append(items, Item{ArticleID: id, Quantity: qty})
	}
	return Cart{WalletAddress: wallet, Items: items}
}

// Remove removes articleID from the cart for wallet.
// Returns false if the item was not present.
func (s *Store) Remove(wallet string, articleID uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.data[wallet]
	if e == nil {
		return false
	}
	if _, ok := e.items[articleID]; !ok {
		return false
	}
	delete(e.items, articleID)
	if len(e.items) == 0 {
		delete(s.data, wallet)
		s.notify()
	} else {
		e.updatedAt = s.now()
	}
	return true
}

// Clear removes all items from the cart for wallet.
func (s *Store) Clear(wallet string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.data[wallet]; exists {
		delete(s.data, wallet)
		s.notify()
	}
}

// EvictExpired removes carts whose last update was more than the configured
// TTL ago, returning the number of carts removed. Safe to call concurrently
// with other Store methods. A non-positive TTL disables eviction.
func (s *Store) EvictExpired() int {
	if s.ttl <= 0 {
		return 0
	}
	cutoff := s.now().Add(-s.ttl)

	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for wallet, e := range s.data {
		if e.updatedAt.Before(cutoff) {
			delete(s.data, wallet)
			removed++
		}
	}
	if removed > 0 {
		s.notify()
	}
	return removed
}

// StartJanitor runs EvictExpired every interval until stop is closed.
// Returns immediately; the janitor runs on its own goroutine.
func (s *Store) StartJanitor(interval time.Duration, stop <-chan struct{}) {
	if interval <= 0 || s.ttl <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.EvictExpired()
			case <-stop:
				return
			}
		}
	}()
}
