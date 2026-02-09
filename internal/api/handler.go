package api

import (
	"encoding/json"
	"errors"
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
		if errors.Is(err, snapshot.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no snapshots found")
			return
		}
		slog.Error("failed to get latest snapshot", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
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
		if errors.Is(err, snapshot.ErrNotFound) {
			writeError(w, http.StatusNotFound, "snapshot not found for date")
			return
		}
		slog.Error("failed to get snapshot by date", "date", dateStr, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
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
		slog.Error("failed to list snapshots", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, snapshots)
}

// GenerateSnapshot handles POST /api/v1/snapshots/generate.
func (h *Handler) GenerateSnapshot(w http.ResponseWriter, r *http.Request) {
	data, err := h.snapshots.Generate(r.Context(), "mtlf", time.Now())
	if err != nil {
		slog.Error("failed to generate snapshot", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to generate snapshot")
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("failed to marshal JSON response", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(data); err != nil {
		slog.Warn("failed to write HTTP response body", "error", err)
		return
	}
	_, _ = w.Write([]byte("\n"))
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
