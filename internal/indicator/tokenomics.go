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

// TokenomicsHorizon provides access to Horizon for balance-filtered holder counts, balances, and volumes.
type TokenomicsHorizon interface {
	FetchAssetHolderCountByBalance(ctx context.Context, asset domain.AssetInfo, minBalance decimal.Decimal) (int, error)
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

	// I24: EURMTL holder count — prefer stored value from snapshot, fall back to live Horizon.
	i24 := decimal.Zero
	if data.LiveMetrics != nil && data.LiveMetrics.EURMTLParticipants != nil {
		i24 = domain.SafeParse(*data.LiveMetrics.EURMTLParticipants)
	} else if c.Horizon != nil {
		count, err := c.Horizon.FetchAssetHolderCountByBalance(ctx, domain.EURMTLAsset(), minNonZero)
		if err != nil {
			slog.Warn("failed to fetch asset holders", "asset", "EURMTL", "error", err)
		} else {
			i24 = decimal.NewFromInt(int64(count))
		}
	}

	// Fetch holder balances once for both I27 (count) and I23 (median).
	// FetchAssetHolderBalancesByBalance returns map[account_id]balance, so we get
	// both IDs and balances in a single pair of Horizon pagination sweeps.
	// I27 prefers stored LiveMetrics value; I23 still requires live data (no median in snapshot).
	var mergedHolders map[string]decimal.Decimal
	i27 := decimal.Zero
	i27FromLiveMetrics := false
	i23 := decimal.Zero
	if data.LiveMetrics != nil && data.LiveMetrics.MTLShareholders != nil {
		i27 = domain.SafeParse(*data.LiveMetrics.MTLShareholders)
		i27FromLiveMetrics = true
	}
	if c.Horizon != nil {
		mtlAsset := domain.NewAssetInfo("MTL", domain.IssuerAddress)
		mtlrectAsset := domain.NewAssetInfo("MTLRECT", domain.IssuerAddress)

		mtlBalances, err1 := c.Horizon.FetchAssetHolderBalancesByBalance(ctx, mtlAsset, minOne)
		if err1 != nil {
			slog.Warn("failed to fetch MTL holder balances", "error", err1)
		}

		mtlrectBalances, err2 := c.Horizon.FetchAssetHolderBalancesByBalance(ctx, mtlrectAsset, minOne)
		if err2 != nil {
			slog.Warn("failed to fetch MTLRECT holder balances", "error", err2)
		}

		if err1 != nil || err2 != nil {
			slog.Warn("I23/I27/I21/I22 will be zero due to failed Horizon calls",
				"mtl_error", err1, "mtlrect_error", err2)
		} else {
			// Merge by account ID: each holder's total = MTL balance + MTLRECT balance
			mergedHolders = make(map[string]decimal.Decimal, len(mtlBalances)+len(mtlrectBalances))
			for id, bal := range mtlBalances {
				mergedHolders[id] = bal
			}
			for id, bal := range mtlrectBalances {
				mergedHolders[id] = mergedHolders[id].Add(bal)
			}

			// I27: count of unique holders — only set from live data when LiveMetrics
			// did not provide the value (a legit stored 0 must not be overwritten).
			if !i27FromLiveMetrics {
				i27 = decimal.NewFromInt(int64(len(mergedHolders)))
			}

			// I23: median of merged shareholdings
			// Note: minBalance is applied per-asset, not on the merged total.
			// An account must hold >= minBalance of at least one asset to be included.
			if len(mergedHolders) > 0 {
				values := make([]decimal.Decimal, 0, len(mergedHolders))
				for _, bal := range mergedHolders {
					values = append(values, bal)
				}
				i23 = Median(values)
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
