package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/mtlprog/stat/internal/snapshot"
)

// Handler provides HTTP endpoints for the statistics API.
type Handler struct {
	snapshots *snapshot.Service
}

// NewHandler creates a new API handler.
func NewHandler(snapshots *snapshot.Service) *Handler {
	return &Handler{snapshots: snapshots}
}

// GetLatestSnapshot handles GET /api/v1/snapshots/latest.
func (h *Handler) GetLatestSnapshot(w http.ResponseWriter, r *http.Request) {
	s, err := h.snapshots.GetLatest(r.Context(), "mtlf")
	if err != nil {
		writeError(w, http.StatusNotFound, "no snapshots found")
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// GetSnapshotByDate handles GET /api/v1/snapshots/{date}.
func (h *Handler) GetSnapshotByDate(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, s)
}

// ListSnapshots handles GET /api/v1/snapshots.
func (h *Handler) ListSnapshots(w http.ResponseWriter, r *http.Request) {
	const maxLimit = 365
	limit := 30
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = min(n, maxLimit)
		}
	}

	snapshots, err := h.snapshots.List(r.Context(), "mtlf", limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list snapshots")
		return
	}
	writeJSON(w, http.StatusOK, snapshots)
}

// GenerateSnapshot handles POST /api/v1/snapshots/generate.
func (h *Handler) GenerateSnapshot(w http.ResponseWriter, r *http.Request) {
	data, err := h.snapshots.Generate(r.Context(), "mtlf", time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate snapshot: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode JSON response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
