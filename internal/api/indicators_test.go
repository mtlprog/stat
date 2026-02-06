package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mtlprog/stat/internal/indicator"
	"github.com/mtlprog/stat/internal/snapshot"
)

func TestGetIndicatorsSuccess(t *testing.T) {
	data, _ := json.Marshal(map[string]any{
		"accounts":         []any{},
		"mutualFunds":      []any{},
		"otherAccounts":    []any{},
		"aggregatedTotals": map[string]any{"totalEURMTL": "0", "totalXLM": "0", "accountCount": 0, "tokenCount": 0},
	})
	repo := &mockSnapshotRepo{
		snapshots: []snapshot.Snapshot{
			{ID: 1, EntityID: 1, SnapshotDate: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC), Data: data},
		},
	}
	snapshotSvc := snapshot.NewService(&mockFundService{}, repo)
	indicatorSvc := indicator.NewService(nil, nil, nil)
	handler := NewIndicatorHandler(snapshotSvc, indicatorSvc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/indicators", nil)
	w := httptest.NewRecorder()
	handler.GetIndicators(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var indicators []indicator.Indicator
	if err := json.NewDecoder(w.Body).Decode(&indicators); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(indicators) == 0 {
		t.Error("expected non-empty indicators list")
	}
}

func TestGetIndicatorsNoSnapshot(t *testing.T) {
	repo := &mockSnapshotRepo{}
	snapshotSvc := snapshot.NewService(&mockFundService{}, repo)
	indicatorSvc := indicator.NewService(nil, nil, nil)
	handler := NewIndicatorHandler(snapshotSvc, indicatorSvc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/indicators", nil)
	w := httptest.NewRecorder()
	handler.GetIndicators(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestGetIndicatorsByDateSuccess(t *testing.T) {
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	data, _ := json.Marshal(map[string]any{
		"accounts":         []any{},
		"mutualFunds":      []any{},
		"otherAccounts":    []any{},
		"aggregatedTotals": map[string]any{"totalEURMTL": "0", "totalXLM": "0", "accountCount": 0, "tokenCount": 0},
	})
	repo := &mockSnapshotRepo{
		snapshots: []snapshot.Snapshot{
			{ID: 1, EntityID: 1, SnapshotDate: date, Data: data},
		},
	}
	snapshotSvc := snapshot.NewService(&mockFundService{}, repo)
	indicatorSvc := indicator.NewService(nil, nil, nil)
	handler := NewIndicatorHandler(snapshotSvc, indicatorSvc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/indicators/2024-01-15", nil)
	req.SetPathValue("date", "2024-01-15")
	w := httptest.NewRecorder()
	handler.GetIndicatorsByDate(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestGetIndicatorsByDateInvalid(t *testing.T) {
	repo := &mockSnapshotRepo{}
	snapshotSvc := snapshot.NewService(&mockFundService{}, repo)
	indicatorSvc := indicator.NewService(nil, nil, nil)
	handler := NewIndicatorHandler(snapshotSvc, indicatorSvc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/indicators/not-a-date", nil)
	req.SetPathValue("date", "not-a-date")
	w := httptest.NewRecorder()
	handler.GetIndicatorsByDate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
