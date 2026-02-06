package indicator

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// DividendCalculator computes dividend-related indicators (I11, I15, I33, I34, I54, I55, I16, I17).
type DividendCalculator struct{}

func (c *DividendCalculator) IDs() []int { return []int{11, 15, 33, 34, 54, 55, 16, 17} }
func (c *DividendCalculator) Dependencies() []int { return []int{5, 10} }

func (c *DividendCalculator) Calculate(_ context.Context, _ domain.FundStructureData, deps map[int]Indicator, _ *HistoricalData) ([]Indicator, error) {
	i5 := deps[5].Value   // Total Shares
	i10 := deps[10].Value // Share Market Price

	// I11: Monthly Dividends (placeholder — requires payment history from Horizon)
	i11 := decimal.Zero

	// I15: DPS = I11 / I5
	i15 := decimal.Zero
	if !i5.IsZero() {
		i15 = i11.Div(i5)
	}

	// I54: Annual DPS (sum of 12 monthly DPS from historical snapshots)
	i54 := decimal.Zero

	// I55: Price year ago (from historical snapshot)
	i55 := decimal.Zero

	// I16: ADY1 (Annual Dividend Yield forecast)
	i16 := decimal.Zero

	// I17: ADY2 = (I54 / I55) * 100
	i17 := decimal.Zero
	if !i55.IsZero() {
		i17 = i54.Div(i55).Mul(decimal.NewFromInt(100))
	}

	// I33: EPS = median(monthly_divs) * 12 / I5
	i33 := decimal.Zero

	// I34: P/E = I10 / I54
	i34 := decimal.Zero
	if !i54.IsZero() {
		i34 = i10.Div(i54)
	}

	return []Indicator{
		{ID: 11, Name: "Monthly Dividends", Value: i11, Unit: "EURMTL"},
		{ID: 15, Name: "Dividends Per Share", Value: i15, Unit: "EURMTL"},
		{ID: 16, Name: "Annual Dividend Yield 1", Value: i16, Unit: "%"},
		{ID: 17, Name: "Annual Dividend Yield 2", Value: i17, Unit: "%"},
		{ID: 33, Name: "Earnings Per Share", Value: i33, Unit: "EURMTL"},
		{ID: 34, Name: "Price/Earnings Ratio", Value: i34, Unit: "ratio"},
		{ID: 54, Name: "Annual DPS", Value: i54, Unit: "EURMTL"},
		{ID: 55, Name: "Price Year Ago", Value: i55, Unit: "EURMTL"},
	}, nil
}
