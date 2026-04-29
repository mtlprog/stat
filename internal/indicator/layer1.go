package indicator

import (
	"context"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// Layer1Calculator computes derived indicators (I3, I4, I5, I6, I7, I10, I49)
// purely from snapshot data. All live values come from data.LiveMetrics, which
// is populated upstream by metrics.EnrichMetrics — Layer1 itself makes no
// network calls and has no Horizon dependency.
type Layer1Calculator struct{}

func (c *Layer1Calculator) IDs() []int          { return []int{3, 4, 5, 6, 7, 10, 49} }
func (c *Layer1Calculator) Dependencies() []int { return []int{51, 52, 53, 58, 59, 60} }

func (c *Layer1Calculator) Calculate(_ context.Context, data domain.FundStructureData, deps map[int]Indicator, _ *HistoricalData) ([]Indicator, error) {
	// I3: Assets Value MTLF = I51 + I52 + I53 + I58 + I59 + I60
	i3 := deps[51].Value.Add(deps[52].Value).Add(deps[53].Value).
		Add(deps[58].Value).Add(deps[59].Value).Add(deps[60].Value)

	// I4: Operating Balance = sum of (EURMTL balances + XLM balances converted to EURMTL) across subfond accounts
	i4 := calculateOperatingBalance(data)

	// Live values come from the snapshot's LiveMetrics block, which is filled
	// upstream by metrics.EnrichMetrics with sticky-fallback to yesterday's
	// persisted value on Horizon failures. A nil field here means the snapshot
	// pre-dates the LiveMetrics rollout — the indicator resolves to zero, which
	// is the documented behaviour for backfilled history (see CLAUDE.md).
	i6 := liveValue(data.LiveMetrics, func(m *domain.FundLiveMetrics) *string { return m.MTLCirculation })
	i7 := liveValue(data.LiveMetrics, func(m *domain.FundLiveMetrics) *string { return m.MTLRECTCirculation })
	i10 := liveValue(data.LiveMetrics, func(m *domain.FundLiveMetrics) *string { return m.MTLMarketPrice })
	i49 := liveValue(data.LiveMetrics, func(m *domain.FundLiveMetrics) *string { return m.MTLRECTMarketPrice })

	// I5: Total shares = I6 + I7
	i5 := i6.Add(i7)

	return []Indicator{
		NewIndicator(3, i3, "", ""),
		NewIndicator(4, i4, "", ""),
		NewIndicator(5, i5, "", ""),
		NewIndicator(6, i6, "", ""),
		NewIndicator(7, i7, "", ""),
		NewIndicator(10, i10, "", ""),
		NewIndicator(49, i49, "", ""),
	}, nil
}

// liveValue extracts a parsed Decimal from FundLiveMetrics. Returns zero when
// metrics are absent or the field is nil — calculators must treat both cases
// the same.
func liveValue(m *domain.FundLiveMetrics, get func(*domain.FundLiveMetrics) *string) decimal.Decimal {
	if m == nil {
		return decimal.Zero
	}
	v := get(m)
	if v == nil {
		return decimal.Zero
	}
	return domain.SafeParse(*v)
}

func calculateOperatingBalance(data domain.FundStructureData) decimal.Decimal {
	total := decimal.Zero
	for _, acc := range data.Accounts {
		if acc.Type == domain.AccountTypeSubfond {
			for _, token := range acc.Tokens {
				if token.Asset.Code == "EURMTL" {
					total = total.Add(domain.SafeParse(token.Balance))
				}
			}
			xlmPrice := domain.SafeParse(lo.FromPtr(acc.XLMPriceInEURMTL))
			xlmBal := domain.SafeParse(acc.XLMBalance)
			total = total.Add(xlmBal.Mul(xlmPrice))
		}
	}
	return total
}
