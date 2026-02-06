package indicator

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// Layer2Calculator computes ratio indicators (I1, I2, I8, I30).
type Layer2Calculator struct{}

func (c *Layer2Calculator) IDs() []int          { return []int{1, 2, 8, 30} }
func (c *Layer2Calculator) Dependencies() []int { return []int{3, 5, 10, 61} }

func (c *Layer2Calculator) Calculate(_ context.Context, _ domain.FundStructureData, deps map[int]Indicator, _ *HistoricalData) ([]Indicator, error) {
	i5 := deps[5].Value   // Total Shares
	i10 := deps[10].Value // Share Market Price
	i3 := deps[3].Value   // Assets Value MTLF
	i61 := deps[61].Value // BTC Rate

	// I1: Market Cap EUR = I5 * I10
	i1 := i5.Mul(i10)

	// I2: Market Cap BTC = I1 / I61
	i2 := decimal.Zero
	if !i61.IsZero() {
		i2 = i1.Div(i61)
	}

	// I8: Share Book Value = I3 / I5
	i8 := decimal.Zero
	if !i5.IsZero() {
		i8 = i3.Div(i5)
	}

	// I30: Price/Book Ratio = I10 / I8
	i30 := decimal.Zero
	if !i8.IsZero() {
		i30 = i10.Div(i8)
	}

	return []Indicator{
		NewIndicator(1, i1, "", ""),
		NewIndicator(2, i2, "", ""),
		NewIndicator(8, i8, "", ""),
		NewIndicator(30, i30, "", ""),
	}, nil
}
