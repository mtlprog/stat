package indicator

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// TokenomicsCalculator computes tokenomics indicators (I18, I21-I27, I40).
type TokenomicsCalculator struct {
	Horizon TokenomicsHorizon
}

// TokenomicsHorizon provides access to Horizon for asset holder counts.
type TokenomicsHorizon interface {
	FetchAssetHolders(ctx context.Context, asset domain.AssetInfo) (int, error)
}

func (c *TokenomicsCalculator) IDs() []int          { return []int{18, 21, 22, 23, 24, 25, 26, 27, 40} }
func (c *TokenomicsCalculator) Dependencies() []int { return []int{1, 5} }

func (c *TokenomicsCalculator) Calculate(ctx context.Context, _ domain.FundStructureData, deps map[int]Indicator, _ *HistoricalData) ([]Indicator, error) {
	i1 := deps[1].Value // Market Cap
	i5 := deps[5].Value // Total Shares

	// I24: EURMTL holder count
	i24 := decimal.Zero
	if c.Horizon != nil {
		count, err := c.Horizon.FetchAssetHolders(ctx, domain.EURMTLAsset)
		if err == nil {
			i24 = decimal.NewFromInt(int64(count))
		}
	}

	// I27: Holders with >= 1 share (placeholder — requires scanning all holders)
	i27 := decimal.Zero

	// I18: Shareholders by EURMTL (dividend recipients — placeholder)
	i18 := decimal.Zero

	// I21: Average Shareholding = I5 / I27
	i21 := decimal.Zero
	if !i27.IsZero() {
		i21 = i5.Div(i27)
	}

	// I22: Average Share Price = I1 / I27
	i22 := decimal.Zero
	if !i27.IsZero() {
		i22 = i1.Div(i27)
	}

	// I23: Median shareholding size (requires full holder list — placeholder)
	i23 := decimal.Zero

	// I25: EURMTL payment per day (placeholder — requires payment history)
	i25 := decimal.Zero

	// I26: EURMTL payment total 30d (placeholder)
	i26 := decimal.Zero

	// I40: MTLAP holder count
	i40 := decimal.Zero
	if c.Horizon != nil {
		mtlapAsset := domain.AssetInfo{Code: "MTLAP", Issuer: domain.EURMTLAsset.Issuer, Type: domain.AssetTypeCreditAlphanum4}
		count, err := c.Horizon.FetchAssetHolders(ctx, mtlapAsset)
		if err == nil {
			i40 = decimal.NewFromInt(int64(count))
		}
	}

	return []Indicator{
		{ID: 18, Name: "Shareholders by EURMTL", Value: i18, Unit: "accounts"},
		{ID: 21, Name: "Average Shareholding", Value: i21, Unit: "shares"},
		{ID: 22, Name: "Average Share Price", Value: i22, Unit: "EURMTL"},
		{ID: 23, Name: "Median Shareholding", Value: i23, Unit: "shares"},
		{ID: 24, Name: "EURMTL Participants", Value: i24, Unit: "accounts"},
		{ID: 25, Name: "EURMTL Daily Volume", Value: i25, Unit: "EURMTL"},
		{ID: 26, Name: "EURMTL 30d Volume", Value: i26, Unit: "EURMTL"},
		{ID: 27, Name: "MTL Shareholders (>=1)", Value: i27, Unit: "accounts"},
		{ID: 40, Name: "Association Participants", Value: i40, Unit: "accounts"},
	}, nil
}
