package fund

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

func TestCalculateAccountTotalEURMTLNormal(t *testing.T) {
	price1 := "2.0"
	price2 := "100.0"
	xlmPrice := "0.5"

	tokens := []domain.TokenPriceWithBalance{
		{Balance: "10", PriceInEURMTL: &price1},
		{Balance: "0.0000001", PriceInEURMTL: &price2, IsNFT: true},
	}

	total := calculateAccountTotalEURMTL(tokens, "1000", &xlmPrice)

	// 10*2 + 100 (NFT) + 1000*0.5 = 20 + 100 + 500 = 620
	if !total.Equal(decimal.NewFromInt(620)) {
		t.Errorf("totalEURMTL = %s, want 620", total)
	}
}

func TestCalculateAccountTotalXLMNormal(t *testing.T) {
	price := "5.0"

	tokens := []domain.TokenPriceWithBalance{
		{Balance: "10", PriceInXLM: &price},
	}

	total := calculateAccountTotalXLM(tokens, "500")

	// 10*5 + 500 = 550
	if !total.Equal(decimal.NewFromInt(550)) {
		t.Errorf("totalXLM = %s, want 550", total)
	}
}

func TestCalculateAccountTotalNilPrices(t *testing.T) {
	tokens := []domain.TokenPriceWithBalance{
		{Balance: "10", PriceInEURMTL: nil},
	}

	total := calculateAccountTotalEURMTL(tokens, "0", nil)
	if !total.Equal(decimal.Zero) {
		t.Errorf("totalEURMTL with nil prices = %s, want 0", total)
	}
}

func TestCalculateFundTotals(t *testing.T) {
	accounts := []domain.FundAccountPortfolio{
		{TotalEURMTL: decimal.NewFromInt(1000), TotalXLM: decimal.NewFromInt(5000), Tokens: make([]domain.TokenPriceWithBalance, 3)},
		{TotalEURMTL: decimal.NewFromInt(2000), TotalXLM: decimal.NewFromInt(10000), Tokens: make([]domain.TokenPriceWithBalance, 5)},
	}

	totals := calculateFundTotals(accounts)

	if !totals.TotalEURMTL.Equal(decimal.NewFromInt(3000)) {
		t.Errorf("TotalEURMTL = %s, want 3000", totals.TotalEURMTL)
	}
	if !totals.TotalXLM.Equal(decimal.NewFromInt(15000)) {
		t.Errorf("TotalXLM = %s, want 15000", totals.TotalXLM)
	}
	if totals.AccountCount != 2 {
		t.Errorf("AccountCount = %d, want 2", totals.AccountCount)
	}
	if totals.TokenCount != 8 {
		t.Errorf("TokenCount = %d, want 8", totals.TokenCount)
	}
}
