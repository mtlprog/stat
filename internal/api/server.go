package api

import (
	"net/http"

	"github.com/mtlprog/stat/internal/indicator"
	"github.com/mtlprog/stat/internal/snapshot"
)

// NewServer creates an HTTP server with all routes configured.
func NewServer(port string, snapshots *snapshot.Service, indicators *indicator.Service) *http.Server {
	handler := NewHandler(snapshots)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/snapshots/latest", handler.GetLatestSnapshot)
	mux.HandleFunc("GET /api/v1/snapshots/{date}", handler.GetSnapshotByDate)
	mux.HandleFunc("GET /api/v1/snapshots", handler.ListSnapshots)
	mux.HandleFunc("POST /api/v1/snapshots/generate", handler.GenerateSnapshot)

	if indicators != nil {
		indHandler := NewIndicatorHandler(snapshots, indicators)
		mux.HandleFunc("GET /api/v1/indicators", indHandler.GetIndicators)
		mux.HandleFunc("GET /api/v1/indicators/{date}", indHandler.GetIndicatorsByDate)
	}

	return &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}
}
