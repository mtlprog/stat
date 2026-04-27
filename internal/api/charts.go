package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/indicator"
	"github.com/mtlprog/stat/internal/snapshot"
)

// SubfundSlice is one slice of the balance-by-subfund pie.
type SubfundSlice struct {
	Name    string          `json:"name"`
	Type    string          `json:"type"`
	Address string          `json:"address"`
	Value   decimal.Decimal `json:"value"`
}

// BalanceBySubfundResponse is the response for GET /api/v1/charts/balance-by-subfund.
type BalanceBySubfundResponse struct {
	Date   string         `json:"date"` // YYYY-MM-DD
	Slices []SubfundSlice `json:"slices"`
}

// HistoryPoint is a single (date, value) sample in a time series.
type HistoryPoint struct {
	Date  string          `json:"date"` // YYYY-MM-DD
	Value decimal.Decimal `json:"value"`
}

// IndicatorSeries is one indicator's time series.
type IndicatorSeries struct {
	ID     int            `json:"id"`
	Name   string         `json:"name"`
	Unit   string         `json:"unit"`
	Points []HistoryPoint `json:"points"`
}

// IndicatorHistoryResponse is the response for GET /api/v1/charts/indicator-history.
type IndicatorHistoryResponse struct {
	Series []IndicatorSeries `json:"series"`
}

// ChartsHandler provides chart-data endpoints.
type ChartsHandler struct {
	snapshots *snapshot.Service
	repo      indicator.Repository
}

// NewChartsHandler creates a new charts handler.
func NewChartsHandler(snapshots *snapshot.Service, repo indicator.Repository) *ChartsHandler {
	return &ChartsHandler{snapshots: snapshots, repo: repo}
}

// GetBalanceBySubfund handles GET /api/v1/charts/balance-by-subfund.
//
// @Summary      Fund balance split by sub-fund
// @Description  Returns the EURMTL value of the 4 sub-fund accounts (MABIZ, MCITY, DEFI, BOSS) plus MAIN ISSUER and ADMIN for a given date.
// @Tags         charts
// @Produce      json
// @Param        date  query  string  false  "Snapshot date (YYYY-MM-DD); defaults to latest"
// @Success      200  {object}  BalanceBySubfundResponse
// @Failure      400  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/v1/charts/balance-by-subfund [get]
func (h *ChartsHandler) GetBalanceBySubfund(w http.ResponseWriter, r *http.Request) {
	dateStr := r.URL.Query().Get("date")

	var snap *snapshot.Snapshot
	var err error
	if dateStr != "" {
		date, parseErr := time.Parse("2006-01-02", dateStr)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid date format, expected YYYY-MM-DD")
			return
		}
		snap, err = h.snapshots.GetByDate(r.Context(), fundSlug, date)
	} else {
		snap, err = h.snapshots.GetLatest(r.Context(), fundSlug)
	}
	if err != nil {
		if errors.Is(err, snapshot.ErrNotFound) {
			writeError(w, http.StatusNotFound, "snapshot not found")
			return
		}
		slog.Error("failed to fetch snapshot for subfund pie", "date", dateStr, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var data domain.FundStructureData
	if err := json.Unmarshal(snap.Data, &data); err != nil {
		slog.Error("failed to parse snapshot data", "snapshot_id", snap.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to parse snapshot data")
		return
	}

	// TODO: temporarily includes issuer + operational alongside subfond so the pie matches
	// AggregatedAccounts(); remove once the chart is finalized.
	subfonds := lo.Filter(data.Accounts, func(a domain.FundAccountPortfolio, _ int) bool {
		return a.Type == domain.AccountTypeSubfond ||
			a.Type == domain.AccountTypeIssuer ||
			a.Type == domain.AccountTypeOperational
	})

	addrByName := make(map[string]string, len(subfonds))
	for _, acc := range domain.AccountRegistry() {
		addrByName[acc.Name] = acc.Address
	}

	slices := lo.Map(subfonds, func(a domain.FundAccountPortfolio, _ int) SubfundSlice {
		return SubfundSlice{
			Name:    a.Name,
			Type:    string(a.Type),
			Address: addrByName[a.Name],
			Value:   a.TotalEURMTL,
		}
	})

	writeJSON(w, http.StatusOK, BalanceBySubfundResponse{
		Date:   snap.SnapshotDate.UTC().Format("2006-01-02"),
		Slices: slices,
	})
}

// GetIndicatorHistory handles GET /api/v1/charts/indicator-history.
//
// @Summary      Indicator time-series
// @Description  Returns historical points for one or more indicator IDs over the requested range.
// @Tags         charts
// @Produce      json
// @Param        ids    query  string  true   "Comma-separated indicator IDs (e.g. 1,3,17,24,27)"
// @Param        range  query  string  false  "Range: 30d, 90d, 180d, 365d, or 'all' (default: 90d)"
// @Success      200  {object}  IndicatorHistoryResponse
// @Failure      400  {object}  map[string]string
// @Router       /api/v1/charts/indicator-history [get]
func (h *ChartsHandler) GetIndicatorHistory(w http.ResponseWriter, r *http.Request) {
	idsStr := r.URL.Query().Get("ids")
	if idsStr == "" {
		writeError(w, http.StatusBadRequest, "missing required query param: ids")
		return
	}
	ids, err := parseIDList(idsStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	from, err := parseHistoryRange(r.URL.Query().Get("range"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	points, err := h.repo.GetHistory(r.Context(), fundSlug, ids, from)
	if err != nil {
		slog.Error("failed to fetch indicator history", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, IndicatorHistoryResponse{Series: groupHistory(ids, points)})
}

// groupHistory groups history points by indicator ID, preserving the requested ID order
// and joining metadata. Indicators with no points are returned with an empty Points slice.
func groupHistory(ids []int, points []indicator.HistoryPoint) []IndicatorSeries {
	pointsByID := make(map[int][]HistoryPoint, len(ids))
	for _, p := range points {
		pointsByID[p.IndicatorID] = append(pointsByID[p.IndicatorID], HistoryPoint{
			Date:  p.SnapshotDate.UTC().Format("2006-01-02"),
			Value: p.Value,
		})
	}

	series := make([]IndicatorSeries, len(ids))
	for i, id := range ids {
		meta := indicator.NewIndicator(id, decimal.Zero, "", "")
		series[i] = IndicatorSeries{
			ID:     id,
			Name:   meta.Name,
			Unit:   meta.Unit,
			Points: pointsByID[id],
		}
		if series[i].Points == nil {
			series[i].Points = []HistoryPoint{}
		}
	}
	return series
}

// parseIDList parses a comma-separated list of positive ints, deduplicating while
// preserving first-seen order.
func parseIDList(s string) ([]int, error) {
	seen := make(map[int]bool)
	var ids []int
	for _, tok := range strings.Split(s, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		n, err := strconv.Atoi(tok)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid indicator id %q", tok)
		}
		if !seen[n] {
			seen[n] = true
			ids = append(ids, n)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("ids list is empty")
	}
	return ids, nil
}

// parseHistoryRange returns the cutoff `from` date for the requested range.
// Empty defaults to 90d. "all" returns the zero time (no lower bound).
func parseHistoryRange(s string) (time.Time, error) {
	now := time.Now().UTC().Truncate(24 * time.Hour)
	if s == "" {
		return now.AddDate(0, 0, -90), nil
	}
	if s == "all" {
		return time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), nil
	}
	days, ok := parsePeriodDays(s)
	if !ok {
		return time.Time{}, fmt.Errorf("invalid range %q, valid: 30d, 90d, 180d, 365d, or 'all'", s)
	}
	return now.AddDate(0, 0, -days), nil
}
