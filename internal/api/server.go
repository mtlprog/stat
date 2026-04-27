package api

import (
	"net/http"
	"time"

	httpswagger "github.com/swaggo/http-swagger"

	_ "github.com/mtlprog/stat/docs"
	"github.com/mtlprog/stat/internal/indicator"
	"github.com/mtlprog/stat/internal/snapshot"
	"github.com/mtlprog/stat/internal/static"
)

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// NewServer creates an HTTP server with all routes configured.
//
// @title           MTL Fund Statistics API
// @version         1.0
// @description     Read-only API exposing fund snapshots, computed indicators, and chart data.
// @BasePath        /
func NewServer(port string, snapshots *snapshot.Service, indicators indicator.Repository) *http.Server {
	handler := NewHandler(snapshots)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /skill.md", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Write(static.SkillMD)
	})
	mux.HandleFunc("GET /api/v1/snapshots/latest", handler.GetLatestSnapshot)
	mux.HandleFunc("GET /api/v1/snapshots/{date}", handler.GetSnapshotByDate)
	mux.HandleFunc("GET /api/v1/snapshots", handler.ListSnapshots)

	// Legacy endpoints for dreadnought frontend compatibility.
	mux.HandleFunc("GET /api/snapshots", handler.ListSnapshotsCompat)
	mux.HandleFunc("GET /api/fund-structure", handler.GetFundStructureCompat)

	if indicators != nil {
		indHandler := NewIndicatorHandler(indicators)
		chartsHandler := NewChartsHandler(snapshots, indicators)
		mux.HandleFunc("GET /api/v1/indicators", indHandler.GetIndicators)
		mux.HandleFunc("GET /api/v1/indicators/{date}", indHandler.GetIndicatorsByDate)
		mux.HandleFunc("GET /api/v1/charts/balance-by-subfund", chartsHandler.GetBalanceBySubfund)
		mux.HandleFunc("GET /api/v1/charts/indicator-history", chartsHandler.GetIndicatorHistory)
	}

	mux.Handle("GET /swagger/", httpswagger.Handler(httpswagger.URL("/swagger/doc.json")))

	return &http.Server{
		Addr:         ":" + port,
		Handler:      corsMiddleware(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
}
