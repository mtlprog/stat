package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/snapshot"
)

type mockSnapshotRepo struct {
	snapshots     []snapshot.Snapshot
	entityID      int
	lastListLimit int
}

func (m *mockSnapshotRepo) Save(_ context.Context, _ int, _ time.Time, _ json.RawMessage) error {
	return nil
}

func (m *mockSnapshotRepo) GetLatest(_ context.Context, _ string) (*snapshot.Snapshot, error) {
	if len(m.snapshots) == 0 {
		return nil, snapshot.ErrNotFound
	}
	return &m.snapshots[0], nil
}

func (m *mockSnapshotRepo) GetByDate(_ context.Context, _ string, date time.Time) (*snapshot.Snapshot, error) {
	for _, s := range m.snapshots {
		if s.SnapshotDate.Equal(date) {
			return &s, nil
		}
	}
	return nil, snapshot.ErrNotFound
}

func (m *mockSnapshotRepo) List(_ context.Context, _ string, limit int) ([]snapshot.Snapshot, error) {
	m.lastListLimit = limit
	if limit > len(m.snapshots) {
		limit = len(m.snapshots)
	}
	return m.snapshots[:limit], nil
}

func (m *mockSnapshotRepo) GetEntityID(_ context.Context, _ string) (int, error) {
	return m.entityID, nil
}

func (m *mockSnapshotRepo) EnsureEntity(_ context.Context, _, _, _ string) (int, error) {
	return m.entityID, nil
}

type mockFundService struct{}

func (m *mockFundService) GetFundStructure(_ context.Context) (domain.FundStructureData, error) {
	return domain.FundStructureData{}, nil
}

func TestGetLatestSnapshotSuccess(t *testing.T) {
	data, _ := json.Marshal(map[string]string{"test": "data"})
	repo := &mockSnapshotRepo{
		snapshots: []snapshot.Snapshot{
			{ID: 1, EntityID: 1, SnapshotDate: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC), Data: data},
		},
	}
	svc := snapshot.NewService(&mockFundService{}, repo)
	handler := NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/snapshots/latest", nil)
	w := httptest.NewRecorder()
	handler.GetLatestSnapshot(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var result snapshot.Snapshot
	json.NewDecoder(w.Body).Decode(&result)
	if result.ID != 1 {
		t.Errorf("snapshot ID = %d, want 1", result.ID)
	}
}

func TestGetLatestSnapshotNotFound(t *testing.T) {
	repo := &mockSnapshotRepo{}
	svc := snapshot.NewService(&mockFundService{}, repo)
	handler := NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/snapshots/latest", nil)
	w := httptest.NewRecorder()
	handler.GetLatestSnapshot(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestGetSnapshotByDateSuccess(t *testing.T) {
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	data, _ := json.Marshal(map[string]string{"test": "data"})
	repo := &mockSnapshotRepo{
		snapshots: []snapshot.Snapshot{
			{ID: 1, EntityID: 1, SnapshotDate: date, Data: data},
		},
	}
	svc := snapshot.NewService(&mockFundService{}, repo)
	handler := NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/snapshots/2024-01-15", nil)
	req.SetPathValue("date", "2024-01-15")
	w := httptest.NewRecorder()
	handler.GetSnapshotByDate(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestGetSnapshotByDateInvalid(t *testing.T) {
	repo := &mockSnapshotRepo{}
	svc := snapshot.NewService(&mockFundService{}, repo)
	handler := NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/snapshots/not-a-date", nil)
	req.SetPathValue("date", "not-a-date")
	w := httptest.NewRecorder()
	handler.GetSnapshotByDate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestListSnapshotsLimitCappedAt365(t *testing.T) {
	data, _ := json.Marshal(map[string]string{})
	repo := &mockSnapshotRepo{
		snapshots: []snapshot.Snapshot{
			{ID: 1, Data: data},
		},
	}
	svc := snapshot.NewService(&mockFundService{}, repo)
	handler := NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/snapshots?limit=9999", nil)
	w := httptest.NewRecorder()
	handler.ListSnapshots(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if repo.lastListLimit != 365 {
		t.Errorf("limit passed to repo = %d, want 365 (should be capped)", repo.lastListLimit)
	}
}

func TestListSnapshotsNegativeLimit(t *testing.T) {
	data, _ := json.Marshal(map[string]string{})
	repo := &mockSnapshotRepo{
		snapshots: []snapshot.Snapshot{
			{ID: 1, Data: data},
			{ID: 2, Data: data},
		},
	}
	svc := snapshot.NewService(&mockFundService{}, repo)
	handler := NewHandler(svc)

	// Negative limit should fall back to default 30
	req := httptest.NewRequest(http.MethodGet, "/api/v1/snapshots?limit=-5", nil)
	w := httptest.NewRecorder()
	handler.ListSnapshots(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var result []snapshot.Snapshot
	json.NewDecoder(w.Body).Decode(&result)
	if len(result) != 2 {
		t.Errorf("snapshot count = %d, want 2 (default limit should apply)", len(result))
	}
}

func TestListSnapshots(t *testing.T) {
	data, _ := json.Marshal(map[string]string{})
	repo := &mockSnapshotRepo{
		snapshots: []snapshot.Snapshot{
			{ID: 1, Data: data},
			{ID: 2, Data: data},
		},
	}
	svc := snapshot.NewService(&mockFundService{}, repo)
	handler := NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/snapshots?limit=10", nil)
	w := httptest.NewRecorder()
	handler.ListSnapshots(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var result []snapshot.Snapshot
	json.NewDecoder(w.Body).Decode(&result)
	if len(result) != 2 {
		t.Errorf("snapshot count = %d, want 2", len(result))
	}
}
