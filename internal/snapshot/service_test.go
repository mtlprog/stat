package snapshot

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/mtlprog/stat/internal/domain"
)

type mockFundService struct {
	data domain.FundStructureData
	err  error
}

func (m *mockFundService) GetFundStructure(_ context.Context) (domain.FundStructureData, error) {
	return m.data, m.err
}

type mockRepo struct {
	entityID  int
	entityErr error
	saveErr   error
	savedData json.RawMessage
	savedDate time.Time
	latest    *Snapshot
	latestErr error
	byDate    *Snapshot
	byDateErr error
	list      []Snapshot
	listErr   error
}

func (m *mockRepo) Save(_ context.Context, _ int, date time.Time, data json.RawMessage) error {
	m.savedData = data
	m.savedDate = date
	return m.saveErr
}

func (m *mockRepo) GetLatest(_ context.Context, _ string) (*Snapshot, error) {
	if m.latestErr != nil {
		return nil, m.latestErr
	}
	return m.latest, nil
}

func (m *mockRepo) GetByDate(_ context.Context, _ string, _ time.Time) (*Snapshot, error) {
	if m.byDateErr != nil {
		return nil, m.byDateErr
	}
	return m.byDate, nil
}

func (m *mockRepo) GetNearestBefore(_ context.Context, _ string, _ time.Time) (*Snapshot, error) {
	if m.byDateErr != nil {
		return nil, m.byDateErr
	}
	return m.byDate, nil
}

func (m *mockRepo) List(_ context.Context, _ string, _ int) ([]Snapshot, error) {
	return m.list, m.listErr
}

func (m *mockRepo) GetEntityID(_ context.Context, _ string) (int, error) {
	return m.entityID, m.entityErr
}

func (m *mockRepo) EnsureEntity(_ context.Context, _, _, _ string) (int, error) {
	return m.entityID, m.entityErr
}

func TestGenerateSuccess(t *testing.T) {
	fundData := domain.FundStructureData{
		AggregatedTotals: domain.AggregatedTotals{AccountCount: 3},
	}
	repo := &mockRepo{entityID: 1}
	fund := &mockFundService{data: fundData}
	svc := NewService(fund, repo)

	result, err := svc.Generate(context.Background(), "mtlf", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AggregatedTotals.AccountCount != 3 {
		t.Errorf("AccountCount = %d, want 3", result.AggregatedTotals.AccountCount)
	}
	if repo.savedData == nil {
		t.Error("expected data to be saved")
	}
}

func TestGenerateFundServiceError(t *testing.T) {
	repo := &mockRepo{entityID: 1}
	fund := &mockFundService{err: errors.New("fund service error")}
	svc := NewService(fund, repo)

	_, err := svc.Generate(context.Background(), "mtlf", time.Now())
	if err == nil {
		t.Fatal("expected error from fund service")
	}
}

func TestGenerateRepoSaveError(t *testing.T) {
	repo := &mockRepo{entityID: 1, saveErr: errors.New("save failed")}
	fund := &mockFundService{data: domain.FundStructureData{}}
	svc := NewService(fund, repo)

	_, err := svc.Generate(context.Background(), "mtlf", time.Now())
	if err == nil {
		t.Fatal("expected error from repo save")
	}
}

func TestGenerateEntityNotFound(t *testing.T) {
	repo := &mockRepo{entityErr: ErrNotFound}
	fund := &mockFundService{}
	svc := NewService(fund, repo)

	_, err := svc.Generate(context.Background(), "unknown", time.Now())
	if err == nil {
		t.Fatal("expected error for unknown entity")
	}
}
