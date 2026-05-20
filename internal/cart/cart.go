// Package cart provides an in-memory shopping cart keyed by wallet address.
package cart

import (
	"sync"

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

// Store is a thread-safe in-memory cart store.
type Store struct {
	mu       sync.Mutex
	data     map[string]map[uuid.UUID]int
	observer SizeObserver
}

// NewStore creates an empty cart Store.
func NewStore() *Store {
	return &Store{data: make(map[string]map[uuid.UUID]int)}
}

// SetSizeObserver registers a callback fired on every change in the number
// of non-empty carts. Passing nil disables notifications.
// Must be called before any concurrent use of the Store.
func (s *Store) SetSizeObserver(observer SizeObserver) {
	s.observer = observer
}

// notify must be called with s.mu held.
func (s *Store) notify() {
	if s.observer == nil {
		return
	}
	count := 0
	for _, items := range s.data {
		if len(items) > 0 {
			count++
		}
	}
	s.observer(count)
}

// Add adds quantity units of articleID to the cart for wallet.
func (s *Store) Add(wallet string, articleID uuid.UUID, quantity int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	wasEmpty := len(s.data[wallet]) == 0
	if s.data[wallet] == nil {
		s.data[wallet] = make(map[uuid.UUID]int)
	}
	s.data[wallet][articleID] += quantity
	if wasEmpty {
		s.notify()
	}
}

// Len returns the number of non-empty carts currently in the store.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, items := range s.data {
		if len(items) > 0 {
			count++
		}
	}
	return count
}

// Get returns the current cart for wallet.
func (s *Store) Get(wallet string) Cart {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]Item, 0, len(s.data[wallet]))
	for id, qty := range s.data[wallet] {
		items = append(items, Item{ArticleID: id, Quantity: qty})
	}
	return Cart{WalletAddress: wallet, Items: items}
}

// Remove removes articleID from the cart for wallet.
// Returns false if the item was not present.
func (s *Store) Remove(wallet string, articleID uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[wallet][articleID]; !ok {
		return false
	}
	delete(s.data[wallet], articleID)
	if len(s.data[wallet]) == 0 {
		delete(s.data, wallet)
		s.notify()
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
