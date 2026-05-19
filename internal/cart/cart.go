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

// Store is a thread-safe in-memory cart store.
// Key: wallet address (string), Value: map[article_id]quantity.
type Store struct {
	mu   sync.Mutex
	data map[string]map[uuid.UUID]int
}

// NewStore creates an empty cart Store.
func NewStore() *Store {
	return &Store{data: make(map[string]map[uuid.UUID]int)}
}

// Add adds quantity units of articleID to the cart for wallet.
func (s *Store) Add(wallet string, articleID uuid.UUID, quantity int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data[wallet] == nil {
		s.data[wallet] = make(map[uuid.UUID]int)
	}
	s.data[wallet][articleID] += quantity
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
	return true
}

// Clear removes all items from the cart for wallet.
func (s *Store) Clear(wallet string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, wallet)
}
