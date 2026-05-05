package indicator

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// TokenomicsCalculator computes tokenomics indicators (I18, I21-I27, I40)
// from snapshot LiveMetrics + Layer1 deps. No Horizon calls — every live
// value (I18, I23-I27, I40) is read from data.LiveMetrics, which
// metrics.EnrichMetrics populates upstream with sticky-fallback to the prior
// day on fetch failures.
type TokenomicsCalculator struct{}

func (c *TokenomicsCalculator) IDs() []int          { return []int{18, 21, 22, 23, 24, 25, 26, 27, 40} }
func (c *TokenomicsCalculator) Dependencies() []int { return []int{1, 5} }

func (c *TokenomicsCalculator) Calculate(_ context.Context, data domain.FundStructureData, deps map[int]Indicator, _ *HistoricalData) ([]Indicator, error) {
	i1 := deps[1].Value // Market Cap
	i5 := deps[5].Value // Total Shares

	// All live-fetched values come straight from the snapshot.
	i18 := liveValue(data.LiveMetrics, func(m *domain.FundLiveMetrics) *string { return m.EURMTLShareholders })
	i23 := liveValue(data.LiveMetrics, func(m *domain.FundLiveMetrics) *string { return m.MTLShareholdersMedian })
	i24 := liveValue(data.LiveMetrics, func(m *domain.FundLiveMetrics) *string { return m.EURMTLParticipants })
	i25 := liveValue(data.LiveMetrics, func(m *domain.FundLiveMetrics) *string { return m.EURMTLDailyVolume })
	i26 := liveValue(data.LiveMetrics, func(m *domain.FundLiveMetrics) *string { return m.EURMTL30dVolume })
	i27 := liveValue(data.LiveMetrics, func(m *domain.FundLiveMetrics) *string { return m.MTLShareholders })
	i40 := liveValue(data.LiveMetrics, func(m *domain.FundLiveMetrics) *string { return m.MTLAPHolders })

	// I21: Average Shareholding = I5 / I27
	i21 := decimal.Zero
	if !i27.IsZero() {
		i21 = i5.Div(i27)
	}

	// I22: Average Value per Shareholder = I1 / I27
	i22 := decimal.Zero
	if !i27.IsZero() {
		i22 = i1.Div(i27)
	}

	return []Indicator{
		NewIndicator(18, i18, "", ""),
		NewIndicator(21, i21, "", ""),
		NewIndicator(22, i22, "", ""),
		NewIndicator(23, i23, "", ""),
		NewIndicator(24, i24, "", ""),
		NewIndicator(25, i25, "", ""),
		NewIndicator(26, i26, "", ""),
		NewIndicator(27, i27, "", ""),
		NewIndicator(40, i40, "", ""),
	}, nil
}
