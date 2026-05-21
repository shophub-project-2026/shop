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

// weiPerEther is 10^18, exact.
var weiPerEther = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

// USDtoWei converts a USD amount to Wei using the given ETH/USD rate.
//
// 1 ETH = 1e18 Wei.  Wei = (usd / ethPriceUSD) * 1e18 = (usd * 1e18) / ethPriceUSD.
//
// We perform the arithmetic with math/big rationals so the result is
// exact up to the final integer truncation. The previous float64
// implementation lost precision for amounts above ~$9 million in USD or
// rates with many fractional digits.
//
// Non-positive inputs return 0 — they are programmer errors that callers
// should have rejected up front; we don't want the function to panic.
func USDtoWei(usd float64, ethPriceUSD float64) *big.Int {
	if usd <= 0 || ethPriceUSD <= 0 {
		return new(big.Int)
	}
	// Build big.Rat values from the floats. SetFloat64 is exact for any
	// finite IEEE-754 number, so no precision is lost at this stage.
	usdRat := new(big.Rat).SetFloat64(usd)
	rateRat := new(big.Rat).SetFloat64(ethPriceUSD)
	weiPerEtherRat := new(big.Rat).SetInt(weiPerEther)

	// wei = usd * 1e18 / rate
	num := new(big.Rat).Mul(usdRat, weiPerEtherRat)
	res := new(big.Rat).Quo(num, rateRat)

	// Floor to integer Wei.
	q := new(big.Int).Quo(res.Num(), res.Denom())
	return q
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
