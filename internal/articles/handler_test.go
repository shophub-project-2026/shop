package articles_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/shophub-project-2026/shop/internal/articles"
	"github.com/shophub-project-2026/shop/internal/server/middleware"
)

// mockRepo is an in-memory articles.Repository used for unit tests.
type mockRepo struct {
	data map[uuid.UUID]articles.Article
}

func newMockRepo() *mockRepo {
	return &mockRepo{data: make(map[uuid.UUID]articles.Article)}
}

func (m *mockRepo) List(_ context.Context, search string) ([]articles.Article, error) {
	var out []articles.Article
	for _, a := range m.data {
		if search == "" || contains(a.Name, search) {
			out = append(out, a)
		}
	}
	return out, nil
}

func (m *mockRepo) Get(_ context.Context, id uuid.UUID) (*articles.Article, error) {
	a, ok := m.data[id]
	if !ok {
		return nil, articles.ErrNotFound
	}
	return &a, nil
}

func (m *mockRepo) Create(_ context.Context, in articles.CreateInput) (*articles.Article, error) {
	a := articles.Article{
		ID:       uuid.New(),
		Name:     in.Name,
		Quantity: in.Quantity,
		Price:    in.Price,
	}
	m.data[a.ID] = a
	return &a, nil
}

func (m *mockRepo) Update(_ context.Context, id uuid.UUID, in articles.UpdateInput) (*articles.Article, error) {
	a, ok := m.data[id]
	if !ok {
		return nil, articles.ErrNotFound
	}
	if in.Name != "" {
		a.Name = in.Name
	}
	if in.Quantity != nil {
		a.Quantity = *in.Quantity
	}
	if in.Price != nil {
		a.Price = *in.Price
	}
	m.data[id] = a
	return &a, nil
}

func (m *mockRepo) Delete(_ context.Context, id uuid.UUID) error {
	if _, ok := m.data[id]; !ok {
		return articles.ErrNotFound
	}
	delete(m.data, id)
	return nil
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

func newTestMux(adminKey string) *http.ServeMux {
	repo := newMockRepo()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	h := articles.NewHandler(repo, logger)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, middleware.Admin(adminKey))
	return mux
}

func TestListArticles_Empty(t *testing.T) {
	mux := newTestMux("")
	req := httptest.NewRequest("GET", "/articles", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result []articles.Article
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty list, got %d items", len(result))
	}
}

func TestCreateAndGetArticle(t *testing.T) {
	const adminKey = "secret"
	mux := newTestMux(adminKey)

	// create
	body, _ := json.Marshal(map[string]any{"name": "Widget", "quantity": 10, "price": 9.99})
	req := httptest.NewRequest("POST", "/articles", bytes.NewReader(body))
	req.Header.Set("X-Admin-Key", adminKey)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d — %s", w.Code, w.Body.String())
	}
	var created articles.Article
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Name != "Widget" {
		t.Errorf("name: want Widget, got %s", created.Name)
	}

	// get
	req2 := httptest.NewRequest("GET", "/articles/"+created.ID.String(), nil)
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", w2.Code)
	}
}

func TestCreateArticle_AdminRequired(t *testing.T) {
	mux := newTestMux("secret")
	body, _ := json.Marshal(map[string]any{"name": "X", "quantity": 1, "price": 1.0})
	req := httptest.NewRequest("POST", "/articles", bytes.NewReader(body))
	// no X-Admin-Key header
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestDeleteArticle(t *testing.T) {
	const adminKey = "secret"
	mux := newTestMux(adminKey)

	body, _ := json.Marshal(map[string]any{"name": "ToDelete", "quantity": 1, "price": 1.0})
	req := httptest.NewRequest("POST", "/articles", bytes.NewReader(body))
	req.Header.Set("X-Admin-Key", adminKey)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var created articles.Article
	_ = json.NewDecoder(w.Body).Decode(&created)

	req2 := httptest.NewRequest("DELETE", "/articles/"+created.ID.String(), nil)
	req2.Header.Set("X-Admin-Key", adminKey)
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)

	if w2.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w2.Code)
	}
}

func TestGetArticle_NotFound(t *testing.T) {
	mux := newTestMux("")
	req := httptest.NewRequest("GET", "/articles/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
