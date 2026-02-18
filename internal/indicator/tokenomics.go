package indicator

import (
	"context"
	"log/slog"

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
		if err != nil {
			slog.Warn("failed to fetch asset holders", "asset", "EURMTL", "error", err)
		} else {
			i24 = decimal.NewFromInt(int64(count))
		}
	}

	// I27: Accounts holding >= 1 MTL or >= 1 MTLRECT (sum of both holder counts; may overcount accounts holding both)
	i27 := decimal.Zero
	if c.Horizon != nil {
		mtlAsset := domain.NewAssetInfo("MTL", domain.IssuerAddress)
		mtlCount, err := c.Horizon.FetchAssetHolders(ctx, mtlAsset)
		if err != nil {
			slog.Warn("failed to fetch MTL holders", "error", err)
		} else {
			mtlrectAsset := domain.NewAssetInfo("MTLRECT", domain.IssuerAddress)
			mtlrectCount, err := c.Horizon.FetchAssetHolders(ctx, mtlrectAsset)
			if err != nil {
				slog.Warn("failed to fetch MTLRECT holders", "error", err)
			} else {
				i27 = decimal.NewFromInt(int64(mtlCount + mtlrectCount))
			}
		}
	}

	// I18: Shareholders by EURMTL (placeholder - returns zero, requires dividend recipient data)
	i18 := decimal.Zero

	// I21: Average Shareholding = I5 / I27
	i21 := decimal.Zero
	if !i27.IsZero() {
		i21 = i5.Div(i27)
	}

	// I22: Average Value per Shareholder = I1 / I27
	i22 := decimal.Zero
	if !i27.IsZero() {
		i22 = i1.Div(i27)
	}

	// I23: Median shareholding size (placeholder - returns zero, requires full holder list)
	i23 := decimal.Zero

	// I25: EURMTL payment per day (placeholder - returns zero, requires payment history)
	i25 := decimal.Zero

	// I26: EURMTL payment total 30d (placeholder - returns zero, requires payment history)
	i26 := decimal.Zero

	// I40: MTLAP holder count
	i40 := decimal.Zero
	if c.Horizon != nil {
		mtlapAsset := domain.NewAssetInfo("MTLAP", domain.IssuerAddress)
		count, err := c.Horizon.FetchAssetHolders(ctx, mtlapAsset)
		if err != nil {
			slog.Warn("failed to fetch asset holders", "asset", "MTLAP", "error", err)
		} else {
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
