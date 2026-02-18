package snapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/mtlprog/stat/internal/domain"
)

// FundStructureService defines the fund structure generation interface.
type FundStructureService interface {
	GetFundStructure(ctx context.Context) (domain.FundStructureData, error)
}

// MetricsEnricher computes live metrics and injects them into snapshot data at generation time.
// This enables accurate period-over-period comparison by storing values that would otherwise
// require live Horizon queries, which are unavailable for historical snapshots.
type MetricsEnricher interface {
	EnrichMetrics(ctx context.Context, data *domain.FundStructureData) error
}

// Service manages snapshot generation and retrieval.
type Service struct {
	fund     FundStructureService
	repo     Repository
	enricher MetricsEnricher
}

// NewService creates a new SnapshotService. An optional MetricsEnricher can be provided
// to store live metrics (I10, I6/I7, I11) in each snapshot for historical comparison.
func NewService(fund FundStructureService, repo Repository, enrichers ...MetricsEnricher) *Service {
	var enricher MetricsEnricher
	if len(enrichers) > 0 {
		enricher = enrichers[0]
	}
	return &Service{fund: fund, repo: repo, enricher: enricher}
}

// Generate creates a new snapshot for the given entity slug and date.
func (s *Service) Generate(ctx context.Context, slug string, date time.Time) (domain.FundStructureData, error) {
	entityID, err := s.repo.GetEntityID(ctx, slug)
	if err != nil {
		return domain.FundStructureData{}, fmt.Errorf("getting entity: %w", err)
	}

	fundData, err := s.fund.GetFundStructure(ctx)
	if err != nil {
		return domain.FundStructureData{}, fmt.Errorf("generating fund structure: %w", err)
	}

	if s.enricher != nil {
		if err := s.enricher.EnrichMetrics(ctx, &fundData); err != nil {
			slog.Warn("failed to enrich snapshot with live metrics", "error", err)
		}
	}

	data, err := json.Marshal(fundData)
	if err != nil {
		return domain.FundStructureData{}, fmt.Errorf("marshaling fund data: %w", err)
	}

	if err := s.repo.Save(ctx, entityID, date, data); err != nil {
		return domain.FundStructureData{}, fmt.Errorf("saving snapshot: %w", err)
	}

	return fundData, nil
}

// GetLatest retrieves the most recent snapshot for the entity.
func (s *Service) GetLatest(ctx context.Context, slug string) (*Snapshot, error) {
	return s.repo.GetLatest(ctx, slug)
}

// GetByDate retrieves a snapshot for a specific date.
func (s *Service) GetByDate(ctx context.Context, slug string, date time.Time) (*Snapshot, error) {
	return s.repo.GetByDate(ctx, slug, date)
}

// List retrieves recent snapshots.
func (s *Service) List(ctx context.Context, slug string, limit int) ([]Snapshot, error) {
	return s.repo.List(ctx, slug, limit)
}
