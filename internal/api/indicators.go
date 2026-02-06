package api

import (
	"encoding/json"
	"net/http"
	"time"

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

// GetIndicators handles GET /api/v1/indicators — latest snapshot indicators.
func (h *IndicatorHandler) GetIndicators(w http.ResponseWriter, r *http.Request) {
	s, err := h.snapshots.GetLatest(r.Context(), "mtlf")
	if err != nil {
		writeError(w, http.StatusNotFound, "no snapshots found")
		return
	}

	var data domain.FundStructureData
	if err := json.Unmarshal(s.Data, &data); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to parse snapshot data")
		return
	}

	indicators, err := h.indicators.CalculateAll(r.Context(), data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to calculate indicators: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, indicators)
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
		writeError(w, http.StatusNotFound, "snapshot not found for date")
		return
	}

	var data domain.FundStructureData
	if err := json.Unmarshal(s.Data, &data); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to parse snapshot data")
		return
	}

	indicators, err := h.indicators.CalculateAll(r.Context(), data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to calculate indicators: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, indicators)
}
