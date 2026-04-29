package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/indicator"
	"github.com/mtlprog/stat/internal/snapshot"
)

func subfundSnapshotData(t *testing.T) []byte {
	t.Helper()
	data := domain.FundStructureData{
		Accounts: []domain.FundAccountPortfolio{
			{Name: "MAIN ISSUER", Type: domain.AccountTypeIssuer, TotalEURMTL: decimal.NewFromInt(500)},
			{Name: "MABIZ", Type: domain.AccountTypeSubfond, TotalEURMTL: decimal.NewFromInt(100)},
			{Name: "MCITY", Type: domain.AccountTypeSubfond, TotalEURMTL: decimal.NewFromInt(200)},
			{Name: "DEFI", Type: domain.AccountTypeSubfond, TotalEURMTL: decimal.NewFromInt(300)},
			{Name: "BOSS", Type: domain.AccountTypeSubfond, TotalEURMTL: decimal.NewFromInt(400)},
			{Name: "ADMIN", Type: domain.AccountTypeOperational, TotalEURMTL: decimal.NewFromInt(50)},
		},
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func TestGetBalanceBySubfundLatest(t *testing.T) {
	repo := &mockSnapshotRepo{
		snapshots: []snapshot.Snapshot{
			{ID: 1, EntityID: 1, SnapshotDate: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC), Data: subfundSnapshotData(t)},
		},
	}
	snapSvc := snapshot.NewService(&mockFundService{}, repo)
	handler := NewChartsHandler(snapSvc, &mockIndicatorRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/charts/balance-by-subfund", nil)
	w := httptest.NewRecorder()
	handler.GetBalanceBySubfund(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result BalanceBySubfundResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Date != "2024-01-15" {
		t.Errorf("date = %q, want 2024-01-15", result.Date)
	}
	if len(result.Slices) != 6 {
		t.Fatalf("expected 6 slices (4 subfond + issuer + operational), got %d", len(result.Slices))
	}
	wantNames := map[string]string{
		"MABIZ":       string(domain.AccountTypeSubfond),
		"MCITY":       string(domain.AccountTypeSubfond),
		"DEFI":        string(domain.AccountTypeSubfond),
		"BOSS":        string(domain.AccountTypeSubfond),
		"MAIN ISSUER": string(domain.AccountTypeIssuer),
		"ADMIN":       string(domain.AccountTypeOperational),
	}
	for _, s := range result.Slices {
		wantType, ok := wantNames[s.Name]
		if !ok {
			t.Errorf("unexpected slice name %q", s.Name)
			continue
		}
		if s.Type != wantType {
			t.Errorf("slice %s type = %q, want %q", s.Name, s.Type, wantType)
		}
		if s.Address == "" {
			t.Errorf("slice %s missing address", s.Name)
		}
	}
}

func TestGetBalanceBySubfundByDate(t *testing.T) {
	date := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	repo := &mockSnapshotRepo{
		snapshots: []snapshot.Snapshot{
			{ID: 1, EntityID: 1, SnapshotDate: date, Data: subfundSnapshotData(t)},
		},
	}
	snapSvc := snapshot.NewService(&mockFundService{}, repo)
	handler := NewChartsHandler(snapSvc, &mockIndicatorRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/charts/balance-by-subfund?date=2024-03-01", nil)
	w := httptest.NewRecorder()
	handler.GetBalanceBySubfund(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestGetBalanceBySubfundInvalidDate(t *testing.T) {
	handler := NewChartsHandler(snapshot.NewService(&mockFundService{}, &mockSnapshotRepo{}), &mockIndicatorRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/charts/balance-by-subfund?date=garbage", nil)
	w := httptest.NewRecorder()
	handler.GetBalanceBySubfund(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestGetBalanceBySubfundNotFound(t *testing.T) {
	handler := NewChartsHandler(snapshot.NewService(&mockFundService{}, &mockSnapshotRepo{}), &mockIndicatorRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/charts/balance-by-subfund", nil)
	w := httptest.NewRecorder()
	handler.GetBalanceBySubfund(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestGetIndicatorHistorySuccess(t *testing.T) {
	d1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	repo := &mockIndicatorRepo{
		historyPoints: []indicator.HistoryPoint{
			{SnapshotDate: d1, IndicatorID: 1, Value: decimal.NewFromInt(100)},
			{SnapshotDate: d2, IndicatorID: 1, Value: decimal.NewFromInt(110)},
			{SnapshotDate: d1, IndicatorID: 3, Value: decimal.NewFromInt(50)},
		},
	}
	handler := NewChartsHandler(snapshot.NewService(&mockFundService{}, &mockSnapshotRepo{}), repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/charts/indicator-history?ids=1,3&range=30d", nil)
	w := httptest.NewRecorder()
	handler.GetIndicatorHistory(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result IndicatorHistoryResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Series) != 2 {
		t.Fatalf("expected 2 series, got %d", len(result.Series))
	}
	if result.Series[0].ID != 1 || len(result.Series[0].Points) != 2 {
		t.Errorf("series[0] = %+v, want id=1 with 2 points", result.Series[0])
	}
	if result.Series[1].ID != 3 || len(result.Series[1].Points) != 1 {
		t.Errorf("series[1] = %+v, want id=3 with 1 point", result.Series[1])
	}
}

func TestGetIndicatorHistoryMissingIDs(t *testing.T) {
	handler := NewChartsHandler(snapshot.NewService(&mockFundService{}, &mockSnapshotRepo{}), &mockIndicatorRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/charts/indicator-history", nil)
	w := httptest.NewRecorder()
	handler.GetIndicatorHistory(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestGetIndicatorHistoryInvalidIDs(t *testing.T) {
	handler := NewChartsHandler(snapshot.NewService(&mockFundService{}, &mockSnapshotRepo{}), &mockIndicatorRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/charts/indicator-history?ids=abc", nil)
	w := httptest.NewRecorder()
	handler.GetIndicatorHistory(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestGetIndicatorHistoryInvalidRange(t *testing.T) {
	handler := NewChartsHandler(snapshot.NewService(&mockFundService{}, &mockSnapshotRepo{}), &mockIndicatorRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/charts/indicator-history?ids=1&range=99d", nil)
	w := httptest.NewRecorder()
	handler.GetIndicatorHistory(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestGetIndicatorHistoryEmptyPoints(t *testing.T) {
	handler := NewChartsHandler(snapshot.NewService(&mockFundService{}, &mockSnapshotRepo{}), &mockIndicatorRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/charts/indicator-history?ids=1&range=30d", nil)
	w := httptest.NewRecorder()
	handler.GetIndicatorHistory(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result IndicatorHistoryResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(result.Series))
	}
	if len(result.Series[0].Points) != 0 {
		t.Errorf("expected empty Points for series with no data, got %v", result.Series[0].Points)
	}
}

func TestParseHistoryRangeAll(t *testing.T) {
	from, err := parseHistoryRange("all")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !from.Equal(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("range=all from = %v, want 1970-01-01", from)
	}
}

func TestParseHistoryRangeDefault(t *testing.T) {
	from, err := parseHistoryRange("")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	now := time.Now().UTC().Truncate(24 * time.Hour)
	expected := now.AddDate(0, 0, -90)
	if !from.Equal(expected) {
		t.Errorf("default from = %v, want %v", from, expected)
	}
}
