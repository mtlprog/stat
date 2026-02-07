package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/mtlprog/stat/internal/api"
	"github.com/mtlprog/stat/internal/config"
	"github.com/mtlprog/stat/internal/database"
	"github.com/mtlprog/stat/internal/external"
	"github.com/mtlprog/stat/internal/fund"
	"github.com/mtlprog/stat/internal/horizon"
	"github.com/mtlprog/stat/internal/indicator"
	"github.com/mtlprog/stat/internal/portfolio"
	"github.com/mtlprog/stat/internal/price"
	"github.com/mtlprog/stat/internal/snapshot"
	"github.com/mtlprog/stat/internal/valuation"
	"github.com/mtlprog/stat/internal/worker"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()

	// Connect to database
	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	// Run migrations
	migrationsSub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		log.Fatalf("Failed to create migrations sub-fs: %v", err)
	}
	if err := database.RunMigrations(ctx, pool, migrationsSub); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Create Horizon client
	horizonClient := horizon.NewClient(cfg.HorizonURL, cfg.HorizonRetryMax, cfg.HorizonRetryBaseDelay)

	// Create services
	portfolioSvc := portfolio.NewService(horizonClient)
	priceSvc := price.NewService(horizonClient)
	valuationSvc := valuation.NewService(horizonClient)

	// External price service
	coingecko := external.NewCoinGeckoClient(cfg.CoinGeckoURL, cfg.CoinGeckoDelay, cfg.CoinGeckoRetryMax)
	quoteRepo := external.NewPgQuoteRepository(pool)
	externalSvc := external.NewService(coingecko, quoteRepo)

	// Fund structure service
	fundSvc := fund.NewService(portfolioSvc, priceSvc, valuationSvc, externalSvc)

	// Snapshot service
	snapshotRepo := snapshot.NewPgRepository(pool)
	snapshotSvc := snapshot.NewService(fundSvc, snapshotRepo)

	// Ensure default entity exists
	if _, err := snapshotRepo.EnsureEntity(ctx, "mtlf", "Montelibero Fund", "Montelibero Fund statistics"); err != nil {
		log.Fatalf("Failed to ensure entity: %v", err)
	}

	// Start workers
	quoteWorker := worker.NewQuoteWorker(externalSvc, cfg.QuoteWorkerInterval)
	go quoteWorker.Run(ctx)

	reportWorker := worker.NewReportWorker(snapshotSvc, cfg.ReportWorkerInterval)
	go reportWorker.Run(ctx)

	// Indicator service (HistoricalData still nil — requires snapshot history, a separate feature)
	indicatorSvc := indicator.NewService(priceSvc, horizonClient, nil)

	if cfg.AdminAPIKey == "" {
		slog.Warn("ADMIN_API_KEY not set, generate endpoint is unprotected")
	}

	// Start HTTP server
	srv := api.NewServer(cfg.HTTPPort, snapshotSvc, indicatorSvc, cfg.AdminAPIKey)

	go func() {
		log.Printf("HTTP server listening on :%s", cfg.HTTPPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
			stop()
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()
	log.Println("Shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("Shutdown complete")
}
