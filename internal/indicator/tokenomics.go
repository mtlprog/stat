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
		count, err := c.Horizon.FetchAssetHolders(ctx, domain.EURMTLAsset())
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
		mtlapAsset := domain.AssetInfo{Code: "MTLAP", Issuer: domain.IssuerAddress, Type: domain.AssetTypeCreditAlphanum4}
		count, err := c.Horizon.FetchAssetHolders(ctx, mtlapAsset)
		if err == nil {
			i40 = decimal.NewFromInt(int64(count))
		}
	}

	return []Indicator{
		NewIndicator(18, i18, "", ""),
		NewIndicator(21, i21, "", ""),
		NewIndicator(22, i22, "", ""),
		NewIndicator(23, i23, "", ""),
		NewIndicator(24, i24, "", ""),
		NewIndicator(25, i25, "", ""),
		NewIndicator(26, i26, "", ""),
		NewIndicator(27, i27, "", ""),
		NewIndicator(40, i40, "", ""),
	}, nil
}
