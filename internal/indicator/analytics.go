package indicator

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// AnalyticsCalculator computes analytics indicators (I43-I48).
type AnalyticsCalculator struct{}

func (c *AnalyticsCalculator) IDs() []int          { return []int{43, 44, 45, 46, 47, 48} }
func (c *AnalyticsCalculator) Dependencies() []int { return []int{3, 5, 10, 54, 55, 61} }

func (c *AnalyticsCalculator) Calculate(_ context.Context, _ domain.FundStructureData, deps map[int]Indicator, _ *HistoricalData) ([]Indicator, error) {
	i10 := deps[10].Value // Share Market Price
	i54 := deps[54].Value // Annual DPS
	i55 := deps[55].Value // Price Year Ago
	i3 := deps[3].Value   // Assets Value MTLF
	i5 := deps[5].Value   // Total Shares

	// I43: Total ROI = ((I10 - I55) + I54) / I55 * 100
	i43 := decimal.Zero
	if !i55.IsZero() {
		i43 = i10.Sub(i55).Add(i54).Div(i55).Mul(decimal.NewFromInt(100))
	}

	// I44: Beta (requires historical time series — placeholder)
	i44 := decimal.Zero

	// I45: Sharpe (requires historical time series — placeholder)
	i45 := decimal.Zero

	// I46: Sortino (requires historical time series — placeholder)
	i46 := decimal.Zero

	// I47: VaR (requires historical time series — placeholder)
	i47 := decimal.Zero

	// I48: D/BV (Annual DPS / Book Value per Share) = I54 / (I3 / I5)
	i48 := decimal.Zero
	if !i5.IsZero() && !i3.IsZero() {
		avPerShare := i3.Div(i5)
		if !avPerShare.IsZero() {
			i48 = i54.Div(avPerShare)
		}
	}

	return []Indicator{
		NewIndicator(43, i43, "", ""),
		NewIndicator(44, i44, "", ""),
		NewIndicator(45, i45, "", ""),
		NewIndicator(46, i46, "", ""),
		NewIndicator(47, i47, "", ""),
		NewIndicator(48, i48, "", ""),
	}, nil
}
