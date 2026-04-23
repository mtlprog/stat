package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/snapshot"
)

// compatFundStructure is the response format for GET /api/fund-structure (legacy).
// It merges Accounts + MutualFunds into a single Accounts slice and omits
// Warnings and LiveMetrics to match the old dreadnought API.
type compatFundStructure struct {
	Accounts         []domain.FundAccountPortfolio `json:"accounts"`
	OtherAccounts    []domain.FundAccountPortfolio `json:"otherAccounts"`
	AggregatedTotals domain.AggregatedTotals       `json:"aggregatedTotals"`
}

// ListSnapshotsCompat handles GET /api/snapshots (legacy).
func (h *Handler) ListSnapshotsCompat(w http.ResponseWriter, r *http.Request) {
	metas, err := h.snapshots.ListMeta(r.Context(), "mtlf")
	if err != nil {
		slog.Error("failed to list snapshots (compat)", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, metas)
}

// GetFundStructureCompat handles GET /api/fund-structure (legacy).
func (h *Handler) GetFundStructureCompat(w http.ResponseWriter, r *http.Request) {
	dateStr := r.URL.Query().Get("date")

	var s *snapshot.Snapshot
	var err error

	if dateStr != "" {
		date, parseErr := time.Parse("2006-01-02", dateStr)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid date format, expected YYYY-MM-DD")
			return
		}
		s, err = h.snapshots.GetByDate(r.Context(), "mtlf", date)
	} else {
		s, err = h.snapshots.GetLatest(r.Context(), "mtlf")
	}

	if err != nil {
		if errors.Is(err, snapshot.ErrNotFound) {
			writeError(w, http.StatusNotFound, "snapshot not found")
			return
		}
		slog.Error("failed to get fund structure (compat)", "date", dateStr, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var data domain.FundStructureData
	if err := json.Unmarshal(s.Data, &data); err != nil {
		slog.Error("failed to unmarshal snapshot data (compat)", "snapshot_id", s.ID, "date", s.SnapshotDate, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	compat := compatFundStructure{
		Accounts:         slices.Concat(data.Accounts, data.MutualFunds),
		OtherAccounts:    data.OtherAccounts,
		AggregatedTotals: data.AggregatedTotals,
	}
	writeJSON(w, http.StatusOK, compat)
}
