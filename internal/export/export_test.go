package export

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/indicator"
)

type stubHistory struct {
	calls   int
	values  map[int]indicator.Indicator // returned for any "recent" lookup
	yearAgo map[int]indicator.Indicator
	err     error
}

func (s *stubHistory) GetNearestBefore(_ context.Context, _ string, date time.Time) (map[int]indicator.Indicator, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	// Differentiate the 365d lookup from shorter periods so a test can simulate
	// "no historical data for that date".
	if date.Before(time.Now().UTC().AddDate(0, -6, 0)) {
		return s.yearAgo, nil
	}
	return s.values, nil
}

type captureWriter struct {
	rows []IndicatorRow
}

func (w *captureWriter) Write(_ context.Context, rows []IndicatorRow) error {
	w.rows = rows
	return nil
}

func TestExportComputesPercentChangesFromRepo(t *testing.T) {
	hist := &stubHistory{
		values: map[int]indicator.Indicator{
			1: {ID: 1, Value: decimal.NewFromInt(100)},
		},
		yearAgo: nil, // no 365d data
	}
	w := &captureWriter{}
	svc := NewService(hist, w)

	current := []indicator.Indicator{
		{ID: 1, Value: decimal.NewFromInt(120)},
	}

	rows, err := svc.Export(context.Background(), current)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}

	row := rows[0]
	if row.WeekChange == nil || !row.WeekChange.Equal(decimal.RequireFromString("0.2")) {
		t.Errorf("WeekChange = %v, want 0.2", row.WeekChange)
	}
	if row.YearChange != nil {
		t.Errorf("YearChange = %v, want nil (no 365d data)", row.YearChange)
	}
	// fetchHistorical hits 4 periods (7/30/90/365) — no extra recomputations.
	if hist.calls != 4 {
		t.Errorf("history.calls = %d, want 4 (one per period)", hist.calls)
	}
}

func TestExportFallsBackToMonitoringHistory(t *testing.T) {
	hist := &stubHistory{} // no values from DB
	w := &captureWriter{}
	svc := NewService(hist, w)

	yearAgoDate := time.Now().UTC().AddDate(0, 0, -365)
	monHist := MonitoringHistory{
		yearAgoDate: {1: decimal.NewFromInt(50)},
	}

	current := []indicator.Indicator{
		{ID: 1, Value: decimal.NewFromInt(100)},
	}

	rows, err := svc.ExportWithHistory(context.Background(), current, monHist)
	if err != nil {
		t.Fatalf("ExportWithHistory failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].YearChange == nil || !rows[0].YearChange.Equal(decimal.NewFromInt(1)) {
		t.Errorf("YearChange = %v, want 1.0 (monitoring fallback)", rows[0].YearChange)
	}
}

func TestExportSurvivesRepoErrors(t *testing.T) {
	hist := &stubHistory{err: errors.New("connection reset")}
	w := &captureWriter{}
	svc := NewService(hist, w)

	current := []indicator.Indicator{{ID: 1, Value: decimal.NewFromInt(50)}}
	rows, err := svc.Export(context.Background(), current)
	if err != nil {
		t.Fatalf("Export should not fail on repo error: %v", err)
	}
	if rows[0].WeekChange != nil {
		t.Errorf("WeekChange should be nil when repo errors, got %v", rows[0].WeekChange)
	}
}

func TestComputeChangeZeroHistorical(t *testing.T) {
	byID := map[int]indicator.Indicator{
		1: {ID: 1, Value: decimal.Zero},
	}
	if got := computeChange(1, decimal.NewFromInt(10), byID); got != nil {
		t.Errorf("computeChange = %v, want nil (zero historical)", got)
	}
}
