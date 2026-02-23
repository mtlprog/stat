package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/mtlprog/stat/internal/api"
	"github.com/mtlprog/stat/internal/config"
	"github.com/mtlprog/stat/internal/database"
	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/export"
	"github.com/mtlprog/stat/internal/external"
	"github.com/mtlprog/stat/internal/fund"
	"github.com/mtlprog/stat/internal/horizon"
	"github.com/mtlprog/stat/internal/indicator"
	"github.com/mtlprog/stat/internal/metrics"
	"github.com/mtlprog/stat/internal/portfolio"
	"github.com/mtlprog/stat/internal/price"
	"github.com/mtlprog/stat/internal/snapshot"
	"github.com/mtlprog/stat/internal/valuation"
	"github.com/mtlprog/stat/migrations"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app := &cli.App{
		Name:  "stat",
		Usage: "Montelibero Fund statistics",
		Commands: []*cli.Command{
			{
				Name:   "serve",
				Usage:  "Start HTTP API server",
				Action: runServe,
			},
			{
				Name:   "quote",
				Usage:  "Fetch and store external price quotes",
				Action: runQuote,
			},
			{
				Name:   "report",
				Usage:  "Generate fund snapshot and export to Sheets",
				Action: runReport,
			},
		},
	}

	if err := app.RunContext(ctx, os.Args); err != nil {
		log.Fatal(err)
	}
}

func runQuote(c *cli.Context) error {
	ctx := c.Context
	cfg := config.Load()

	if cfg.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

	if err := database.RunMigrations(ctx, pool, migrations.FS); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	coingecko := external.NewCoinGeckoClient(cfg.CoinGeckoURL, cfg.CoinGeckoDelay, cfg.CoinGeckoRetryMax)
	quoteRepo := external.NewPgQuoteRepository(pool)
	externalSvc := external.NewService(coingecko, quoteRepo)

	if err := externalSvc.FetchAndStoreQuotes(ctx); err != nil {
		return fmt.Errorf("fetching quotes: %w", err)
	}

	slog.Info("quotes fetched successfully")
	return nil
}

func runReport(c *cli.Context) error {
	ctx := c.Context
	cfg := config.Load()

	if cfg.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

	if err := database.RunMigrations(ctx, pool, migrations.FS); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	horizonClient := horizon.NewClient(cfg.HorizonURL, cfg.HorizonRetryMax, cfg.HorizonRetryBaseDelay)
	portfolioSvc := portfolio.NewService(horizonClient)
	priceSvc := price.NewService(horizonClient)
	valuationSvc := valuation.NewService(horizonClient)

	coingecko := external.NewCoinGeckoClient(cfg.CoinGeckoURL, cfg.CoinGeckoDelay, cfg.CoinGeckoRetryMax)
	quoteRepo := external.NewPgQuoteRepository(pool)
	externalSvc := external.NewService(coingecko, quoteRepo)

	fundSvc := fund.NewService(portfolioSvc, priceSvc, valuationSvc, externalSvc)

	snapshotRepo := snapshot.NewPgRepository(pool)
	var fundAddrs []string
	for _, a := range domain.AccountRegistry() {
		fundAddrs = append(fundAddrs, a.Address)
	}
	metricsSvc := metrics.NewService(horizonClient, priceSvc, fundAddrs)
	snapshotSvc := snapshot.NewService(fundSvc, snapshotRepo, metricsSvc)

	if _, err := snapshotRepo.EnsureEntity(ctx, "mtlf", "Montelibero Fund", "Montelibero Fund statistics"); err != nil {
		return fmt.Errorf("ensuring entity: %w", err)
	}

	now := time.Now().UTC()
	date := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	data, err := snapshotSvc.Generate(ctx, "mtlf", date)
	if err != nil {
		return fmt.Errorf("generating snapshot: %w", err)
	}
	slog.Info("snapshot generated successfully", "date", date.Format("2006-01-02"))

	if cfg.GoogleSheetsSpreadsheetID != "" && cfg.GoogleCredentialsJSON != "" {
		hist := &indicator.HistoricalData{Repo: snapshotRepo, Slug: "mtlf"}
		indicatorSvc := indicator.NewService(priceSvc, horizonClient, hist)

		sheetsWriter, err := export.NewSheetsWriter(ctx, cfg.GoogleSheetsSpreadsheetID, cfg.GoogleCredentialsJSON)
		if err != nil {
			return fmt.Errorf("initializing Google Sheets writer: %w", err)
		}
		exportSvc := export.NewService(indicatorSvc, snapshotRepo, sheetsWriter)
		if err := exportSvc.Export(ctx, data); err != nil {
			return fmt.Errorf("exporting to Google Sheets: %w", err)
		}
		slog.Info("Google Sheets export completed")
	}

	return nil
}

func runServe(c *cli.Context) error {
	ctx := c.Context
	cfg := config.Load()

	if cfg.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

	if err := database.RunMigrations(ctx, pool, migrations.FS); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	horizonClient := horizon.NewClient(cfg.HorizonURL, cfg.HorizonRetryMax, cfg.HorizonRetryBaseDelay)
	portfolioSvc := portfolio.NewService(horizonClient)
	priceSvc := price.NewService(horizonClient)
	valuationSvc := valuation.NewService(horizonClient)

	coingecko := external.NewCoinGeckoClient(cfg.CoinGeckoURL, cfg.CoinGeckoDelay, cfg.CoinGeckoRetryMax)
	quoteRepo := external.NewPgQuoteRepository(pool)
	externalSvc := external.NewService(coingecko, quoteRepo)

	fundSvc := fund.NewService(portfolioSvc, priceSvc, valuationSvc, externalSvc)

	snapshotRepo := snapshot.NewPgRepository(pool)
	var fundAddrs []string
	for _, a := range domain.AccountRegistry() {
		fundAddrs = append(fundAddrs, a.Address)
	}
	metricsSvc := metrics.NewService(horizonClient, priceSvc, fundAddrs)
	snapshotSvc := snapshot.NewService(fundSvc, snapshotRepo, metricsSvc)

	if _, err := snapshotRepo.EnsureEntity(ctx, "mtlf", "Montelibero Fund", "Montelibero Fund statistics"); err != nil {
		return fmt.Errorf("ensuring entity: %w", err)
	}

	hist := &indicator.HistoricalData{Repo: snapshotRepo, Slug: "mtlf"}
	indicatorSvc := indicator.NewService(priceSvc, horizonClient, hist)

	srv := api.NewServer(cfg.HTTPPort, snapshotSvc, indicatorSvc)

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("HTTP server listening", "port", cfg.HTTPPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("HTTP server: %w", err)
	case <-ctx.Done():
		slog.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP server shutdown error", "error", err)
	}

	slog.Info("shutdown complete")
	return nil
}
