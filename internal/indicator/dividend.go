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

// fetchPriceYearAgo retrieves the MTL price from the snapshot nearest to 365
// days ago. The chain is: snapshot LiveMetrics → token-price scan in the same
// snapshot → I10 history in the indicator repository. Any failure mode of the
// snapshot path (ErrNotFound or transient DB error) falls through to the
// indicator-repo lookup — the indicator table carries continuous I10 history
// from the legacy MONITORING import and is the authoritative source for dates
// preceding the LiveMetrics rollout. Returns zero (and logs Warn) only when
// every source is exhausted.
func fetchPriceYearAgo(ctx context.Context, hist *HistoricalData) decimal.Decimal {
	yearAgo := time.Now().UTC().AddDate(-1, 0, 0)

	if price := snapshotPriceYearAgo(ctx, hist, yearAgo); !price.IsZero() {
		return price
	}

	if price := lookupIndicatorAt(ctx, hist, 10, yearAgo); !price.IsZero() {
		return price
	}

	slog.Warn("I55 unresolvable — no MTL price found in snapshot, tokens, or indicator history",
		"slug", hist.Slug, "date", yearAgo.Format("2006-01-02"))
	return decimal.Zero
}

// snapshotPriceYearAgo returns the year-ago MTL price from the snapshot path
// (LiveMetrics → tokens), or zero on any miss. Errors are logged but never
// propagate — the caller chains to the indicator-repo fallback.
func snapshotPriceYearAgo(ctx context.Context, hist *HistoricalData, yearAgo time.Time) decimal.Decimal {
	snap, err := hist.Repo.GetNearestBefore(ctx, hist.Slug, yearAgo)
	if err != nil {
		if !errors.Is(err, snapshot.ErrNotFound) {
			slog.Error("snapshot lookup for price year ago failed, falling through to indicator repo",
				"slug", hist.Slug, "date", yearAgo.Format("2006-01-02"), "error", err)
		}
		return decimal.Zero
	}
	if snap == nil {
		return decimal.Zero
	}

	var data domain.FundStructureData
	if err := json.Unmarshal(snap.Data, &data); err != nil {
		slog.Error("failed to parse historical snapshot data, falling through to indicator repo",
			"slug", hist.Slug, "error", err)
		return decimal.Zero
	}

	if data.LiveMetrics != nil && data.LiveMetrics.MTLMarketPrice != nil {
		if price := domain.SafeParse(*data.LiveMetrics.MTLMarketPrice); !price.IsZero() {
			return price
		}
	}
	return findMTLPrice(data)
}

// fetchMonthlyDividends12m collects stored monthly dividend values from the
// last 12 months. For each of the past 12 calendar months we first try the
// snapshot's LiveMetrics, then fall back to I11 from the indicator repository
// (a single batched GetHistory call covers all 12 targets). Months whose value
// resolves to "missing in both sources" are skipped *and logged* — the median
// downstream is then computed over fewer samples, but the operator gets a
// signal.
func fetchMonthlyDividends12m(ctx context.Context, hist *HistoricalData) []decimal.Decimal {
	now := time.Now().UTC()
	indHist := loadI11History(ctx, hist, now.AddDate(-1, -1, 0))

	var divs []decimal.Decimal
	var dropped []string
	for i := 1; i <= 12; i++ {
		target := now.AddDate(0, -i, 0)

		if v, ok := monthlyDividendFromSnapshot(ctx, hist, target, i); ok {
			divs = append(divs, v)
			continue
		}

		if v, ok := nearestI11(indHist, target); ok {
			divs = append(divs, v)
			continue
		}

		dropped = append(dropped, target.Format("2006-01"))
	}

	if len(dropped) > 0 {
		slog.Warn("monthly dividends: some months missing from both snapshot and indicator history",
			"slug", hist.Slug, "dropped_months", dropped, "kept", len(divs))
	}
	return divs
}

// monthlyDividendFromSnapshot returns LiveMetrics.MonthlyDividends from the
// nearest snapshot before target, or (zero, false) on any miss. Errors are
// logged but never propagate; the caller chains to the indicator-repo path.
// "Found but legitimately zero" returns (0, true) — that's a real data point.
func monthlyDividendFromSnapshot(ctx context.Context, hist *HistoricalData, target time.Time, month int) (decimal.Decimal, bool) {
	snap, err := hist.Repo.GetNearestBefore(ctx, hist.Slug, target)
	if err != nil {
		if !errors.Is(err, snapshot.ErrNotFound) {
			slog.Error("snapshot lookup for monthly dividends failed, falling through to indicator repo",
				"slug", hist.Slug, "month", month, "target", target.Format("2006-01"), "error", err)
		}
		return decimal.Zero, false
	}
	if snap == nil {
		return decimal.Zero, false
	}

	var data domain.FundStructureData
	if err := json.Unmarshal(snap.Data, &data); err != nil {
		slog.Error("failed to parse snapshot for monthly dividends, falling through to indicator repo",
			"slug", hist.Slug, "month", month, "error", err)
		return decimal.Zero, false
	}
	if data.LiveMetrics == nil || data.LiveMetrics.MonthlyDividends == nil {
		return decimal.Zero, false
	}
	return domain.SafeParse(*data.LiveMetrics.MonthlyDividends), true
}

// loadI11History fetches all I11 points at or after `from` in a single query
// so the per-month loop in fetchMonthlyDividends12m doesn't issue 12 separate
// GetNearestBefore calls. Returns nil on any failure or when the repository
// is absent — the caller treats that the same as "no I11 in history".
func loadI11History(ctx context.Context, hist *HistoricalData, from time.Time) []HistoryPoint {
	if hist == nil || hist.IndicatorRepo == nil {
		return nil
	}
	pts, err := hist.IndicatorRepo.GetHistory(ctx, hist.Slug, []int{11}, from)
	if err != nil {
		slog.Error("failed to load I11 history from indicator repo",
			"slug", hist.Slug, "from", from.Format("2006-01-02"), "error", err)
		return nil
	}
	return pts
}

// nearestI11 returns the I11 value at-or-before target from the prefetched
// history slice (assumed sorted ascending by date — that's what GetHistory
// returns). Returns (0, false) when no point is at-or-before target.
func nearestI11(history []HistoryPoint, target time.Time) (decimal.Decimal, bool) {
	var found *HistoryPoint
	for i := range history {
		if history[i].IndicatorID != 11 {
			continue
		}
		if history[i].SnapshotDate.After(target) {
			break
		}
		found = &history[i]
	}
	if found == nil {
		return decimal.Zero, false
	}
	return found.Value, true
}

// lookupIndicatorAt returns the latest value of indicator id at-or-before
// target from the indicator repository. Returns zero when the repository is
// absent, the lookup errors, or no row exists. Note: each call issues a full
// GetNearestBefore (which scans every indicator ID, not just the requested
// one) — for tight loops over a date range, batch via GetHistory instead.
func lookupIndicatorAt(ctx context.Context, hist *HistoricalData, id int, target time.Time) decimal.Decimal {
	if hist == nil || hist.IndicatorRepo == nil {
		return decimal.Zero
	}
	res, err := hist.IndicatorRepo.GetNearestBefore(ctx, hist.Slug, target)
	if err != nil {
		slog.Error("failed to lookup indicator from repository",
			"slug", hist.Slug, "id", id, "target", target.Format("2006-01-02"), "error", err)
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
