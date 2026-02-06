package indicator

import (
	"context"

	"github.com/mtlprog/stat/internal/domain"
)

// Service manages indicator calculation.
type Service struct {
	registry *Registry
	hist     *HistoricalData
}

// NewService creates a new IndicatorService with all calculators registered.
func NewService(horizonPrice HorizonPriceSource, tokenomicsHorizon TokenomicsHorizon, hist *HistoricalData) *Service {
	registry := NewRegistry()

	// Layer 0: per-account values
	registry.Register(&Layer0Calculator{})

	// Layer 1: derived values
	registry.Register(&Layer1Calculator{Horizon: horizonPrice})

	// Layer 2: ratios
	registry.Register(&Layer2Calculator{})

	// Layer 3: dividends
	registry.Register(&DividendCalculator{})

	// Layer 4: analytics
	registry.Register(&AnalyticsCalculator{})

	// Layer 5: tokenomics
	registry.Register(&TokenomicsCalculator{Horizon: tokenomicsHorizon})

	return &Service{registry: registry, hist: hist}
}

// CalculateAll computes all indicators from a snapshot.
func (s *Service) CalculateAll(ctx context.Context, data domain.FundStructureData) ([]Indicator, error) {
	return s.registry.CalculateAll(ctx, data, s.hist)
}
