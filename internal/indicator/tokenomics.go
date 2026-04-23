package indicator

import (
	"context"
	"log/slog"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// TokenomicsCalculator computes tokenomics indicators (I18, I21-I27, I40).
type TokenomicsCalculator struct {
	Horizon TokenomicsHorizon
}

// TokenomicsHorizon provides access to Horizon for balance-filtered holder counts, IDs, balances, and volumes.
type TokenomicsHorizon interface {
	FetchAssetHolderCountByBalance(ctx context.Context, asset domain.AssetInfo, minBalance decimal.Decimal) (int, error)
	FetchAssetHolderIDsByBalance(ctx context.Context, asset domain.AssetInfo, minBalance decimal.Decimal) ([]string, error)
	FetchAssetHolderBalancesByBalance(ctx context.Context, asset domain.AssetInfo, minBalance decimal.Decimal) (map[string]decimal.Decimal, error)
	FetchEURMTLPaymentVolume(ctx context.Context, since time.Time) (decimal.Decimal, error)
}

func (c *TokenomicsCalculator) IDs() []int          { return []int{18, 21, 22, 23, 24, 25, 26, 27, 40} }
func (c *TokenomicsCalculator) Dependencies() []int { return []int{1, 5} }

func (c *TokenomicsCalculator) Calculate(ctx context.Context, data domain.FundStructureData, deps map[int]Indicator, _ *HistoricalData) ([]Indicator, error) {
	i1 := deps[1].Value // Market Cap
	i5 := deps[5].Value // Total Shares

	minNonZero := decimal.New(1, -7) // 0.0000001 = 1 stroop, smallest non-zero Stellar balance
	minOne := decimal.NewFromInt(1)  // balance >= 1

	// I24: EURMTL holder count (accounts with balance >= 1 stroop)
	i24 := decimal.Zero
	if c.Horizon != nil {
		count, err := c.Horizon.FetchAssetHolderCountByBalance(ctx, domain.EURMTLAsset(), minNonZero)
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

		mtlIDs, err1 := c.Horizon.FetchAssetHolderIDsByBalance(ctx, mtlAsset, minOne)
		if err1 != nil {
			slog.Warn("failed to fetch MTL holder IDs", "error", err1)
		}

		mtlrectIDs, err2 := c.Horizon.FetchAssetHolderIDsByBalance(ctx, mtlrectAsset, minOne)
		if err2 != nil {
			slog.Warn("failed to fetch MTLRECT holder IDs", "error", err2)
		}

		if err1 != nil || err2 != nil {
			slog.Warn("I27/I21/I22 will be zero due to failed Horizon calls",
				"mtl_error", err1, "mtlrect_error", err2)
		} else {
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

	// I23: Median shareholding size — median balance across union of MTL+MTLRECT holders (>= 1)
	i23 := decimal.Zero
	if c.Horizon != nil {
		i23 = c.fetchMedianShareholding(ctx, minOne)
	}

	// I25: EURMTL daily payment volume — prefer stored value, fall back to live Horizon
	i25 := decimal.Zero
	if data.LiveMetrics != nil && data.LiveMetrics.EURMTLDailyVolume != nil {
		i25 = domain.SafeParse(*data.LiveMetrics.EURMTLDailyVolume)
	} else if c.Horizon != nil {
		since := time.Now().AddDate(0, 0, -1)
		vol, err := c.Horizon.FetchEURMTLPaymentVolume(ctx, since)
		if err != nil {
			slog.Warn("failed to fetch EURMTL daily volume", "error", err)
		} else {
			i25 = vol
		}
	}

	// I26: EURMTL 30d payment volume — prefer stored value, fall back to live Horizon
	i26 := decimal.Zero
	if data.LiveMetrics != nil && data.LiveMetrics.EURMTL30dVolume != nil {
		i26 = domain.SafeParse(*data.LiveMetrics.EURMTL30dVolume)
	} else if c.Horizon != nil {
		since := time.Now().AddDate(0, 0, -30)
		vol, err := c.Horizon.FetchEURMTLPaymentVolume(ctx, since)
		if err != nil {
			slog.Warn("failed to fetch EURMTL 30d volume", "error", err)
		} else {
			i26 = vol
		}
	}

	// I40: MTLAP holder count (accounts with balance >= 1)
	i40 := decimal.Zero
	if c.Horizon != nil {
		count, err := c.Horizon.FetchAssetHolderCountByBalance(ctx, domain.MTLAPAsset(), minOne)
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

// fetchMedianShareholding computes the median shareholding across the union of
// MTL and MTLRECT holders with balance >= minBalance. Each holder's total is
// the sum of their MTL + MTLRECT balances, merged by account ID.
func (c *TokenomicsCalculator) fetchMedianShareholding(ctx context.Context, minBalance decimal.Decimal) decimal.Decimal {
	mtlAsset := domain.NewAssetInfo("MTL", domain.IssuerAddress)
	mtlrectAsset := domain.NewAssetInfo("MTLRECT", domain.IssuerAddress)

	mtlBalances, err1 := c.Horizon.FetchAssetHolderBalancesByBalance(ctx, mtlAsset, minBalance)
	if err1 != nil {
		slog.Warn("failed to fetch MTL holder balances for I23", "error", err1)
		return decimal.Zero
	}

	mtlrectBalances, err2 := c.Horizon.FetchAssetHolderBalancesByBalance(ctx, mtlrectAsset, minBalance)
	if err2 != nil {
		slog.Warn("failed to fetch MTLRECT holder balances for I23", "error", err2)
		return decimal.Zero
	}

	// Merge by account ID: each holder's total = MTL balance + MTLRECT balance
	merged := make(map[string]decimal.Decimal, len(mtlBalances)+len(mtlrectBalances))
	for id, bal := range mtlBalances {
		merged[id] = bal
	}
	for id, bal := range mtlrectBalances {
		merged[id] = merged[id].Add(bal)
	}

	if len(merged) == 0 {
		return decimal.Zero
	}

	values := make([]decimal.Decimal, 0, len(merged))
	for _, bal := range merged {
		values = append(values, bal)
	}

	return Median(values)
}
