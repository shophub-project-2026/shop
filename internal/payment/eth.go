// Package payment handles Ethereum Sepolia testnet transaction verification.
package payment

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// EthClient is the subset of go-ethereum operations used by this package.
// It is defined as an interface so tests can inject a mock.
type EthClient interface {
	// TransactionByHash returns the transaction and whether it is still pending.
	TransactionByHash(ctx context.Context, hash common.Hash) (*types.Transaction, bool, error)
	// TransactionReceipt returns the receipt for a mined transaction.
	TransactionReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error)
	// BlockNumber returns the latest block number.
	BlockNumber(ctx context.Context) (uint64, error)
}

// NewEthClient dials the given RPC URL and returns a live client.
// The caller must close it when done.
func NewEthClient(ctx context.Context, rpcURL string) (EthClient, error) {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("dial eth rpc: %w", err)
	}
	return client, nil
}

// USDtoWei converts a USD amount to Wei using the given ETH/USD rate.
// 1 ETH = 1e18 Wei.
func USDtoWei(usd float64, ethPriceUSD float64) *big.Int {
	// eth = usd / ethPriceUSD
	// wei = eth * 1e18
	ethAmount := usd / ethPriceUSD
	weiFloat := ethAmount * 1e18
	wei := new(big.Float).SetFloat64(weiFloat)
	result, _ := wei.Int(nil)
	return result
}

// VerifyPayment checks that the transaction identified by txHash on the
// Ethereum network:
//   - is mined (not pending)
//   - has at least minConfirmations confirmations
//   - sends funds to recipientAddress (case-insensitive)
//   - transfers at least expectedWei
//
// Returns nil if all checks pass.
func VerifyPayment(
	ctx context.Context,
	client EthClient,
	txHash common.Hash,
	recipientAddress string,
	expectedWei *big.Int,
	minConfirmations uint64,
) error {
	tx, isPending, err := client.TransactionByHash(ctx, txHash)
	if err != nil {
		return fmt.Errorf("get transaction: %w", err)
	}
	if isPending {
		return fmt.Errorf("transaction is still pending")
	}

	receipt, err := client.TransactionReceipt(ctx, txHash)
	if err != nil {
		return fmt.Errorf("get receipt: %w", err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return fmt.Errorf("transaction failed on-chain")
	}

	latest, err := client.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("get block number: %w", err)
	}
	confirmations := latest - receipt.BlockNumber.Uint64()
	if confirmations < minConfirmations {
		return fmt.Errorf("only %d confirmation(s), need %d", confirmations, minConfirmations)
	}

	// check recipient
	to := tx.To()
	if to == nil {
		return fmt.Errorf("transaction has no recipient (contract creation)")
	}
	if !strings.EqualFold(to.Hex(), recipientAddress) {
		return fmt.Errorf("wrong recipient: got %s, want %s", to.Hex(), recipientAddress)
	}

	// check amount
	if tx.Value().Cmp(expectedWei) < 0 {
		return fmt.Errorf("insufficient payment: got %s wei, want %s wei",
			tx.Value().String(), expectedWei.String())
	}

	return nil
}
