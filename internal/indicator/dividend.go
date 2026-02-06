package indicator

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// DividendCalculator computes dividend-related indicators (I11, I15, I16, I17, I33, I34, I54, I55).
type DividendCalculator struct{}

func (c *DividendCalculator) IDs() []int          { return []int{11, 15, 16, 17, 33, 34, 54, 55} }
func (c *DividendCalculator) Dependencies() []int { return []int{5, 10} }

func (c *DividendCalculator) Calculate(_ context.Context, _ domain.FundStructureData, deps map[int]Indicator, _ *HistoricalData) ([]Indicator, error) {
	i5 := deps[5].Value   // Total Shares
	i10 := deps[10].Value // Share Market Price

	// I11: Monthly Dividends (placeholder — returns zero, requires payment history from Horizon)
	i11 := decimal.Zero

	// I15: DPS = I11 / I5
	i15 := decimal.Zero
	if !i5.IsZero() {
		i15 = i11.Div(i5)
	}

	// I54: Annual DPS (placeholder — returns zero, requires sum of 12 monthly DPS from historical snapshots)
	i54 := decimal.Zero

	// I55: Price year ago (placeholder — returns zero, requires historical snapshot)
	i55 := decimal.Zero

	// I16: ADY1 (placeholder — returns zero, Annual Dividend Yield forecast)
	i16 := decimal.Zero

	// I17: ADY2 = (I54 / I55) * 100
	i17 := decimal.Zero
	if !i55.IsZero() {
		i17 = i54.Div(i55).Mul(decimal.NewFromInt(100))
	}

	// I33: EPS (placeholder — returns zero, requires median of monthly dividends)
	i33 := decimal.Zero

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
