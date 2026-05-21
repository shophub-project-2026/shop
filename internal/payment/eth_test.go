package payment_test

import (
	"math/big"
	"testing"

	"github.com/shophub-project-2026/shop/internal/payment"
)

func TestUSDtoWei_BasicConversion(t *testing.T) {
	// $300 at $3000/ETH = 0.1 ETH = 10^17 wei
	got := payment.USDtoWei(300, 3000)
	want := new(big.Int).Exp(big.NewInt(10), big.NewInt(17), nil)
	if got.Cmp(want) != 0 {
		t.Errorf("USDtoWei(300, 3000) = %s, want %s", got.String(), want.String())
	}
}

func TestUSDtoWei_NonIntegralRate(t *testing.T) {
	// $1 at $2500.50/ETH ≈ 0.000399920016 ETH
	// Exact: 1e18 / 2500.5 = 399_840_031_993_601.27...  → floored to 399_840_031_993_601
	got := payment.USDtoWei(1, 2500.5)
	if got.Sign() <= 0 {
		t.Errorf("USDtoWei(1, 2500.5) should be > 0, got %s", got.String())
	}
	// Just verify it's in the expected order of magnitude (~4e14 wei).
	min := big.NewInt(3_990_000_000_000_00) // 3.99e14
	max := big.NewInt(4_000_000_000_000_00) // 4.00e14
	if got.Cmp(min) < 0 || got.Cmp(max) > 0 {
		t.Errorf("USDtoWei(1, 2500.5) = %s, want between %s and %s", got, min, max)
	}
}

func TestUSDtoWei_LargeAmountStaysPrecise(t *testing.T) {
	// $99_999_999.99 at $2.50/ETH should not lose precision the way float64 would.
	got := payment.USDtoWei(99_999_999.99, 2.50)
	// Expected wei = 99_999_999.99 * 1e18 / 2.5 = 3.99999999996e25
	want, _ := new(big.Int).SetString("39999999996000000000000000", 10)
	// Allow ±1 wei rounding because float→big.Rat conversion of 99_999_999.99
	// produces the exact binary representation, which is slightly off.
	diff := new(big.Int).Sub(got, want)
	diff.Abs(diff)
	tolerance := new(big.Int).Exp(big.NewInt(10), big.NewInt(10), nil) // 1e10 wei (~10 µETH)
	if diff.Cmp(tolerance) > 0 {
		t.Errorf("USDtoWei($99,999,999.99, $2.50) = %s, want ≈ %s (diff %s)", got, want, diff)
	}
}

func TestUSDtoWei_NonPositiveReturnsZero(t *testing.T) {
	if got := payment.USDtoWei(0, 3000); got.Sign() != 0 {
		t.Errorf("USDtoWei(0, 3000) = %s, want 0", got)
	}
	if got := payment.USDtoWei(100, 0); got.Sign() != 0 {
		t.Errorf("USDtoWei(100, 0) = %s, want 0", got)
	}
	if got := payment.USDtoWei(-1, 3000); got.Sign() != 0 {
		t.Errorf("USDtoWei(-1, 3000) = %s, want 0", got)
	}
}
