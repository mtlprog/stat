package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/indicator"
)

const fundSlug = "mtlf"

// PeriodChange holds absolute and percentage change for one comparison period.
type PeriodChange struct {
	Abs decimal.Decimal `json:"abs"`
	Pct decimal.Decimal `json:"pct"`
}

// IndicatorWithChanges extends Indicator with optional multi-period changes.
// `changes` is omitted when ?compare is not requested or no historical data exists.
type IndicatorWithChanges struct {
	ID          int                     `json:"id"`
	Name        string                  `json:"name"`
	Value       decimal.Decimal         `json:"value"`
	Unit        string                  `json:"unit"`
	Description string                  `json:"description,omitempty"`
	Changes     map[string]PeriodChange `json:"changes,omitempty"`
}

// IndicatorHandler provides HTTP endpoints for indicators backed by fund_indicators.
type IndicatorHandler struct {
	repo indicator.Repository
}

// NewIndicatorHandler creates a new indicator handler.
func NewIndicatorHandler(repo indicator.Repository) *IndicatorHandler {
	return &IndicatorHandler{repo: repo}
}

// GetIndicators handles GET /api/v1/indicators.
//
// @Summary      Latest indicators
// @Description  Returns indicators from the most recent stored snapshot. Optional `compare` adds period-over-period changes.
// @Tags         indicators
// @Produce      json
// @Param        compare  query  string  false  "Comma-separated periods: any of 30d,90d,180d,365d, or 'all'"
// @Success      200  {array}   IndicatorWithChanges
// @Failure      400  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/v1/indicators [get]
func (h *IndicatorHandler) GetIndicators(w http.ResponseWriter, r *http.Request) {
	indicators, latestDate, err := h.repo.GetLatest(r.Context(), fundSlug)
	if err != nil {
		if errors.Is(err, indicator.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no indicators found")
			return
		}
		slog.Error("failed to get latest indicators", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	periods, err := parsePeriodList(r.URL.Query().Get("compare"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if len(periods) == 0 {
		writeJSON(w, http.StatusOK, toWithChanges(indicators, nil))
		return
	}

	historical := make(map[int]map[int]indicator.Indicator, len(periods))
	for _, days := range periods {
		hist, err := h.repo.GetNearestBefore(r.Context(), fundSlug, latestDate.AddDate(0, 0, -days))
		if err != nil {
			slog.Error("failed to fetch historical indicators", "days", days, "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if hist != nil {
			historical[days] = hist
		}
	}

	writeJSON(w, http.StatusOK, toWithChanges(indicators, buildChanges(indicators, periods, historical)))
}

// GetIndicatorsByDate handles GET /api/v1/indicators/{date}.
//
// @Summary      Indicators by date
// @Description  Returns stored indicators for an exact snapshot date.
// @Tags         indicators
// @Produce      json
// @Param        date  path  string  true  "Snapshot date (YYYY-MM-DD)"
// @Success      200  {array}   indicator.Indicator
// @Failure      400  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/v1/indicators/{date} [get]
func (h *IndicatorHandler) GetIndicatorsByDate(w http.ResponseWriter, r *http.Request) {
	dateStr := r.PathValue("date")
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid date format, expected YYYY-MM-DD")
		return
	}

	indicators, err := h.repo.GetByDate(r.Context(), fundSlug, date)
	if err != nil {
		if errors.Is(err, indicator.ErrNotFound) {
			writeError(w, http.StatusNotFound, "indicators not found for date")
			return
		}
		slog.Error("failed to get indicators by date", "date", dateStr, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, indicators)
}

// toWithChanges wraps each Indicator with an optional Changes map (nil if no compare requested).
func toWithChanges(indicators []indicator.Indicator, changesByID map[int]map[string]PeriodChange) []IndicatorWithChanges {
	result := make([]IndicatorWithChanges, len(indicators))
	for i, ind := range indicators {
		result[i] = IndicatorWithChanges{
			ID:          ind.ID,
			Name:        ind.Name,
			Value:       ind.Value,
			Unit:        ind.Unit,
			Description: ind.Description,
			Changes:     changesByID[ind.ID],
		}
	}
	return result
}

// buildChanges computes the per-indicator changes map keyed by period label (e.g. "30d").
// Periods with no historical row, or where the historical value is zero, are omitted.
func buildChanges(current []indicator.Indicator, periods []int, historical map[int]map[int]indicator.Indicator) map[int]map[string]PeriodChange {
	out := make(map[int]map[string]PeriodChange, len(current))
	for _, ind := range current {
		var changes map[string]PeriodChange
		for _, days := range periods {
			hist, ok := historical[days][ind.ID]
			if !ok || hist.Value.IsZero() {
				continue
			}
			abs := ind.Value.Sub(hist.Value)
			pct := abs.Div(hist.Value).Mul(decimal.NewFromInt(100))
			if changes == nil {
				changes = make(map[string]PeriodChange, len(periods))
			}
			changes[periodLabel(days)] = PeriodChange{Abs: abs, Pct: pct}
		}
		if changes != nil {
			out[ind.ID] = changes
		}
	}
	return out
}

// periodLabel formats a day count back to its canonical label (e.g. 30 → "30d").
func periodLabel(days int) string {
	return fmt.Sprintf("%dd", days)
}

// parsePeriodList parses a comma-separated list of period tokens (e.g. "30d,90d") or
// the special token "all" (= 30d,90d,180d,365d). Returns deduplicated, sorted day counts.
// Returns nil with no error when the input is empty.
func parsePeriodList(s string) ([]int, error) {
	if s == "" {
		return nil, nil
	}
	if s == "all" {
		return []int{30, 90, 180, 365}, nil
	}

	seen := make(map[int]bool)
	var days []int
	for _, tok := range strings.Split(s, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		n, ok := parsePeriodDays(tok)
		if !ok {
			return nil, fmt.Errorf("invalid period %q, valid: 30d, 90d, 180d, 365d, or 'all'", tok)
		}
		if !seen[n] {
			seen[n] = true
			days = append(days, n)
		}
	}
	return days, nil
}

// parsePeriodDays converts a single period string (e.g. "30d") to a number of days.
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
