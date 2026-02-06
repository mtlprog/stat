package indicator

import (
	"context"
	"log/slog"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// Layer0Calculator computes per-account total values (I51-I53, I56-I60) and BTC rate (I61).
type Layer0Calculator struct{}

func (c *Layer0Calculator) IDs() []int          { return []int{51, 52, 53, 56, 57, 58, 59, 60, 61} }
func (c *Layer0Calculator) Dependencies() []int { return nil }

func (c *Layer0Calculator) Calculate(_ context.Context, data domain.FundStructureData, _ map[int]Indicator, _ *HistoricalData) ([]Indicator, error) {
	allAccounts := lo.Flatten([][]domain.FundAccountPortfolio{data.Accounts, data.MutualFunds, data.OtherAccounts})

	accountIndicators := map[string]int{
		"DEFI":        51,
		"MCITY":       52,
		"MABIZ":       53,
		"APART":       56,
		"MFB":         57,
		"MAIN ISSUER": 58,
		"BOSS":        59,
		"ADMIN":       60,
	}

	var indicators []Indicator

	found := make(map[int]bool)
	for _, acc := range allAccounts {
		if id, ok := accountIndicators[acc.Name]; ok {
			indicators = append(indicators, NewIndicator(id, acc.TotalEURMTL, "", ""))
			found[id] = true
		}
	}

	// Emit zero-value indicators for any accounts not found in data
	for name, id := range accountIndicators {
		if !found[id] {
			slog.Warn("account not found in fund data, emitting zero indicator",
				"account", name, "indicatorID", id)
			indicators = append(indicators, NewIndicator(id, decimal.Zero, "", ""))
		}
	}

	// I61: BTC rate — from BTC/WBTC market price in portfolio tokens
	btcPrice := findBTCPrice(allAccounts)
	indicators = append(indicators, NewIndicator(61, btcPrice, "", ""))

	return indicators, nil
}

func findBTCPrice(accounts []domain.FundAccountPortfolio) decimal.Decimal {
	for _, acc := range accounts {
		for _, token := range acc.Tokens {
			if lo.Contains([]string{"BTC", "WBTC"}, token.Asset.Code) && token.PriceInEURMTL != nil {
				price := domain.SafeParse(*token.PriceInEURMTL)
				if !price.IsZero() {
					return price
				}
			}
		}
	}
	return decimal.Zero
}
