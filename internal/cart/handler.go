package cart

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
)

// Handler exposes the cart Store over HTTP.
type Handler struct {
	store  *Store
	logger *slog.Logger
}

// NewHandler constructs a cart Handler.
func NewHandler(store *Store, logger *slog.Logger) *Handler {
	return &Handler{store: store, logger: logger}
}

// RegisterRoutes wires the cart routes onto mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /cart", h.add)
	mux.HandleFunc("GET /cart", h.get)
	mux.HandleFunc("DELETE /cart/{articleId}", h.remove)
}

type addRequest struct {
	ArticleID     uuid.UUID `json:"article_id"`
	Quantity      int       `json:"quantity"`
	WalletAddress string    `json:"wallet_address"`
}

func (h *Handler) add(w http.ResponseWriter, r *http.Request) {
	var req addRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.WalletAddress == "" {
		writeError(w, http.StatusBadRequest, "wallet_address is required")
		return
	}
	if req.Quantity <= 0 {
		writeError(w, http.StatusBadRequest, "quantity must be > 0")
		return
	}
	h.store.Add(req.WalletAddress, req.ArticleID, req.Quantity)
	cart := h.store.Get(req.WalletAddress)
	writeJSON(w, http.StatusOK, cart)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	wallet := r.URL.Query().Get("wallet")
	if wallet == "" {
		writeError(w, http.StatusBadRequest, "wallet query param is required")
		return
	}
	writeJSON(w, http.StatusOK, h.store.Get(wallet))
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	wallet := r.URL.Query().Get("wallet")
	if wallet == "" {
		writeError(w, http.StatusBadRequest, "wallet query param is required")
		return
	}
	articleID, err := uuid.Parse(r.PathValue("articleId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid article id")
		return
	}
	if !h.store.Remove(wallet, articleID) {
		writeError(w, http.StatusNotFound, "item not found in cart")
		return
	}
	writeJSON(w, http.StatusOK, h.store.Get(wallet))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
