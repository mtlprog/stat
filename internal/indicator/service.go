package indicator

import (
	"context"
	"log/slog"

	"github.com/mtlprog/stat/internal/domain"
)

// Service manages indicator calculation.
type Service struct {
	registry *Registry
	hist     *HistoricalData
}

// NewService creates a new indicator Service with all calculators registered.
// Nil dependencies are allowed but will limit which indicators can be computed.
func NewService(horizonPrice HorizonPriceSource, tokenomicsHorizon TokenomicsHorizon, hist *HistoricalData) *Service {
	if horizonPrice == nil {
		slog.Warn("indicator service: horizonPrice is nil, Layer 1 market prices will be zero")
	}
	if tokenomicsHorizon == nil {
		slog.Warn("indicator service: tokenomicsHorizon is nil, tokenomics indicators will be zero")
	}
	registry := NewRegistry()

	// Layer 0: per-account values
	registry.Register(&Layer0Calculator{})

	// Layer 1: derived values
	registry.Register(&Layer1Calculator{Horizon: horizonPrice})

	// Layer 2: ratios
	registry.Register(&Layer2Calculator{})

	// Dividend indicators
	registry.Register(&DividendCalculator{})

	// Analytics indicators
	registry.Register(&AnalyticsCalculator{})

	// Tokenomics indicators
	registry.Register(&TokenomicsCalculator{Horizon: tokenomicsHorizon})

	return &Service{registry: registry, hist: hist}
}

// CalculateAll computes all indicators from a snapshot.
func (s *Service) CalculateAll(ctx context.Context, data domain.FundStructureData) ([]Indicator, error) {
	return s.registry.CalculateAll(ctx, data, s.hist)
}
