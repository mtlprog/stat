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

// TokenomicsHorizon provides access to Horizon for asset holder counts and IDs.
type TokenomicsHorizon interface {
	FetchAssetHolders(ctx context.Context, asset domain.AssetInfo) (int, error)
	FetchAllAssetHolderIDs(ctx context.Context, asset domain.AssetInfo) ([]string, error)
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

	// I27: Accounts holding >= 1 MTL or >= 1 MTLRECT (union to avoid double-counting)
	i27 := decimal.Zero
	if c.Horizon != nil {
		mtlAsset := domain.NewAssetInfo("MTL", domain.IssuerAddress)
		mtlrectAsset := domain.NewAssetInfo("MTLRECT", domain.IssuerAddress)

		mtlIDs, err1 := c.Horizon.FetchAllAssetHolderIDs(ctx, mtlAsset)
		if err1 != nil {
			slog.Warn("failed to fetch MTL holder IDs", "error", err1)
		}

		mtlrectIDs, err2 := c.Horizon.FetchAllAssetHolderIDs(ctx, mtlrectAsset)
		if err2 != nil {
			slog.Warn("failed to fetch MTLRECT holder IDs", "error", err2)
		}

		if err1 == nil && err2 == nil {
			holderSet := make(map[string]struct{}, len(mtlIDs)+len(mtlrectIDs))
			for _, id := range mtlIDs {
				holderSet[id] = struct{}{}
			}
			for _, id := range mtlrectIDs {
				holderSet[id] = struct{}{}
			}
			i27 = decimal.NewFromInt(int64(len(holderSet)))
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
		count, err := c.Horizon.FetchAssetHolders(ctx, domain.MTLAPAsset())
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
