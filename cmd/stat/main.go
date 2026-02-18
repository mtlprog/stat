package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/mtlprog/stat/internal/api"
	"github.com/mtlprog/stat/internal/config"
	"github.com/mtlprog/stat/internal/database"
	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/external"
	"github.com/mtlprog/stat/internal/fund"
	"github.com/mtlprog/stat/internal/horizon"
	"github.com/mtlprog/stat/internal/indicator"
	"github.com/mtlprog/stat/internal/metrics"
	"github.com/mtlprog/stat/internal/portfolio"
	"github.com/mtlprog/stat/internal/price"
	"github.com/mtlprog/stat/internal/snapshot"
	"github.com/mtlprog/stat/internal/valuation"
	"github.com/mtlprog/stat/internal/worker"
	"github.com/mtlprog/stat/migrations"
)

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
	if err := database.RunMigrations(ctx, pool, migrations.FS); err != nil {
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

	// Snapshot service with metrics enrichment
	snapshotRepo := snapshot.NewPgRepository(pool)
	var fundAddrs []string
	for _, a := range domain.AccountRegistry() {
		fundAddrs = append(fundAddrs, a.Address)
	}
	metricsSvc := metrics.NewService(horizonClient, priceSvc, fundAddrs)
	snapshotSvc := snapshot.NewService(fundSvc, snapshotRepo, metricsSvc)

	// Ensure default entity exists
	if _, err := snapshotRepo.EnsureEntity(ctx, "mtlf", "Montelibero Fund", "Montelibero Fund statistics"); err != nil {
		log.Fatalf("Failed to ensure entity: %v", err)
	}

	// Start workers
	quoteWorker := worker.NewQuoteWorker(externalSvc, cfg.QuoteWorkerInterval)
	go quoteWorker.Run(ctx)

	reportWorker := worker.NewReportWorker(snapshotSvc, cfg.ReportWorkerInterval)
	go reportWorker.Run(ctx)

	// Wire historical data for time-series indicators (I55, etc.)
	hist := &indicator.HistoricalData{
		Repo: snapshotRepo,
		Slug: "mtlf",
	}

	indicatorSvc := indicator.NewService(priceSvc, horizonClient, hist)

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
