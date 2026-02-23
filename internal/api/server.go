package api

import (
	"net/http"
	"time"

	"github.com/mtlprog/stat/internal/indicator"
	"github.com/mtlprog/stat/internal/snapshot"
	"github.com/mtlprog/stat/internal/static"
)

// NewServer creates an HTTP server with all routes configured.
func NewServer(port string, snapshots *snapshot.Service, indicators *indicator.Service) *http.Server {
	handler := NewHandler(snapshots)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /skill.md", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Write(static.SkillMD)
	})
	mux.HandleFunc("GET /api/v1/snapshots/latest", handler.GetLatestSnapshot)
	mux.HandleFunc("GET /api/v1/snapshots/{date}", handler.GetSnapshotByDate)
	mux.HandleFunc("GET /api/v1/snapshots", handler.ListSnapshots)

	if indicators != nil {
		indHandler := NewIndicatorHandler(snapshots, indicators)
		mux.HandleFunc("GET /api/v1/indicators", indHandler.GetIndicators)
		mux.HandleFunc("GET /api/v1/indicators/{date}", indHandler.GetIndicatorsByDate)
	}

	return &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
}
