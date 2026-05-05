package indicator

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/snapshot"
)

// DividendCalculator computes dividend-related indicators (I11, I15, I16, I17, I33, I34, I54, I55).
// Live values (I11) come from data.LiveMetrics — populated upstream by
// metrics.EnrichMetrics with sticky-fallback to the prior day on Horizon
// failures. The calculator itself makes no Horizon calls, but it does read
// historical snapshots through the supplied HistoricalData (for I16/I17/I33/I55
// dividend-chain math). Pure of network IO at this layer.
type DividendCalculator struct{}

func (c *DividendCalculator) IDs() []int          { return []int{11, 15, 16, 17, 33, 34, 54, 55} }
func (c *DividendCalculator) Dependencies() []int { return []int{5, 10} }

func (c *DividendCalculator) Calculate(ctx context.Context, data domain.FundStructureData, deps map[int]Indicator, hist *HistoricalData) ([]Indicator, error) {
	i5 := deps[5].Value   // Total Shares
	i10 := deps[10].Value // Share Market Price

	// I11: Monthly Dividends — read from snapshot. Absent ⇒ zero (legacy snapshots).
	i11 := liveValue(data.LiveMetrics, func(m *domain.FundLiveMetrics) *string { return m.MonthlyDividends })

	// I15: DPS = I11 / I5
	i15 := decimal.Zero
	if !i5.IsZero() {
		i15 = i11.Div(i5)
	}

	// I55: Price Year Ago — use GetNearestBefore to find snapshot closest to 365 days ago
	i55 := decimal.Zero
	if hist != nil {
		i55 = fetchPriceYearAgo(ctx, hist)
	}

	// I54: Annual DPS = I15 * 12 (annualized monthly DPS)
	i54 := i15.Mul(decimal.NewFromInt(12))

	// Gather 12 months of monthly dividend values for I16 and I33
	var divs12m []decimal.Decimal
	if hist != nil {
		divs12m = fetchMonthlyDividends12m(ctx, hist)
	}

	// I33: EPS = Median(monthly_divs) * 12 / I5
	i33 := decimal.Zero
	if !i5.IsZero() && len(divs12m) > 0 {
		i33 = Median(divs12m).Mul(decimal.NewFromInt(12)).Div(i5)
	}

	// I16: ADY1 = (Median(monthly_divs) * 12) / (I5 * I10 * (1 - (I10-I55)/I55)) * 100
	i16 := decimal.Zero
	if !i5.IsZero() && !i10.IsZero() && !i55.IsZero() && len(divs12m) > 0 {
		annualDivs := Median(divs12m).Mul(decimal.NewFromInt(12))
		deltaP := i10.Sub(i55).Div(i55)
		factor := decimal.NewFromInt(1).Sub(deltaP)
		if !factor.IsZero() {
			denom := i5.Mul(i10).Mul(factor)
			if !denom.IsZero() {
				i16 = annualDivs.Div(denom).Mul(decimal.NewFromInt(100))
			}
		}
	}

	// I17: ADY2 = (I54 / I55) * 100
	i17 := decimal.Zero
	if !i55.IsZero() {
		i17 = i54.Div(i55).Mul(decimal.NewFromInt(100))
	}

	// I34: P/E = I10 / I54
	i34 := decimal.Zero
	if !i54.IsZero() {
		i34 = i10.Div(i54)
	}

	return []Indicator{
		NewIndicator(11, i11, "", ""),
		NewIndicator(15, i15, "", ""),
		NewIndicator(16, i16, "", ""),
		NewIndicator(17, i17, "", ""),
		NewIndicator(33, i33, "", ""),
		NewIndicator(34, i34, "", ""),
		NewIndicator(54, i54, "", ""),
		NewIndicator(55, i55, "", ""),
	}, nil
}

// fetchPriceYearAgo retrieves the MTL price from the snapshot nearest to 365 days ago.
// It checks LiveMetrics first, then falls back to scanning stored token prices,
// and finally to the indicator repository (which carries I10 history imported
// from the legacy MONITORING sheet, predating the snapshot table).
func fetchPriceYearAgo(ctx context.Context, hist *HistoricalData) decimal.Decimal {
	yearAgo := time.Now().UTC().AddDate(-1, 0, 0)
	snap, err := hist.Repo.GetNearestBefore(ctx, hist.Slug, yearAgo)
	if err != nil && !errors.Is(err, snapshot.ErrNotFound) {
		slog.Error("failed to fetch historical snapshot for price year ago", "date", yearAgo.Format("2006-01-02"), "error", err)
		return decimal.Zero
	}

	if snap != nil {
		var historicalData domain.FundStructureData
		if err := json.Unmarshal(snap.Data, &historicalData); err != nil {
			slog.Error("failed to parse historical snapshot data", "error", err)
		} else {
			if historicalData.LiveMetrics != nil && historicalData.LiveMetrics.MTLMarketPrice != nil {
				if price := domain.SafeParse(*historicalData.LiveMetrics.MTLMarketPrice); !price.IsZero() {
					return price
				}
			}
			if price := findMTLPrice(historicalData); !price.IsZero() {
				return price
			}
		}
	}

	return lookupIndicatorAt(ctx, hist, 10, yearAgo)
}

// fetchMonthlyDividends12m collects stored monthly dividend values from the last 12 months.
// For each of the past 12 calendar months we first try the snapshot's LiveMetrics,
// then fall back to I11 in the indicator repository — months that predate the
// LiveMetrics rollout (Feb 2026) only have I11 in the legacy-imported indicator
// history.
func fetchMonthlyDividends12m(ctx context.Context, hist *HistoricalData) []decimal.Decimal {
	var divs []decimal.Decimal
	now := time.Now().UTC()
	for i := 1; i <= 12; i++ {
		target := now.AddDate(0, -i, 0)

		var fromSnap decimal.Decimal
		snap, err := hist.Repo.GetNearestBefore(ctx, hist.Slug, target)
		if err != nil && !errors.Is(err, snapshot.ErrNotFound) {
			slog.Error("failed to fetch snapshot for monthly dividends",
				"month", i, "target", target.Format("2006-01"), "error", err)
		} else if snap != nil {
			var data domain.FundStructureData
			if err := json.Unmarshal(snap.Data, &data); err != nil {
				slog.Error("failed to parse snapshot for monthly dividends", "month", i, "error", err)
			} else if data.LiveMetrics != nil && data.LiveMetrics.MonthlyDividends != nil {
				fromSnap = domain.SafeParse(*data.LiveMetrics.MonthlyDividends)
			}
		}

		if !fromSnap.IsZero() {
			divs = append(divs, fromSnap)
			continue
		}

		if v := lookupIndicatorAt(ctx, hist, 11, target); !v.IsZero() {
			divs = append(divs, v)
		}
	}
	return divs
}

// lookupIndicatorAt returns the value of indicator id at-or-before target from
// the indicator repository, or zero when the repository is absent / has no data.
func lookupIndicatorAt(ctx context.Context, hist *HistoricalData, id int, target time.Time) decimal.Decimal {
	if hist == nil || hist.IndicatorRepo == nil {
		return decimal.Zero
	}
	res, err := hist.IndicatorRepo.GetNearestBefore(ctx, hist.Slug, target)
	if err != nil {
		slog.Error("failed to lookup indicator from repository",
			"id", id, "target", target.Format("2006-01-02"), "error", err)
		return decimal.Zero
	}
	ind, ok := res[id]
	if !ok {
		return decimal.Zero
	}
	return ind.Value
}

// findMTLPrice scans all accounts in the snapshot for a stored MTL token price.
func findMTLPrice(data domain.FundStructureData) decimal.Decimal {
	allAccounts := lo.Flatten([][]domain.FundAccountPortfolio{
		data.Accounts,
		data.MutualFunds,
		data.OtherAccounts,
	})
	for _, acc := range allAccounts {
		for _, token := range acc.Tokens {
			if token.Asset.Code == "MTL" && token.Asset.Issuer == domain.IssuerAddress && token.PriceInEURMTL != nil {
				price := domain.SafeParse(*token.PriceInEURMTL)
				if !price.IsZero() {
					return price
				}
			}
		}
	}
	return decimal.Zero
}
