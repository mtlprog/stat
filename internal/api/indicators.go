package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/indicator"
	"github.com/mtlprog/stat/internal/snapshot"
)

// IndicatorHandler provides HTTP endpoints for indicators.
type IndicatorHandler struct {
	snapshots  *snapshot.Service
	indicators *indicator.Service
}

// NewIndicatorHandler creates a new indicator handler.
func NewIndicatorHandler(snapshots *snapshot.Service, indicators *indicator.Service) *IndicatorHandler {
	return &IndicatorHandler{snapshots: snapshots, indicators: indicators}
}

// IndicatorComparison extends Indicator with optional period-over-period change data.
type IndicatorComparison struct {
	ID        int              `json:"id"`
	Name      string           `json:"name"`
	Value     decimal.Decimal  `json:"value"`
	Unit      string           `json:"unit"`
	ChangeAbs *decimal.Decimal `json:"change_abs,omitempty"`
	ChangePct *decimal.Decimal `json:"change_pct,omitempty"`
}

// GetIndicators handles GET /api/v1/indicators — latest snapshot indicators.
// Accepts optional ?compare=30d|90d|180d|365d to include period-over-period changes.
func (h *IndicatorHandler) GetIndicators(w http.ResponseWriter, r *http.Request) {
	s, err := h.snapshots.GetLatest(r.Context(), "mtlf")
	if err != nil {
		if errors.Is(err, snapshot.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no snapshots found")
			return
		}
		slog.Error("failed to get latest snapshot", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var data domain.FundStructureData
	if err := json.Unmarshal(s.Data, &data); err != nil {
		slog.Error("failed to parse snapshot data", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to parse snapshot data")
		return
	}

	indicators, err := h.indicators.CalculateAll(r.Context(), data)
	if err != nil {
		slog.Error("failed to calculate indicators", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	compareParam := r.URL.Query().Get("compare")
	if compareParam == "" {
		writeJSON(w, http.StatusOK, indicators)
		return
	}

	days, ok := parsePeriodDays(compareParam)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid compare period, use 30d, 90d, 180d, or 365d")
		return
	}

	historicalMap := h.fetchHistoricalIndicators(r, days)

	result := make([]IndicatorComparison, len(indicators))
	for i, ind := range indicators {
		comp := IndicatorComparison{
			ID:    ind.ID,
			Name:  ind.Name,
			Value: ind.Value,
			Unit:  ind.Unit,
		}
		if hist, ok := historicalMap[ind.ID]; ok && !hist.Value.IsZero() {
			changeAbs := ind.Value.Sub(hist.Value)
			changePct := changeAbs.Div(hist.Value).Mul(decimal.NewFromInt(100))
			comp.ChangeAbs = &changeAbs
			comp.ChangePct = &changePct
		}
		result[i] = comp
	}

	writeJSON(w, http.StatusOK, result)
}

// fetchHistoricalIndicators retrieves indicators for a snapshot N days ago.
// Returns an empty map if no snapshot is available for that date.
func (h *IndicatorHandler) fetchHistoricalIndicators(r *http.Request, days int) map[int]indicator.Indicator {
	historicalDate := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -days)
	historicalSnap, err := h.snapshots.GetByDate(r.Context(), "mtlf", historicalDate)
	if err != nil {
		if !errors.Is(err, snapshot.ErrNotFound) {
			slog.Warn("failed to fetch historical snapshot for comparison", "days", days, "error", err)
		}
		return nil
	}

	var historicalData domain.FundStructureData
	if err := json.Unmarshal(historicalSnap.Data, &historicalData); err != nil {
		slog.Warn("failed to parse historical snapshot data", "error", err)
		return nil
	}

	histInds, err := h.indicators.CalculateAll(r.Context(), historicalData)
	if err != nil {
		slog.Warn("failed to calculate historical indicators", "error", err)
		return nil
	}

	result := make(map[int]indicator.Indicator, len(histInds))
	for _, ind := range histInds {
		result[ind.ID] = ind
	}
	return result
}

// parsePeriodDays converts a period string (e.g. "30d") to a number of days.
func parsePeriodDays(s string) (int, bool) {
	switch s {
	case "30d":
		return 30, true
	case "90d":
		return 90, true
	case "180d":
		return 180, true
	case "365d":
		return 365, true
	}
	return 0, false
}

// GetIndicatorsByDate handles GET /api/v1/indicators/{date}.
func (h *IndicatorHandler) GetIndicatorsByDate(w http.ResponseWriter, r *http.Request) {
	dateStr := r.PathValue("date")
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid date format, expected YYYY-MM-DD")
		return
	}

	s, err := h.snapshots.GetByDate(r.Context(), "mtlf", date)
	if err != nil {
		if errors.Is(err, snapshot.ErrNotFound) {
			writeError(w, http.StatusNotFound, "snapshot not found for date")
			return
		}
		slog.Error("failed to get snapshot by date", "date", dateStr, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var data domain.FundStructureData
	if err := json.Unmarshal(s.Data, &data); err != nil {
		slog.Error("failed to parse snapshot data", "date", dateStr, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to parse snapshot data")
		return
	}

	indicators, err := h.indicators.CalculateAll(r.Context(), data)
	if err != nil {
		slog.Error("failed to calculate indicators", "date", dateStr, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, indicators)
}
