package indicator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	// I55: Price Year Ago — chained snapshot → indicator-repo lookup. Real DB
	// errors on either side propagate up; "no data anywhere" resolves to zero.
	i55 := decimal.Zero
	if hist != nil {
		v, err := fetchPriceYearAgo(ctx, hist)
		if err != nil {
			return nil, fmt.Errorf("fetching price year ago: %w", err)
		}
		i55 = v
	}

	// I54: Annual DPS = I15 * 12 (annualized monthly DPS)
	i54 := i15.Mul(decimal.NewFromInt(12))

	// Gather 12 months of monthly dividend values for I16 and I33
	var divs12m []decimal.Decimal
	if hist != nil {
		v, err := fetchMonthlyDividends12m(ctx, hist)
		if err != nil {
			return nil, fmt.Errorf("fetching monthly dividends 12m: %w", err)
		}
		divs12m = v
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
// snapshot → I10 history in the indicator repository. Per CLAUDE.md, real DB
// errors from EITHER source are propagated as errors — they must NOT be
// conflated with "data not found" (which is the only legitimate signal to
// chain to the next source). Returns zero (and logs Info) only when both
// sources legitimately have no data.
func fetchPriceYearAgo(ctx context.Context, hist *HistoricalData) (decimal.Decimal, error) {
	yearAgo := time.Now().UTC().AddDate(-1, 0, 0)

	price, err := snapshotPriceYearAgo(ctx, hist, yearAgo)
	if err != nil {
		return decimal.Zero, err
	}
	if !price.IsZero() {
		return price, nil
	}

	price, err = lookupIndicatorAt(ctx, hist, 10, yearAgo)
	if err != nil {
		return decimal.Zero, err
	}
	if !price.IsZero() {
		return price, nil
	}

	// Info, not Error: this just means the fund has no I10 history at-or-before
	// the year-ago date. Common in a fresh DB; not actionable per-run. A real
	// DB failure would already have surfaced as a propagated error above.
	slog.Info("I55 unresolvable — no MTL price found in snapshot, tokens, or indicator history",
		"slug", hist.Slug, "date", yearAgo.Format("2006-01-02"))
	return decimal.Zero, nil
}

// snapshotPriceYearAgo returns the year-ago MTL price from the snapshot path
// (LiveMetrics → tokens). ErrNotFound and "snapshot exists but has no usable
// price" both return (zero, nil) so the caller can chain to the indicator
// repo. Real DB errors and JSON parse failures return a wrapped error — the
// caller must NOT silently fall through, because that would conflate
// infrastructure failure with absent data.
func snapshotPriceYearAgo(ctx context.Context, hist *HistoricalData, yearAgo time.Time) (decimal.Decimal, error) {
	snap, err := hist.Repo.GetNearestBefore(ctx, hist.Slug, yearAgo)
	if err != nil {
		if errors.Is(err, snapshot.ErrNotFound) {
			return decimal.Zero, nil
		}
		return decimal.Zero, fmt.Errorf("snapshot lookup for price year ago (slug=%s, date=%s): %w",
			hist.Slug, yearAgo.Format("2006-01-02"), err)
	}
	if snap == nil {
		return decimal.Zero, nil
	}

	var data domain.FundStructureData
	if err := json.Unmarshal(snap.Data, &data); err != nil {
		return decimal.Zero, fmt.Errorf("parsing historical snapshot data (slug=%s): %w", hist.Slug, err)
	}

	if data.LiveMetrics != nil && data.LiveMetrics.MTLMarketPrice != nil {
		if price := domain.SafeParse(*data.LiveMetrics.MTLMarketPrice); !price.IsZero() {
			return price, nil
		}
	}
	return findMTLPrice(data), nil
}

// fetchMonthlyDividends12m collects stored monthly dividend values from the
// last 12 months. For each month we first try the snapshot's LiveMetrics,
// then fall back to I11 from the indicator repository (a single batched
// GetHistory call covers all 12 targets). Real DB errors from either source
// propagate up — only legitimate "data not found" triggers fallback. Months
// missing in both sources are skipped *and logged* — the median downstream
// is then computed over fewer samples, but the operator gets a signal.
func fetchMonthlyDividends12m(ctx context.Context, hist *HistoricalData) ([]decimal.Decimal, error) {
	now := time.Now().UTC()

	indHist, err := loadI11History(ctx, hist, now.AddDate(-1, -1, 0))
	if err != nil {
		return nil, err
	}

	var divs []decimal.Decimal
	var dropped []string
	for i := 1; i <= 12; i++ {
		target := now.AddDate(0, -i, 0)

		v, found, err := monthlyDividendFromSnapshot(ctx, hist, target, i)
		if err != nil {
			return nil, err
		}
		if found {
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
		// Info, not Error: median over fewer points is the documented degraded
		// behaviour, not an outage. Real DB failures already propagated above.
		slog.Info("monthly dividends: some months missing from both snapshot and indicator history",
			"slug", hist.Slug, "dropped_months", dropped, "kept", len(divs))
	}
	return divs, nil
}

// monthlyDividendFromSnapshot returns LiveMetrics.MonthlyDividends from the
// nearest snapshot before target. found=false signals "no usable data here,
// try fallback" (ErrNotFound, nil snapshot, or LiveMetrics absent).
// "Found but legitimately zero" returns (0, true, nil) — that's a real data
// point. Real DB errors and JSON parse failures return a wrapped error so
// the caller does NOT silently chain to indicator-repo (CLAUDE.md: don't
// conflate not-found with infrastructure failure).
func monthlyDividendFromSnapshot(ctx context.Context, hist *HistoricalData, target time.Time, month int) (decimal.Decimal, bool, error) {
	snap, err := hist.Repo.GetNearestBefore(ctx, hist.Slug, target)
	if err != nil {
		if errors.Is(err, snapshot.ErrNotFound) {
			return decimal.Zero, false, nil
		}
		return decimal.Zero, false, fmt.Errorf("snapshot lookup for monthly dividends (slug=%s, month=%d, target=%s): %w",
			hist.Slug, month, target.Format("2006-01"), err)
	}
	if snap == nil {
		return decimal.Zero, false, nil
	}

	var data domain.FundStructureData
	if err := json.Unmarshal(snap.Data, &data); err != nil {
		return decimal.Zero, false, fmt.Errorf("parsing snapshot for monthly dividends (slug=%s, month=%d): %w",
			hist.Slug, month, err)
	}
	if data.LiveMetrics == nil || data.LiveMetrics.MonthlyDividends == nil {
		return decimal.Zero, false, nil
	}
	return domain.SafeParse(*data.LiveMetrics.MonthlyDividends), true, nil
}

// loadI11History fetches all I11 points at or after `from` in a single query
// so the per-month loop in fetchMonthlyDividends12m doesn't issue 12 separate
// GetNearestBefore calls. Returns (nil, nil) when the repository is absent.
// Real repo errors are wrapped and returned — the caller must not silently
// proceed without indicator history.
func loadI11History(ctx context.Context, hist *HistoricalData, from time.Time) ([]HistoryPoint, error) {
	if hist == nil || hist.IndicatorRepo == nil {
		return nil, nil
	}
	pts, err := hist.IndicatorRepo.GetHistory(ctx, hist.Slug, []int{11}, from)
	if err != nil {
		return nil, fmt.Errorf("loading I11 history (slug=%s, from=%s): %w",
			hist.Slug, from.Format("2006-01-02"), err)
	}
	return pts, nil
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
// target from the indicator repository. (zero, nil) when the repository is
// absent or no row exists for that id. A real repo error is wrapped and
// returned — the caller must not silently treat it as "no data" (CLAUDE.md:
// distinguish ErrNotFound from infrastructure failure).
//
// Note: each call issues a full GetNearestBefore (which scans every indicator
// id, not just the requested one) — for tight loops over a date range, batch
// via GetHistory instead.
func lookupIndicatorAt(ctx context.Context, hist *HistoricalData, id int, target time.Time) (decimal.Decimal, error) {
	if hist == nil || hist.IndicatorRepo == nil {
		return decimal.Zero, nil
	}
	res, err := hist.IndicatorRepo.GetNearestBefore(ctx, hist.Slug, target)
	if err != nil {
		return decimal.Zero, fmt.Errorf("indicator repo lookup (slug=%s, id=%d, target=%s): %w",
			hist.Slug, id, target.Format("2006-01-02"), err)
	}
	ind, ok := res[id]
	if !ok {
		return decimal.Zero, nil
	}
	return ind.Value, nil
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
