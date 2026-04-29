package indicator

import (
	"context"

	"github.com/mtlprog/stat/internal/domain"
)

// Service manages indicator calculation. Calculators read live values from
// snapshot.LiveMetrics — there are no Horizon dependencies at this layer.
type Service struct {
	registry *Registry
	hist     *HistoricalData
}

// NewService creates a new indicator Service with all calculators registered.
// hist is optional; calculators that need historical data (dividend chain) fall
// back to zero when nil.
func NewService(hist *HistoricalData) *Service {
	registry := NewRegistry()
	registry.Register(&Layer0Calculator{})
	registry.Register(&Layer1Calculator{})
	registry.Register(&Layer2Calculator{})
	registry.Register(&DividendCalculator{})
	registry.Register(&AnalyticsCalculator{})
	registry.Register(&TokenomicsCalculator{})
	return &Service{registry: registry, hist: hist}
}

// CalculateAll computes all indicators from a snapshot.
func (s *Service) CalculateAll(ctx context.Context, data domain.FundStructureData) ([]Indicator, error) {
	return s.registry.CalculateAll(ctx, data, s.hist)
}
