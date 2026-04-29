package export

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/indicator"
)

// mainIndicatorIDs is the set of indicator IDs that appear in the IND_MAIN sheet.
// Based on the MTL_report_1.xlsx example file.
var mainIndicatorIDs = map[int]bool{
	1: true, 2: true, 3: true, 4: true, 5: true, 6: true, 7: true,
	8: true, 10: true, 11: true, 15: true, 16: true, 17: true, 18: true,
	22: true, 24: true, 27: true, 30: true, 40: true,
}

// IndicatorRow holds a computed indicator with historical period changes.
type IndicatorRow struct {
	indicator.Indicator
	WeekChange    *decimal.Decimal
	MonthChange   *decimal.Decimal
	QuarterChange *decimal.Decimal
	YearChange    *decimal.Decimal
	IsMain        bool
}

// SheetWriter writes indicator rows to a spreadsheet destination.
type SheetWriter interface {
	Write(ctx context.Context, rows []IndicatorRow) error
}

// IndicatorHistory exposes the slice of repository methods the export service
// needs for historical comparisons. It is a narrowed view of indicator.Repository
// to keep the export package decoupled from the persistence layer.
type IndicatorHistory interface {
	GetNearestBefore(ctx context.Context, slug string, date time.Time) (map[int]indicator.Indicator, error)
}

// Service writes computed indicators to a spreadsheet destination, joining each
// row with historical period-over-period change data read directly from the
// fund_indicators table — never recomputed from snapshots.
type Service struct {
	history IndicatorHistory
	writer  SheetWriter
	slug    string
}

// NewService creates a new export Service.
func NewService(history IndicatorHistory, writer SheetWriter) *Service {
	return &Service{history: history, writer: writer, slug: "mtlf"}
}

// Export writes IND_ALL/IND_MAIN with historical comparisons read from the
// indicator repository.
func (s *Service) Export(ctx context.Context, current []indicator.Indicator) ([]IndicatorRow, error) {
	return s.exportRows(ctx, current, nil)
}

// ExportWithHistory works like Export but fills gaps in historical data from monHist
// when DB indicators are unavailable. Use this for import-excel where the DB has few
// indicator rows but the Excel MONITORING sheet has full history.
func (s *Service) ExportWithHistory(ctx context.Context, current []indicator.Indicator, monHist MonitoringHistory) ([]IndicatorRow, error) {
	return s.exportRows(ctx, current, monHist)
}

// MonitoringHistory maps dates to indicator values extracted from MONITORING sheet rows.
// Keys are dates (midnight UTC), values map indicator ID → value.
type MonitoringHistory map[time.Time]map[int]decimal.Decimal

// NearestBefore returns indicator values for the latest date in the history that is ≤ target.
// Returns nil if no qualifying date exists.
func (mh MonitoringHistory) NearestBefore(target time.Time) map[int]indicator.Indicator {
	var best time.Time
	var found bool
	for d := range mh {
		if !d.After(target) && (d.After(best) || !found) {
			best = d
			found = true
		}
	}
	if !found {
		return nil
	}
	vals := mh[best]
	result := make(map[int]indicator.Indicator, len(vals))
	for id, v := range vals {
		result[id] = indicator.Indicator{ID: id, Value: v}
	}
	return result
}

// fetchHistorical retrieves persisted indicator sets at-or-before each
// (today − days) target. Reads from fund_indicators only; no recomputation,
// no Horizon traffic.
func (s *Service) fetchHistorical(ctx context.Context, periods []int) map[int]map[int]indicator.Indicator {
	result := make(map[int]map[int]indicator.Indicator, len(periods))
	now := time.Now().UTC()

	for _, days := range periods {
		pastDate := now.AddDate(0, 0, -days)
		hist, err := s.history.GetNearestBefore(ctx, s.slug, pastDate)
		if err != nil {
			if errors.Is(err, indicator.ErrNotFound) {
				continue
			}
			slog.Error("export: load historical indicators failed", "days", days, "error", err)
			continue
		}
		if len(hist) == 0 {
			continue
		}
		result[days] = hist
	}

	return result
}

func (s *Service) exportRows(ctx context.Context, current []indicator.Indicator, monHist MonitoringHistory) ([]IndicatorRow, error) {
	historicalByPeriod := s.fetchHistorical(ctx, []int{7, 30, 90, 365})

	// Fill gaps from monitoring history.
	now := time.Now().UTC()
	for _, days := range []int{7, 30, 90, 365} {
		if historicalByPeriod[days] != nil {
			continue
		}
		pastDate := now.AddDate(0, 0, -days)
		if fallback := monHist.NearestBefore(pastDate); fallback != nil {
			historicalByPeriod[days] = fallback
		}
	}

	rows := make([]IndicatorRow, 0, len(current))
	for _, ind := range current {
		row := IndicatorRow{
			Indicator: ind,
			IsMain:    mainIndicatorIDs[ind.ID],
		}

		row.WeekChange = computeChange(ind.ID, ind.Value, historicalByPeriod[7])
		row.MonthChange = computeChange(ind.ID, ind.Value, historicalByPeriod[30])
		row.QuarterChange = computeChange(ind.ID, ind.Value, historicalByPeriod[90])
		row.YearChange = computeChange(ind.ID, ind.Value, historicalByPeriod[365])

		rows = append(rows, row)
	}

	if err := s.writer.Write(ctx, rows); err != nil {
		return nil, fmt.Errorf("writing indicator rows: %w", err)
	}
	return rows, nil
}

// computeChange returns (current - historical) / historical, or nil if unavailable.
func computeChange(id int, current decimal.Decimal, byID map[int]indicator.Indicator) *decimal.Decimal {
	if byID == nil {
		return nil
	}
	hist, ok := byID[id]
	if !ok || hist.Value.IsZero() {
		return nil
	}
	pct := current.Sub(hist.Value).Div(hist.Value)
	return &pct
}
