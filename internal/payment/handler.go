package payment

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	"github.com/shophub-project-2026/shop/internal/cart"
	"github.com/shophub-project-2026/shop/internal/orders"
)

const minConfirmations = 1

// Handler serves the payment endpoints.
type Handler struct {
	orderRepo   orders.Repository
	cartStore   *cart.Store
	ethClient   EthClient
	walletAddr  string
	ethPriceUSD float64
	logger      *slog.Logger
}

// NewHandler constructs a payment Handler.
func NewHandler(
	orderRepo orders.Repository,
	cartStore *cart.Store,
	ethClient EthClient,
	walletAddr string,
	ethPriceUSD float64,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		orderRepo:   orderRepo,
		cartStore:   cartStore,
		ethClient:   ethClient,
		walletAddr:  walletAddr,
		ethPriceUSD: ethPriceUSD,
		logger:      logger,
	}
}

// RegisterRoutes wires the payment routes onto mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /payment/pending", h.pending)
	mux.HandleFunc("POST /payment/verify", h.verify)
}

type pendingResponse struct {
	OrderID       string  `json:"order_id"`
	WalletAddress string  `json:"wallet_address"`
	TotalUSD      float64 `json:"total_usd"`
	EthAmount     float64 `json:"eth_amount"`
	RecipientAddr string  `json:"recipient_address"`
	EthPriceUSD   float64 `json:"eth_price_usd"`
}

func (h *Handler) pending(w http.ResponseWriter, r *http.Request) {
	wallet := r.URL.Query().Get("wallet")
	if wallet == "" {
		writeError(w, http.StatusBadRequest, "wallet query param is required")
		return
	}

	pendingOrder, err := h.orderRepo.FindPendingByWallet(r.Context(), wallet)
	if errors.Is(err, orders.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no pending order for this wallet")
		return
	}
	if err != nil {
		h.internalError(w, "find pending order", err)
		return
	}

	ethAmount := pendingOrder.TotalAmount / h.ethPriceUSD
	writeJSON(w, http.StatusOK, pendingResponse{
		OrderID:       pendingOrder.ID.String(),
		WalletAddress: wallet,
		TotalUSD:      pendingOrder.TotalAmount,
		EthAmount:     ethAmount,
		RecipientAddr: h.walletAddr,
		EthPriceUSD:   h.ethPriceUSD,
	})
}

type verifyRequest struct {
	OrderID string `json:"order_id"`
	TxHash  string `json:"tx_hash"`
}

func (h *Handler) verify(w http.ResponseWriter, r *http.Request) {
	var req verifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.OrderID == "" || req.TxHash == "" {
		writeError(w, http.StatusBadRequest, "order_id and tx_hash are required")
		return
	}

	orderID, err := uuid.Parse(req.OrderID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid order_id")
		return
	}

	order, err := h.orderRepo.Get(r.Context(), orderID)
	if errors.Is(err, orders.ErrNotFound) {
		writeError(w, http.StatusNotFound, "order not found")
		return
	}
	if err != nil {
		h.internalError(w, "get order", err)
		return
	}
	if order.Status != orders.StatusPending {
		writeError(w, http.StatusConflict, "order is not in pending status")
		return
	}

	expectedWei := USDtoWei(order.TotalAmount, h.ethPriceUSD)
	txHash := common.HexToHash(req.TxHash)

	if err := VerifyPayment(
		r.Context(),
		h.ethClient,
		txHash,
		h.walletAddr,
		expectedWei,
		minConfirmations,
	); err != nil {
		h.logger.Info("payment verification failed", "order_id", req.OrderID, "err", err)
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	txHashStr := req.TxHash
	if err := h.orderRepo.UpdateStatus(r.Context(), orderID, orders.StatusPaid, &txHashStr); err != nil {
		if errors.Is(err, orders.ErrTxHashReused) {
			writeError(w, http.StatusConflict, "this transaction has already been used for another order")
			return
		}
		h.internalError(w, "update order status", err)
		return
	}

	h.cartStore.Clear(order.WalletAddress)

	order.Status = orders.StatusPaid
	order.TxHash = &txHashStr
	writeJSON(w, http.StatusOK, order)
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
