package payment_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/google/uuid"
	"github.com/shophub-project-2026/shop/internal/cart"
	"github.com/shophub-project-2026/shop/internal/orders"
	"github.com/shophub-project-2026/shop/internal/payment"
)

// --- mock ETH client ---

type mockEthClient struct {
	tx           *types.Transaction
	isPending    bool
	receipt      *types.Receipt
	blockNumber  uint64
	txErr        error
	receiptErr   error
	blockNumErr  error
}

func (m *mockEthClient) TransactionByHash(_ context.Context, _ common.Hash) (*types.Transaction, bool, error) {
	return m.tx, m.isPending, m.txErr
}
func (m *mockEthClient) TransactionReceipt(_ context.Context, _ common.Hash) (*types.Receipt, error) {
	return m.receipt, m.receiptErr
}
func (m *mockEthClient) BlockNumber(_ context.Context) (uint64, error) {
	return m.blockNumber, m.blockNumErr
}

// --- mock order repo ---

type mockOrderRepo struct {
	orders map[uuid.UUID]*orders.Order
}

func newMockOrderRepo() *mockOrderRepo {
	return &mockOrderRepo{orders: make(map[uuid.UUID]*orders.Order)}
}

func (m *mockOrderRepo) Create(_ context.Context, in orders.CreateInput) (*orders.Order, error) {
	o := &orders.Order{
		ID:            uuid.New(),
		WalletAddress: in.WalletAddress,
		Status:        orders.StatusPending,
		CreatedAt:     time.Now(),
	}
	for _, item := range in.Items {
		o.TotalAmount += item.UnitPrice * float64(item.Quantity)
	}
	m.orders[o.ID] = o
	return o, nil
}

func (m *mockOrderRepo) List(_ context.Context, limit, _ int) ([]orders.Order, int, error) {
	result := make([]orders.Order, 0, len(m.orders))
	for _, o := range m.orders {
		result = append(result, *o)
		if len(result) >= limit {
			break
		}
	}
	return result, len(m.orders), nil
}

func (m *mockOrderRepo) Get(_ context.Context, id uuid.UUID) (*orders.Order, error) {
	o, ok := m.orders[id]
	if !ok {
		return nil, orders.ErrNotFound
	}
	return o, nil
}

func (m *mockOrderRepo) UpdateStatus(_ context.Context, id uuid.UUID, status string, txHash *string) error {
	o, ok := m.orders[id]
	if !ok {
		return orders.ErrNotFound
	}
	o.Status = status
	o.TxHash = txHash
	return nil
}

func (m *mockOrderRepo) FindPendingByWallet(_ context.Context, wallet string) (*orders.Order, error) {
	for _, o := range m.orders {
		if o.WalletAddress == wallet && o.Status == orders.StatusPending {
			return o, nil
		}
	}
	return nil, orders.ErrNotFound
}

// --- helpers ---

const (
	testWallet      = "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"
	testEthPriceUSD = 3000.0
)

func newTestMux(ethClient payment.EthClient, orderRepo orders.Repository, cartStore *cart.Store) *http.ServeMux {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	h := payment.NewHandler(orderRepo, cartStore, ethClient, testWallet, testEthPriceUSD, logger)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, nil)
	return mux
}

func makeTx(to common.Address, valueWei *big.Int) *types.Transaction {
	return types.NewTx(&types.LegacyTx{
		To:    &to,
		Value: valueWei,
		Gas:   21000,
	})
}

// --- tests ---

func TestPendingPayment_NoOrder(t *testing.T) {
	mux := newTestMux(&mockEthClient{}, newMockOrderRepo(), cart.NewStore())
	req := httptest.NewRequest("GET", "/payment/pending?wallet=0xWallet", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestPendingPayment_Found(t *testing.T) {
	repo := newMockOrderRepo()
	_, _ = repo.Create(context.Background(), orders.CreateInput{
		WalletAddress: "0xBuyer",
		Items:         []orders.ItemInput{{ArticleID: uuid.New(), Quantity: 1, UnitPrice: 300.0}},
	})

	mux := newTestMux(&mockEthClient{}, repo, cart.NewStore())
	req := httptest.NewRequest("GET", "/payment/pending?wallet=0xBuyer", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["eth_amount"] == nil {
		t.Error("expected eth_amount in response")
	}
}

func TestVerifyPayment_Success(t *testing.T) {
	repo := newMockOrderRepo()
	o, _ := repo.Create(context.Background(), orders.CreateInput{
		WalletAddress: "0xBuyer",
		Items:         []orders.ItemInput{{ArticleID: uuid.New(), Quantity: 1, UnitPrice: 300.0}},
	})

	expectedWei := payment.USDtoWei(300.0, testEthPriceUSD) // 300 / 3000 * 1e18 = 0.1 ETH
	toAddr := common.HexToAddress(testWallet)
	tx := makeTx(toAddr, expectedWei)
	receipt := &types.Receipt{
		Status:      types.ReceiptStatusSuccessful,
		BlockNumber: big.NewInt(100),
	}
	ethClient := &mockEthClient{
		tx:          tx,
		isPending:   false,
		receipt:     receipt,
		blockNumber: 101, // 1 confirmation
	}

	cartStore := cart.NewStore()
	cartStore.Add("0xBuyer", uuid.New(), 1)

	mux := newTestMux(ethClient, repo, cartStore)
	body, _ := json.Marshal(map[string]string{
		"order_id": o.ID.String(),
		"tx_hash":  "0xABCDEF",
	})
	req := httptest.NewRequest("POST", "/payment/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", w.Code, w.Body.String())
	}

	updated, _ := repo.Get(context.Background(), o.ID)
	if updated.Status != orders.StatusPaid {
		t.Errorf("expected status paid, got %s", updated.Status)
	}
}

func TestVerifyPayment_WrongRecipient(t *testing.T) {
	repo := newMockOrderRepo()
	o, _ := repo.Create(context.Background(), orders.CreateInput{
		WalletAddress: "0xBuyer",
		Items:         []orders.ItemInput{{ArticleID: uuid.New(), Quantity: 1, UnitPrice: 300.0}},
	})

	wrongAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	tx := makeTx(wrongAddr, payment.USDtoWei(300.0, testEthPriceUSD))
	receipt := &types.Receipt{Status: types.ReceiptStatusSuccessful, BlockNumber: big.NewInt(100)}
	ethClient := &mockEthClient{tx: tx, receipt: receipt, blockNumber: 101}

	mux := newTestMux(ethClient, repo, cart.NewStore())
	body, _ := json.Marshal(map[string]string{"order_id": o.ID.String(), "tx_hash": "0xABC"})
	req := httptest.NewRequest("POST", "/payment/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d — %s", w.Code, w.Body.String())
	}
}

func TestVerifyPayment_Pending(t *testing.T) {
	repo := newMockOrderRepo()
	o, _ := repo.Create(context.Background(), orders.CreateInput{
		WalletAddress: "0xBuyer",
		Items:         []orders.ItemInput{{ArticleID: uuid.New(), Quantity: 1, UnitPrice: 100.0}},
	})

	ethClient := &mockEthClient{
		tx:        makeTx(common.HexToAddress(testWallet), big.NewInt(1)),
		isPending: true,
	}

	mux := newTestMux(ethClient, repo, cart.NewStore())
	body, _ := json.Marshal(map[string]string{"order_id": o.ID.String(), "tx_hash": "0xABC"})
	req := httptest.NewRequest("POST", "/payment/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for pending tx, got %d — %s", w.Code, w.Body.String())
	}
}
