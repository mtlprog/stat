package indicator

import (
	"context"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// Layer0Calculator computes per-account total values (I51-I60) and BTC rate (I61).
type Layer0Calculator struct{}

func (c *Layer0Calculator) IDs() []int           { return []int{51, 52, 53, 56, 57, 58, 59, 60, 61} }
func (c *Layer0Calculator) Dependencies() []int  { return nil }

func (c *Layer0Calculator) Calculate(_ context.Context, data domain.FundStructureData, _ map[int]Indicator, _ *HistoricalData) ([]Indicator, error) {
	allAccounts := append(append(data.Accounts, data.MutualFunds...), data.OtherAccounts...)

	accountIndicators := map[string]struct {
		id   int
		name string
	}{
		"DEFI":         {51, "DEFI Total Value"},
		"MCITY":        {52, "MCITY Total Value"},
		"MABIZ":        {53, "MABIZ Total Value"},
		"APART":        {56, "MFApart Total Value"},
		"MFB":          {57, "MFBond Total Value"},
		"MAIN ISSUER":  {58, "Issuer Free Assets"},
		"BOSS":         {59, "BOSS Total Value"},
		"ADMIN":        {60, "ADMIN Total Value"},
	}

	var indicators []Indicator

	for _, acc := range allAccounts {
		if mapping, ok := accountIndicators[acc.Name]; ok {
			indicators = append(indicators, Indicator{
				ID:    mapping.id,
				Name:  mapping.name,
				Value: decimal.NewFromFloat(acc.TotalEURMTL),
				Unit:  "EURMTL",
			})
		}
	}

	// I61: BTC rate — from external quotes (stored as EURMTL value of BTC in snapshot tokens)
	// For now, search for a BTC-related token price across all accounts
	btcPrice := findBTCPrice(allAccounts)
	indicators = append(indicators, Indicator{
		ID:    61,
		Name:  "BTC Rate",
		Value: btcPrice,
		Unit:  "EUR",
	})

	return indicators, nil
}

func findBTCPrice(accounts []domain.FundAccountPortfolio) decimal.Decimal {
	for _, acc := range accounts {
		for _, token := range acc.Tokens {
			if lo.Contains([]string{"BTC", "WBTC", "yXLM"}, token.Asset.Code) && token.PriceInEURMTL != nil {
				price := domain.SafeParse(*token.PriceInEURMTL)
				if !price.IsZero() {
					return price
				}
			}
		}
	}
	return decimal.Zero
}
