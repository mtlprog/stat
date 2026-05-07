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

// DividendCalculator computes dividend-related indicators (I11, I15, I17, I34,
// I43, I54, I55). Live I11 comes from data.LiveMetrics — populated upstream by
// metrics.EnrichMetrics with sticky-fallback to the prior day on Horizon
// failures. The calculator itself makes no Horizon calls, but it does read
// historical snapshots through the supplied HistoricalData for I55. Pure of
// network IO at this layer.
type DividendCalculator struct{}

func (c *DividendCalculator) IDs() []int          { return []int{11, 15, 17, 34, 43, 54, 55} }
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

	// I43: Total ROI = ((I10 - I55) + I54) / I55 * 100
	i43 := decimal.Zero
	if !i55.IsZero() {
		i43 = i10.Sub(i55).Add(i54).Div(i55).Mul(decimal.NewFromInt(100))
	}

	return []Indicator{
		NewIndicator(11, i11, "", ""),
		NewIndicator(15, i15, "", ""),
		NewIndicator(17, i17, "", ""),
		NewIndicator(34, i34, "", ""),
		NewIndicator(43, i43, "", ""),
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
