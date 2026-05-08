package indicator

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// bppValue is the manually-managed Bitcoin Purchase Price for I39. The real
// formula (Σ EURMTL spent on BTC / BTCMTL held) is deferred — see
// docs/indicators.md Q1. Edit this constant and redeploy when product wants
// the value bumped; existing fund_indicators history is intentionally not
// rewritten on changes.
const bppValue = 24000

// BPPCalculator emits I39 as a constant. No deps, no data dependency — every
// snapshot date gets the same number.
type BPPCalculator struct{}

func (c *BPPCalculator) IDs() []int          { return []int{39} }
func (c *BPPCalculator) Dependencies() []int { return nil }

func (c *BPPCalculator) Calculate(_ context.Context, _ domain.FundStructureData, _ map[int]Indicator, _ *HistoricalData) ([]Indicator, error) {
	return []Indicator{NewIndicator(39, decimal.NewFromInt(bppValue), "", "")}, nil
}
