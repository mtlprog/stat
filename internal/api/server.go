package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/mtlprog/stat/internal/indicator"
	"github.com/mtlprog/stat/internal/snapshot"
	"github.com/mtlprog/stat/internal/static"
)

// NewServer creates an HTTP server with all routes configured.
func NewServer(port string, snapshots *snapshot.Service, indicators *indicator.Service, adminAPIKey string) *http.Server {
	handler := NewHandler(snapshots)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /skill.md", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Write(static.SkillMD)
	})
	mux.HandleFunc("GET /api/v1/snapshots/latest", handler.GetLatestSnapshot)
	mux.HandleFunc("GET /api/v1/snapshots/{date}", handler.GetSnapshotByDate)
	mux.HandleFunc("GET /api/v1/snapshots", handler.ListSnapshots)

	generateHandler := http.HandlerFunc(handler.GenerateSnapshot)
	if adminAPIKey != "" {
		mux.Handle("POST /api/v1/snapshots/generate", requireAuth(adminAPIKey, generateHandler))
	} else {
		mux.Handle("POST /api/v1/snapshots/generate", generateHandler)
	}

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

func requireAuth(apiKey string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		token := strings.TrimPrefix(auth, "Bearer ")
		if !strings.HasPrefix(auth, "Bearer ") || subtle.ConstantTimeCompare([]byte(token), []byte(apiKey)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}
