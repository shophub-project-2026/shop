package cart_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/shophub-project-2026/shop/internal/cart"
)

func newMux() *http.ServeMux {
	store := cart.NewStore()
	h := cart.NewHandler(store, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux
}

func TestCart_AddAndGet(t *testing.T) {
	mux := newMux()
	articleID := uuid.New()

	body, _ := json.Marshal(map[string]any{
		"article_id":     articleID,
		"quantity":       3,
		"wallet_address": "0xABC",
	})
	req := httptest.NewRequest("POST", "/cart", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("add: expected 200, got %d — %s", w.Code, w.Body.String())
	}

	req2 := httptest.NewRequest("GET", "/cart?wallet=0xABC", nil)
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", w2.Code)
	}
	var c cart.Cart
	_ = json.NewDecoder(w2.Body).Decode(&c)
	if len(c.Items) != 1 {
		t.Errorf("want 1 item, got %d", len(c.Items))
	}
	if c.Items[0].Quantity != 3 {
		t.Errorf("want qty 3, got %d", c.Items[0].Quantity)
	}
}

func TestCart_Remove(t *testing.T) {
	mux := newMux()
	articleID := uuid.New()

	body, _ := json.Marshal(map[string]any{
		"article_id":     articleID,
		"quantity":       1,
		"wallet_address": "0xDEF",
	})
	req := httptest.NewRequest("POST", "/cart", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	httptest.NewRecorder()
	mux.ServeHTTP(httptest.NewRecorder(), req)

	req2 := httptest.NewRequest("DELETE", "/cart/"+articleID.String()+"?wallet=0xDEF", nil)
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("remove: expected 200, got %d — %s", w2.Code, w2.Body.String())
	}
	var c cart.Cart
	_ = json.NewDecoder(w2.Body).Decode(&c)
	if len(c.Items) != 0 {
		t.Errorf("want empty cart, got %d items", len(c.Items))
	}
}

func TestCart_MissingWallet(t *testing.T) {
	mux := newMux()
	req := httptest.NewRequest("GET", "/cart", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCart_SizeObserver(t *testing.T) {
	store := cart.NewStore()
	var observed []int
	store.SetSizeObserver(func(n int) { observed = append(observed, n) })

	a1, a2 := uuid.New(), uuid.New()
	store.Add("0xA", a1, 1) // first non-empty cart → notify 1
	store.Add("0xA", a2, 1) // still 1 cart → no notify
	store.Add("0xB", a1, 1) // second cart → notify 2
	store.Remove("0xA", a1) // 0xA still has a2 → no notify
	store.Remove("0xA", a2) // 0xA becomes empty → notify 1
	store.Clear("0xB")      // 0xB cleared → notify 0

	want := []int{1, 2, 1, 0}
	if len(observed) != len(want) {
		t.Fatalf("observer fired %d times, want %d: %v", len(observed), len(want), observed)
	}
	for i, v := range want {
		if observed[i] != v {
			t.Errorf("observer[%d]=%d, want %d (full: %v)", i, observed[i], v, observed)
		}
	}
}
