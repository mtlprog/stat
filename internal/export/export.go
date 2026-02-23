package export

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/indicator"
	"github.com/mtlprog/stat/internal/snapshot"
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

// Service orchestrates indicator calculation and delegates writing to a SheetWriter.
type Service struct {
	indicators *indicator.Service
	snapshots  snapshot.Repository
	writer     SheetWriter
}

// NewService creates a new export Service.
func NewService(indicators *indicator.Service, snapshots snapshot.Repository, writer SheetWriter) *Service {
	return &Service{
		indicators: indicators,
		snapshots:  snapshots,
		writer:     writer,
	}
}

// Export calculates all indicators with historical changes and writes to the sheet.
// Implements worker.AfterSnapshotHook.
func (s *Service) Export(ctx context.Context, data domain.FundStructureData) error {
	current, err := s.indicators.CalculateAll(ctx, data)
	if err != nil {
		return fmt.Errorf("calculating current indicators: %w", err)
	}

	historicalByPeriod := s.fetchHistorical(ctx, []int{7, 30, 90, 365})

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

	return s.writer.Write(ctx, rows)
}

// fetchHistorical retrieves historical indicator sets for each period (days ago).
func (s *Service) fetchHistorical(ctx context.Context, periods []int) map[int]map[int]indicator.Indicator {
	result := make(map[int]map[int]indicator.Indicator, len(periods))
	now := time.Now().UTC()

	for _, days := range periods {
		pastDate := now.AddDate(0, 0, -days)
		snap, err := s.snapshots.GetNearestBefore(ctx, "mtlf", pastDate)
		if err != nil {
			slog.Warn("export: historical snapshot unavailable", "days", days, "error", err)
			continue
		}

		var histData domain.FundStructureData
		if err := json.Unmarshal(snap.Data, &histData); err != nil {
			slog.Warn("export: failed to unmarshal historical snapshot", "days", days, "error", err)
			continue
		}

		histInds, err := s.indicators.CalculateAll(ctx, histData)
		if err != nil {
			slog.Warn("export: failed to calculate historical indicators", "days", days, "error", err)
			continue
		}

		result[days] = lo.KeyBy(histInds, func(ind indicator.Indicator) int { return ind.ID })
	}

	return result
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
