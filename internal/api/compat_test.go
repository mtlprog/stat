package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/snapshot"
	"github.com/shopspring/decimal"
)

func testFundData() domain.FundStructureData {
	return domain.FundStructureData{
		Accounts: []domain.FundAccountPortfolio{
			{ID: "GISSUER", Name: "ISSUER", Type: domain.AccountTypeIssuer, TotalEURMTL: decimal.NewFromInt(100)},
		},
		MutualFunds: []domain.FundAccountPortfolio{
			{ID: "GMUTUAL", Name: "MFB", Type: domain.AccountTypeMutual, TotalEURMTL: decimal.NewFromInt(50)},
		},
		OtherAccounts: []domain.FundAccountPortfolio{
			{ID: "GOTHER", Name: "LABR", Type: domain.AccountTypeOther, TotalEURMTL: decimal.NewFromInt(10)},
		},
		AggregatedTotals: domain.AggregatedTotals{
			TotalEURMTL:  decimal.NewFromInt(100),
			AccountCount: 1,
			TokenCount:   5,
		},
		Warnings:    []string{"test warning"},
		LiveMetrics: &domain.FundLiveMetrics{},
	}
}

func TestListSnapshotsCompat(t *testing.T) {
	date1 := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	date2 := time.Date(2024, 1, 14, 0, 0, 0, 0, time.UTC)
	data, _ := json.Marshal(testFundData())

	repo := &mockSnapshotRepo{
		snapshots: []snapshot.Snapshot{
			{ID: 1, EntityID: 1, SnapshotDate: date1, CreatedAt: date1.Add(5 * time.Minute), Data: data},
			{ID: 2, EntityID: 1, SnapshotDate: date2, CreatedAt: date2.Add(3 * time.Minute), Data: data},
		},
	}
	svc := snapshot.NewService(&mockFundService{}, repo)
	handler := NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/snapshots", nil)
	w := httptest.NewRecorder()
	handler.ListSnapshotsCompat(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result []struct {
		Date      time.Time `json:"date"`
		CreatedAt time.Time `json:"createdAt"`
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("got %d entries, want 2", len(result))
	}
	if !result[0].Date.Equal(date1) {
		t.Errorf("first date = %v, want %v", result[0].Date, date1)
	}

	// Verify no extra fields (id, entityId, data) leak through.
	var raw []map[string]any
	w2 := httptest.NewRecorder()
	handler.ListSnapshotsCompat(w2, httptest.NewRequest(http.MethodGet, "/api/snapshots", nil))
	json.NewDecoder(w2.Body).Decode(&raw)
	for _, entry := range raw {
		for key := range entry {
			if key != "date" && key != "createdAt" {
				t.Errorf("unexpected field %q in compat snapshot entry", key)
			}
		}
	}
}

func TestGetFundStructureCompatLatest(t *testing.T) {
	fundData := testFundData()
	data, _ := json.Marshal(fundData)
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	repo := &mockSnapshotRepo{
		snapshots: []snapshot.Snapshot{
			{ID: 1, EntityID: 1, SnapshotDate: date, Data: data},
		},
	}
	svc := snapshot.NewService(&mockFundService{}, repo)
	handler := NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/fund-structure", nil)
	w := httptest.NewRecorder()
	handler.GetFundStructureCompat(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify accounts and mutualFunds are merged.
	accounts, ok := result["accounts"].([]any)
	if !ok {
		t.Fatal("missing accounts field")
	}
	if len(accounts) != 2 {
		t.Errorf("got %d accounts, want 2 (issuer + mutual merged)", len(accounts))
	}

	// Verify mutualFunds, warnings, live_metrics are absent.
	for _, key := range []string{"mutualFunds", "warnings", "live_metrics"} {
		if _, exists := result[key]; exists {
			t.Errorf("field %q should not exist in compat response", key)
		}
	}

	// Verify otherAccounts present.
	others, ok := result["otherAccounts"].([]any)
	if !ok || len(others) != 1 {
		t.Error("expected 1 otherAccount")
	}

	// Verify aggregatedTotals present.
	if _, ok := result["aggregatedTotals"]; !ok {
		t.Error("missing aggregatedTotals")
	}
}

func TestGetFundStructureCompatByDate(t *testing.T) {
	fundData := testFundData()
	data, _ := json.Marshal(fundData)
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	repo := &mockSnapshotRepo{
		snapshots: []snapshot.Snapshot{
			{ID: 1, EntityID: 1, SnapshotDate: date, Data: data},
		},
	}
	svc := snapshot.NewService(&mockFundService{}, repo)
	handler := NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/fund-structure?date=2024-01-15", nil)
	w := httptest.NewRecorder()
	handler.GetFundStructureCompat(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result compatFundStructure
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(result.Accounts) != 2 {
		t.Errorf("got %d accounts, want 2", len(result.Accounts))
	}
}

func TestGetFundStructureCompatNotFound(t *testing.T) {
	repo := &mockSnapshotRepo{}
	svc := snapshot.NewService(&mockFundService{}, repo)
	handler := NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/fund-structure", nil)
	w := httptest.NewRecorder()
	handler.GetFundStructureCompat(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestGetFundStructureCompatInvalidDate(t *testing.T) {
	repo := &mockSnapshotRepo{}
	svc := snapshot.NewService(&mockFundService{}, repo)
	handler := NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/fund-structure?date=not-a-date", nil)
	w := httptest.NewRecorder()
	handler.GetFundStructureCompat(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
