package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/indicator"
)

type mockIndicatorRepo struct {
	latest          []indicator.Indicator
	latestDate      time.Time
	latestErr       error
	byDate          map[time.Time][]indicator.Indicator
	historyPoints   []indicator.HistoryPoint
	historyErr      error
	nearestByCutoff map[time.Time]map[int]indicator.Indicator
}

func (m *mockIndicatorRepo) Save(_ context.Context, _ int, _ time.Time, _ []indicator.Indicator) error {
	return nil
}

func (m *mockIndicatorRepo) GetByDate(_ context.Context, _ string, date time.Time) ([]indicator.Indicator, error) {
	if inds, ok := m.byDate[date]; ok {
		return inds, nil
	}
	return nil, indicator.ErrNotFound
}

func (m *mockIndicatorRepo) GetLatest(_ context.Context, _ string) ([]indicator.Indicator, time.Time, error) {
	if m.latestErr != nil {
		return nil, time.Time{}, m.latestErr
	}
	if len(m.latest) == 0 {
		return nil, time.Time{}, indicator.ErrNotFound
	}
	return m.latest, m.latestDate, nil
}

func (m *mockIndicatorRepo) GetHistory(_ context.Context, _ string, _ []int, _ time.Time) ([]indicator.HistoryPoint, error) {
	return m.historyPoints, m.historyErr
}

func (m *mockIndicatorRepo) GetNearestBefore(_ context.Context, _ string, date time.Time) (map[int]indicator.Indicator, error) {
	// Return the entry whose key is the latest one ≤ date.
	var bestKey time.Time
	var best map[int]indicator.Indicator
	var found bool
	for cutoff, inds := range m.nearestByCutoff {
		if !cutoff.After(date) && (!found || cutoff.After(bestKey)) {
			bestKey = cutoff
			best = inds
			found = true
		}
	}
	return best, nil
}

func sampleIndicator(id int, value string) indicator.Indicator {
	return indicator.NewIndicator(id, decimal.RequireFromString(value), "", "")
}

func TestGetIndicatorsSuccess(t *testing.T) {
	repo := &mockIndicatorRepo{
		latest:     []indicator.Indicator{sampleIndicator(1, "100"), sampleIndicator(3, "200")},
		latestDate: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
	}
	handler := NewIndicatorHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/indicators", nil)
	w := httptest.NewRecorder()
	handler.GetIndicators(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result []IndicatorWithChanges
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("got %d indicators, want 2", len(result))
	}
	if result[0].Changes != nil {
		t.Errorf("expected nil Changes when no compare requested, got %v", result[0].Changes)
	}
}

func TestGetIndicatorsNoSnapshot(t *testing.T) {
	repo := &mockIndicatorRepo{}
	handler := NewIndicatorHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/indicators", nil)
	w := httptest.NewRecorder()
	handler.GetIndicators(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestGetIndicatorsRepoError(t *testing.T) {
	repo := &mockIndicatorRepo{latestErr: errors.New("db down")}
	handler := NewIndicatorHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/indicators", nil)
	w := httptest.NewRecorder()
	handler.GetIndicators(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestGetIndicatorsByDateSuccess(t *testing.T) {
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	repo := &mockIndicatorRepo{
		byDate: map[time.Time][]indicator.Indicator{
			date: {sampleIndicator(1, "100")},
		},
	}
	handler := NewIndicatorHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/indicators/2024-01-15", nil)
	req.SetPathValue("date", "2024-01-15")
	w := httptest.NewRecorder()
	handler.GetIndicatorsByDate(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestGetIndicatorsByDateInvalid(t *testing.T) {
	handler := NewIndicatorHandler(&mockIndicatorRepo{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/indicators/not-a-date", nil)
	req.SetPathValue("date", "not-a-date")
	w := httptest.NewRecorder()
	handler.GetIndicatorsByDate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestGetIndicatorsCompareInvalidPeriod(t *testing.T) {
	repo := &mockIndicatorRepo{
		latest:     []indicator.Indicator{sampleIndicator(1, "100")},
		latestDate: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
	}
	handler := NewIndicatorHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/indicators?compare=7d", nil)
	w := httptest.NewRecorder()
	handler.GetIndicators(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestGetIndicatorsCompareNoHistory(t *testing.T) {
	repo := &mockIndicatorRepo{
		latest:     []indicator.Indicator{sampleIndicator(1, "100")},
		latestDate: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
	}
	handler := NewIndicatorHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/indicators?compare=30d", nil)
	w := httptest.NewRecorder()
	handler.GetIndicators(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result []IndicatorWithChanges
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, ind := range result {
		if ind.Changes != nil {
			t.Errorf("expected nil Changes when no historical data, got %v", ind.Changes)
		}
	}
}

func TestGetIndicatorsCompareSinglePeriod(t *testing.T) {
	now := time.Date(2024, 4, 15, 0, 0, 0, 0, time.UTC)
	historicalDate := now.AddDate(0, 0, -30)
	repo := &mockIndicatorRepo{
		latest:     []indicator.Indicator{sampleIndicator(1, "120")},
		latestDate: now,
		nearestByCutoff: map[time.Time]map[int]indicator.Indicator{
			historicalDate: {1: sampleIndicator(1, "100")},
		},
	}
	handler := NewIndicatorHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/indicators?compare=30d", nil)
	w := httptest.NewRecorder()
	handler.GetIndicators(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result []IndicatorWithChanges
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d indicators, want 1", len(result))
	}
	change, ok := result[0].Changes["30d"]
	if !ok {
		t.Fatalf("expected Changes[\"30d\"] to exist, got %v", result[0].Changes)
	}
	if !change.Abs.Equal(decimal.NewFromInt(20)) {
		t.Errorf("Abs = %s, want 20", change.Abs)
	}
	if !change.Pct.Equal(decimal.NewFromInt(20)) {
		t.Errorf("Pct = %s, want 20", change.Pct)
	}
}

func TestGetIndicatorsCompareAllPopulatesAvailable(t *testing.T) {
	now := time.Date(2024, 4, 15, 0, 0, 0, 0, time.UTC)
	repo := &mockIndicatorRepo{
		latest:     []indicator.Indicator{sampleIndicator(1, "200")},
		latestDate: now,
		nearestByCutoff: map[time.Time]map[int]indicator.Indicator{
			now.AddDate(0, 0, -30): {1: sampleIndicator(1, "100")},
			now.AddDate(0, 0, -90): {1: sampleIndicator(1, "50")},
			// 180d / 365d: not in map, no historical data
		},
	}
	handler := NewIndicatorHandler(repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/indicators?compare=all", nil)
	w := httptest.NewRecorder()
	handler.GetIndicators(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result []IndicatorWithChanges
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d indicators, want 1", len(result))
	}
	if _, ok := result[0].Changes["30d"]; !ok {
		t.Error("expected Changes[\"30d\"] to be present")
	}
	if _, ok := result[0].Changes["90d"]; !ok {
		t.Error("expected Changes[\"90d\"] to be present")
	}
	if _, ok := result[0].Changes["180d"]; ok {
		t.Error("expected Changes[\"180d\"] to be omitted (no historical row)")
	}
	if _, ok := result[0].Changes["365d"]; ok {
		t.Error("expected Changes[\"365d\"] to be omitted (no historical row)")
	}
}

func TestParsePeriodList(t *testing.T) {
	cases := []struct {
		in   string
		want []int
		err  bool
	}{
		{"", nil, false},
		{"30d", []int{30}, false},
		{"30d,90d", []int{30, 90}, false},
		{"all", []int{30, 90, 180, 365}, false},
		{"30d,30d", []int{30}, false},
		{"7d", nil, true},
		{"foo", nil, true},
	}
	for _, c := range cases {
		got, err := parsePeriodList(c.in)
		if (err != nil) != c.err {
			t.Errorf("parsePeriodList(%q) err = %v, wantErr %v", c.in, err, c.err)
			continue
		}
		if !c.err && !equalInts(got, c.want) {
			t.Errorf("parsePeriodList(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
