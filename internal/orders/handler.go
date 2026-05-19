package orders

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/shophub-project-2026/shop/internal/articles"
	"github.com/shophub-project-2026/shop/internal/cart"
)

// Handler exposes the orders Repository over HTTP.
type Handler struct {
	repo        Repository
	cartStore   *cart.Store
	articleRepo articles.Repository
	logger      *slog.Logger
}

// NewHandler constructs an orders Handler.
func NewHandler(repo Repository, cartStore *cart.Store, articleRepo articles.Repository, logger *slog.Logger) *Handler {
	return &Handler{
		repo:        repo,
		cartStore:   cartStore,
		articleRepo: articleRepo,
		logger:      logger,
	}
}

// RegisterRoutes wires the order routes onto mux.
// POST /orders is public (any wallet can check out).
// GET  /orders is admin-only.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, adminMW func(http.Handler) http.Handler) {
	mux.HandleFunc("POST /orders", h.create)
	mux.Handle("GET /orders", adminMW(http.HandlerFunc(h.list)))
}

type createRequest struct {
	WalletAddress string `json:"wallet_address"`
}

type listResponse struct {
	Orders []Order `json:"orders"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.WalletAddress == "" {
		writeError(w, http.StatusBadRequest, "wallet_address is required")
		return
	}

	cartData := h.cartStore.Get(req.WalletAddress)
	if len(cartData.Items) == 0 {
		writeError(w, http.StatusBadRequest, "cart is empty")
		return
	}

	// resolve current prices from DB
	input := CreateInput{WalletAddress: req.WalletAddress}
	for _, item := range cartData.Items {
		a, err := h.articleRepo.Get(r.Context(), item.ArticleID)
		if err != nil {
			if errors.Is(err, articles.ErrNotFound) {
				writeError(w, http.StatusUnprocessableEntity, "article "+item.ArticleID.String()+" not found")
				return
			}
			h.internalError(w, "get article for order", err)
			return
		}
		input.Items = append(input.Items, ItemInput{
			ArticleID: item.ArticleID,
			Quantity:  item.Quantity,
			UnitPrice: a.Price,
		})
	}

	order, err := h.repo.Create(r.Context(), input)
	if err != nil {
		// insufficient stock error is a 422
		h.logger.Error("create order", "err", err)
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	h.cartStore.Clear(req.WalletAddress)
	writeJSON(w, http.StatusCreated, order)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 20)
	offset := queryInt(r, "offset", 0)
	if limit > 100 {
		limit = 100
	}

	orders, total, err := h.repo.List(r.Context(), limit, offset)
	if err != nil {
		h.internalError(w, "list orders", err)
		return
	}
	if orders == nil {
		orders = []Order{}
	}
	writeJSON(w, http.StatusOK, listResponse{
		Orders: orders,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

func queryInt(r *http.Request, key string, def int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return def
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (h *Handler) internalError(w http.ResponseWriter, op string, err error) {
	h.logger.Error(op, "err", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}
