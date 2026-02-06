package snapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mtlprog/stat/internal/domain"
)

// FundStructureService defines the fund structure generation interface.
type FundStructureService interface {
	GetFundStructure(ctx context.Context) (domain.FundStructureData, error)
}

// Service manages snapshot generation and retrieval.
type Service struct {
	fund FundStructureService
	repo Repository
}

// NewService creates a new SnapshotService.
func NewService(fund FundStructureService, repo Repository) *Service {
	return &Service{fund: fund, repo: repo}
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
