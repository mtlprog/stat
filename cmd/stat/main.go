// Command stat is the Montelibero Fund statistics CLI.
//
// @title           MTL Fund Statistics API
// @version         1.0
// @description     Read-only API exposing fund snapshots, computed indicators, and chart data.
// @description     All numeric fields encoded as JSON strings to preserve full Stellar-stroop precision.
// @BasePath        /
// @schemes         http https
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/urfave/cli/v2"
	"github.com/xuri/excelize/v2"

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
			{
				Name:  "import",
				Usage: "Import historical snapshots from the old stat API",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "api-url",
						Usage: "Base URL of the old stat API",
						Value: "https://stat.mtlf.me",
					},
				},
				Action: runImport,
			},
			{
				Name:  "import-excel",
				Usage: "Import historical MONITORING data from an Excel file",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "file",
						Usage:    "Path to the Excel file (e.g. MTL_report_1.xlsx)",
						Required: true,
					},
				},
				Action: runImportExcel,
			},
			{
				Name:   "backfill-indicators",
				Usage:  "Recompute and persist deterministic indicators for all stored snapshots",
				Action: runBackfillIndicators,
			},
			{
				Name:   "import-indicators-from-sheets",
				Usage:  "Import historical indicator values from the MONITORING Google Sheets tab into fund_indicators",
				Action: runImportIndicatorsFromSheets,
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

// reportTimeout caps the daily report run. Anything longer is a regression we
// want surfaced as a non-zero exit so Railway alerts the maintainer instead of
// silently overlapping with the next day's cron.
const reportTimeout = 30 * time.Minute

func runReport(c *cli.Context) error {
	ctx, cancel := context.WithTimeout(c.Context, reportTimeout)
	defer cancel()

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
	indicatorRepo := indicator.NewPgRepository(pool)
	var fundAddrs []string
	for _, a := range domain.AccountRegistry() {
		fundAddrs = append(fundAddrs, a.Address)
	}
	metricsSvc := metrics.NewService(horizonClient, priceSvc, indicatorRepo, fundAddrs)
	snapshotSvc := snapshot.NewService(fundSvc, snapshotRepo, metricsSvc)

	if _, err := snapshotRepo.EnsureEntity(ctx, "mtlf", "Montelibero Fund", "Montelibero Fund statistics"); err != nil {
		return fmt.Errorf("ensuring entity: %w", err)
	}

	now := time.Now().UTC()
	date := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	stage := startStage("snapshot_generate")
	data, err := snapshotSvc.Generate(ctx, "mtlf", date)
	if err != nil {
		return fmt.Errorf("generating snapshot: %w", err)
	}
	stage.done("date", date.Format("2006-01-02"))

	hist := &indicator.HistoricalData{Repo: snapshotRepo, Slug: "mtlf"}
	indicatorSvc := indicator.NewService(hist)

	stage = startStage("indicator_calculate")
	indicators, err := indicatorSvc.CalculateAll(ctx, data)
	if err != nil {
		return fmt.Errorf("calculating indicators: %w", err)
	}
	stage.done("count", len(indicators))

	entityID, err := snapshotRepo.GetEntityID(ctx, "mtlf")
	if err != nil {
		return fmt.Errorf("getting entity id for indicator persistence: %w", err)
	}

	stage = startStage("indicator_persist")
	if err := indicatorRepo.Save(ctx, entityID, date, indicators); err != nil {
		return fmt.Errorf("persisting indicators: %w", err)
	}
	stage.done("count", len(indicators), "date", date.Format("2006-01-02"))

	if cfg.GoogleSheetsSpreadsheetID != "" && cfg.GoogleCredentialsJSON != "" {
		sheetsWriter, err := export.NewSheetsWriter(ctx, cfg.GoogleSheetsSpreadsheetID, cfg.GoogleCredentialsJSON)
		if err != nil {
			return fmt.Errorf("initializing Google Sheets writer: %w", err)
		}
		exportSvc := export.NewService(indicatorRepo, sheetsWriter)

		stage = startStage("sheets_export_indall")
		rows, err := exportSvc.Export(ctx, indicators)
		if err != nil {
			return fmt.Errorf("exporting to Google Sheets: %w", err)
		}
		stage.done()

		stage = startStage("sheets_append_monitoring")
		if err := sheetsWriter.AppendMonitoring(ctx, rows); err != nil {
			return fmt.Errorf("appending MONITORING row: %w", err)
		}
		stage.done()
	}

	return nil
}

// stageTimer captures the duration of a discrete report stage and emits an
// info-level summary on done(). Used to spot which step blew past its budget.
type stageTimer struct {
	name  string
	start time.Time
}

func startStage(name string) stageTimer {
	slog.Info("stage started", "name", name)
	return stageTimer{name: name, start: time.Now()}
}

func (s stageTimer) done(extra ...any) {
	args := []any{"name", s.name, "duration_ms", time.Since(s.start).Milliseconds()}
	args = append(args, extra...)
	slog.Info("stage completed", args...)
}

func runImport(c *cli.Context) error {
	ctx := c.Context
	cfg := config.Load()
	apiURL := c.String("api-url")

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

	snapshotRepo := snapshot.NewPgRepository(pool)
	indicatorRepo := indicator.NewPgRepository(pool)
	entityID, err := snapshotRepo.EnsureEntity(ctx, "mtlf", "Montelibero Fund", "Montelibero Fund statistics")
	if err != nil {
		return fmt.Errorf("ensuring entity: %w", err)
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}

	// Fetch snapshot date list from old API.
	dates, err := fetchOldSnapshots(ctx, httpClient, apiURL)
	if err != nil {
		return fmt.Errorf("fetching snapshot list: %w", err)
	}
	slog.Info("fetched snapshot list", "count", len(dates))

	const maxConsecutiveErrors = 5

	var imported, skipped, consecutiveErrors int
	for _, d := range dates {
		date := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)

		// Skip dates that already have a snapshot.
		_, err := snapshotRepo.GetByDate(ctx, "mtlf", date)
		if err == nil {
			slog.Info("skipping existing snapshot", "date", date.Format("2006-01-02"))
			skipped++
			consecutiveErrors = 0
			continue
		}
		if !errors.Is(err, snapshot.ErrNotFound) {
			consecutiveErrors++
			slog.Error("checking existing snapshot", "date", date.Format("2006-01-02"), "error", err)
			if consecutiveErrors >= maxConsecutiveErrors {
				return fmt.Errorf("aborting import after %d consecutive errors, last: %w", consecutiveErrors, err)
			}
			continue
		}

		data, err := fetchAndTransform(ctx, httpClient, apiURL, d)
		if err != nil {
			consecutiveErrors++
			slog.Error("failed to import snapshot", "date", date.Format("2006-01-02"), "error", err)
			if consecutiveErrors >= maxConsecutiveErrors {
				return fmt.Errorf("aborting import after %d consecutive errors, last: %w", consecutiveErrors, err)
			}
			continue
		}

		if err := snapshotRepo.Save(ctx, entityID, date, data); err != nil {
			consecutiveErrors++
			slog.Error("failed to save snapshot", "date", date.Format("2006-01-02"), "error", err)
			if consecutiveErrors >= maxConsecutiveErrors {
				return fmt.Errorf("aborting import after %d consecutive errors, last: %w", consecutiveErrors, err)
			}
			continue
		}

		consecutiveErrors = 0
		imported++
		slog.Info("imported snapshot", "date", date.Format("2006-01-02"))
	}

	slog.Info("import complete", "imported", imported, "skipped", skipped, "errors", len(dates)-imported-skipped)

	// Export to Google Sheets if configured.
	if cfg.GoogleSheetsSpreadsheetID == "" || cfg.GoogleCredentialsJSON == "" {
		slog.Info("Google Sheets not configured, skipping export")
		return nil
	}

	hist := &indicator.HistoricalData{Repo: snapshotRepo, Slug: "mtlf"}

	sheetsWriter, err := export.NewSheetsWriter(ctx, cfg.GoogleSheetsSpreadsheetID, cfg.GoogleCredentialsJSON)
	if err != nil {
		return fmt.Errorf("initializing Google Sheets writer: %w", err)
	}

	indicatorSvc := indicator.NewService(hist)

	// IDs that produce correct values from snapshot data alone. Layer0 (I51-I53,
	// I56, I58-I61) reads only account balances/prices stored in the snapshot.
	// Layer1 I3 (Assets Value) and I4 (Operating Balance) depend on Layer0 outputs.
	// Other Layer1+ indicators (I5-I7, I10, etc.) require live_metrics — for legacy
	// snapshots without that block they resolve to zero and we omit them.
	snapshotOnlyIDs := map[int]bool{
		3: true, 4: true,
		51: true, 52: true, 53: true, 56: true, 58: true, 59: true, 60: true, 61: true,
	}

	// Delete existing MONITORING sheet so the bulk import starts clean.
	if err := sheetsWriter.DeleteMonitoringSheet(ctx); err != nil {
		return fmt.Errorf("deleting MONITORING sheet: %w", err)
	}

	// Append MONITORING rows for all dates (oldest first).
	sortedDates := make([]time.Time, len(dates))
	copy(sortedDates, dates)
	sort.Slice(sortedDates, func(i, j int) bool { return sortedDates[i].Before(sortedDates[j]) })

	for _, d := range sortedDates {
		date := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)

		snap, err := snapshotRepo.GetByDate(ctx, "mtlf", date)
		if err != nil {
			slog.Debug("monitoring: snapshot not found", "date", date.Format("2006-01-02"), "error", err)
			continue
		}

		var fundData domain.FundStructureData
		if err := json.Unmarshal(snap.Data, &fundData); err != nil {
			slog.Error("monitoring: failed to unmarshal snapshot", "date", date.Format("2006-01-02"), "error", err)
			continue
		}

		// Snapshots with live_metrics get the full indicator set; legacy snapshots
		// without it get only the snapshot-computable subset (Layer0 + I3/I4).
		hasLiveMetrics := fundData.LiveMetrics != nil
		indicators, err := indicatorSvc.CalculateAll(ctx, fundData)
		if err != nil {
			slog.Error("monitoring: failed to calculate indicators", "date", date.Format("2006-01-02"), "error", err)
			continue
		}

		var rows []export.IndicatorRow
		if hasLiveMetrics {
			rows = lo.Map(indicators, func(ind indicator.Indicator, _ int) export.IndicatorRow {
				return export.IndicatorRow{Indicator: ind}
			})
		} else {
			rows = lo.FilterMap(indicators, func(ind indicator.Indicator, _ int) (export.IndicatorRow, bool) {
				if !snapshotOnlyIDs[ind.ID] {
					return export.IndicatorRow{}, false
				}
				return export.IndicatorRow{Indicator: ind}, true
			})
		}

		if err := sheetsWriter.AppendMonitoringRowOnly(ctx, rows, date); err != nil {
			slog.Error("monitoring: failed to append row", "date", date.Format("2006-01-02"), "error", err)
			continue
		}

		slog.Info("appended MONITORING row", "date", date.Format("2006-01-02"), "full", hasLiveMetrics)

		// Respect Google Sheets API rate limits (60 read requests/min).
		time.Sleep(3 * time.Second)
	}

	// Apply MONITORING formatting once after all rows are written.
	if err := sheetsWriter.ApplyMonitoringFormatting(ctx); err != nil {
		slog.Error("failed to apply MONITORING formatting", "error", err)
	}

	// Update IND_ALL / IND_MAIN with current data.
	exportSvc := export.NewService(indicatorRepo, sheetsWriter)

	latestSnap, err := snapshotRepo.GetLatest(ctx, "mtlf")
	if err != nil {
		return fmt.Errorf("getting latest snapshot for export: %w", err)
	}

	var latestData domain.FundStructureData
	if err := json.Unmarshal(latestSnap.Data, &latestData); err != nil {
		return fmt.Errorf("unmarshaling latest snapshot: %w", err)
	}

	latestIndicators, err := indicatorSvc.CalculateAll(ctx, latestData)
	if err != nil {
		return fmt.Errorf("calculating latest indicators for export: %w", err)
	}

	if _, err := exportSvc.Export(ctx, latestIndicators); err != nil {
		return fmt.Errorf("exporting to Google Sheets: %w", err)
	}
	slog.Info("Google Sheets IND_ALL/IND_MAIN export completed")

	return nil
}

// oldSnapshotEntry matches the old API's /api/snapshots response.
type oldSnapshotEntry struct {
	Date      time.Time `json:"date"`
	CreatedAt time.Time `json:"createdAt"`
}

// oldFundStructure matches the old API's /api/fund-structure response.
type oldFundStructure struct {
	Accounts      []domain.FundAccountPortfolio `json:"accounts"`
	OtherAccounts []domain.FundAccountPortfolio `json:"otherAccounts"`
}

// maxResponseBody limits HTTP response reads to 50 MB.
const maxResponseBody = 50 << 20

func fetchOldSnapshots(ctx context.Context, client *http.Client, apiURL string) ([]time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+"/api/snapshots", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching snapshots: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("unexpected status %d from /api/snapshots: %s", resp.StatusCode, snippet)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var entries []oldSnapshotEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("parsing snapshots: %w", err)
	}

	return lo.Map(entries, func(e oldSnapshotEntry, _ int) time.Time {
		return e.Date
	}), nil
}

func fetchAndTransform(ctx context.Context, client *http.Client, apiURL string, date time.Time) (json.RawMessage, error) {
	url := fmt.Sprintf("%s/api/fund-structure?date=%s", apiURL, date.UTC().Format(time.RFC3339))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching fund structure: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("unexpected status %d for date %s: %s", resp.StatusCode, date.Format("2006-01-02"), snippet)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var old oldFundStructure
	if err := json.Unmarshal(body, &old); err != nil {
		return nil, fmt.Errorf("parsing fund structure: %w", err)
	}

	// Normalize account names to match current system.
	for i := range old.Accounts {
		if old.Accounts[i].Name == "CITY" {
			old.Accounts[i].Name = "MCITY"
		}
	}

	// Split old "accounts" into main (issuer/subfond/operational) and mutual.
	mainAccounts := lo.Filter(old.Accounts, func(a domain.FundAccountPortfolio, _ int) bool {
		return a.Type != domain.AccountTypeMutual
	})
	mutualFunds := lo.Filter(old.Accounts, func(a domain.FundAccountPortfolio, _ int) bool {
		return a.Type == domain.AccountTypeMutual
	})

	// Compute aggregated totals from main accounts only.
	totalEURMTL := lo.Reduce(mainAccounts, func(acc decimal.Decimal, a domain.FundAccountPortfolio, _ int) decimal.Decimal {
		return acc.Add(a.TotalEURMTL)
	}, decimal.Zero)
	totalXLM := lo.Reduce(mainAccounts, func(acc decimal.Decimal, a domain.FundAccountPortfolio, _ int) decimal.Decimal {
		return acc.Add(a.TotalXLM)
	}, decimal.Zero)
	tokenCount := lo.Reduce(mainAccounts, func(acc int, a domain.FundAccountPortfolio, _ int) int {
		return acc + len(a.Tokens)
	}, 0)

	newData := domain.FundStructureData{
		Accounts:      mainAccounts,
		MutualFunds:   mutualFunds,
		OtherAccounts: old.OtherAccounts,
		AggregatedTotals: domain.AggregatedTotals{
			TotalEURMTL:  totalEURMTL,
			TotalXLM:     totalXLM,
			AccountCount: len(mainAccounts),
			TokenCount:   tokenCount,
		},
	}

	result, err := json.Marshal(newData)
	if err != nil {
		return nil, fmt.Errorf("marshaling transformed data: %w", err)
	}

	return result, nil
}

func runImportExcel(c *cli.Context) error {
	ctx := c.Context
	cfg := config.Load()
	filePath := c.String("file")

	// Read the Excel MONITORING tab.
	excelRows, lastExcelDate, err := readExcelMonitoring(filePath)
	if err != nil {
		return fmt.Errorf("reading Excel file: %w", err)
	}
	slog.Info("read Excel MONITORING data", "rows", len(excelRows)-2, "lastDate", lastExcelDate.Format("2006-01-02"))

	if cfg.GoogleSheetsSpreadsheetID == "" || cfg.GoogleCredentialsJSON == "" {
		return fmt.Errorf("GOOGLE_SHEETS_SPREADSHEET_ID and GOOGLE_CREDENTIALS_JSON are required")
	}

	sheetsWriter, err := export.NewSheetsWriter(ctx, cfg.GoogleSheetsSpreadsheetID, cfg.GoogleCredentialsJSON)
	if err != nil {
		return fmt.Errorf("initializing Google Sheets writer: %w", err)
	}

	// Delete existing MONITORING sheet for clean rebuild.
	if err := sheetsWriter.DeleteMonitoringSheet(ctx); err != nil {
		return fmt.Errorf("deleting MONITORING sheet: %w", err)
	}

	// Bulk-write all Excel rows (headers + data) at once.
	if err := sheetsWriter.WriteMonitoringBulk(ctx, excelRows); err != nil {
		return fmt.Errorf("writing Excel data to MONITORING: %w", err)
	}
	slog.Info("wrote Excel MONITORING data to Google Sheets")

	// Append DB snapshots for dates after the last Excel date.
	if cfg.DatabaseURL == "" {
		slog.Info("DATABASE_URL not set, skipping DB snapshot append")
		if err := sheetsWriter.ApplyMonitoringFormatting(ctx); err != nil {
			return fmt.Errorf("applying MONITORING formatting: %w", err)
		}
		return nil
	}

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

	if err := database.RunMigrations(ctx, pool, migrations.FS); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	snapshotRepo := snapshot.NewPgRepository(pool)
	indicatorRepo := indicator.NewPgRepository(pool)
	hist := &indicator.HistoricalData{Repo: snapshotRepo, Slug: "mtlf"}
	fullIndicatorSvc := indicator.NewService(hist)

	// Iterate day by day from lastExcelDate+1 to today.
	const maxConsecutiveErrors = 5

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	var appended, consecutiveErrors int

	for d := lastExcelDate.AddDate(0, 0, 1); !d.After(today); d = d.AddDate(0, 0, 1) {
		snap, err := snapshotRepo.GetByDate(ctx, "mtlf", d)
		if err != nil {
			if errors.Is(err, snapshot.ErrNotFound) {
				slog.Debug("no snapshot for date", "date", d.Format("2006-01-02"))
				continue
			}
			consecutiveErrors++
			slog.Error("database error fetching snapshot", "date", d.Format("2006-01-02"), "error", err)
			if consecutiveErrors >= maxConsecutiveErrors {
				return fmt.Errorf("aborting after %d consecutive errors, last: %w", consecutiveErrors, err)
			}
			continue
		}
		consecutiveErrors = 0

		var fundData domain.FundStructureData
		if err := json.Unmarshal(snap.Data, &fundData); err != nil {
			slog.Error("failed to unmarshal snapshot", "date", d.Format("2006-01-02"), "error", err)
			continue
		}

		indicators, err := fullIndicatorSvc.CalculateAll(ctx, fundData)
		if err != nil {
			slog.Error("failed to calculate indicators", "date", d.Format("2006-01-02"), "error", err)
			continue
		}

		rows := lo.Map(indicators, func(ind indicator.Indicator, _ int) export.IndicatorRow {
			return export.IndicatorRow{Indicator: ind}
		})

		if err := sheetsWriter.AppendMonitoringRowOnly(ctx, rows, d); err != nil {
			slog.Error("failed to append MONITORING row", "date", d.Format("2006-01-02"), "error", err)
			continue
		}

		appended++
		slog.Info("appended MONITORING row from DB", "date", d.Format("2006-01-02"))
		time.Sleep(3 * time.Second)
	}

	slog.Info("DB snapshot append complete", "appended", appended)

	// Apply MONITORING formatting.
	if err := sheetsWriter.ApplyMonitoringFormatting(ctx); err != nil {
		return fmt.Errorf("applying MONITORING formatting: %w", err)
	}

	// Refresh IND_ALL / IND_MAIN with latest snapshot.
	if _, err := snapshotRepo.EnsureEntity(ctx, "mtlf", "Montelibero Fund", "Montelibero Fund statistics"); err != nil {
		return fmt.Errorf("ensuring entity: %w", err)
	}

	latestSnap, err := snapshotRepo.GetLatest(ctx, "mtlf")
	if err != nil {
		if errors.Is(err, snapshot.ErrNotFound) {
			slog.Info("no snapshots in database, skipping IND_ALL/IND_MAIN refresh")
			return nil
		}
		return fmt.Errorf("getting latest snapshot for IND_ALL/IND_MAIN refresh: %w", err)
	}

	var latestData domain.FundStructureData
	if err := json.Unmarshal(latestSnap.Data, &latestData); err != nil {
		return fmt.Errorf("unmarshaling latest snapshot: %w", err)
	}

	latestIndicators, err := fullIndicatorSvc.CalculateAll(ctx, latestData)
	if err != nil {
		return fmt.Errorf("calculating latest indicators for export: %w", err)
	}

	exportSvc := export.NewService(indicatorRepo, sheetsWriter)
	monHist := buildMonitoringHistory(excelRows)
	if _, err := exportSvc.ExportWithHistory(ctx, latestIndicators, monHist); err != nil {
		return fmt.Errorf("exporting to Google Sheets: %w", err)
	}
	slog.Info("Google Sheets IND_ALL/IND_MAIN export completed")

	return nil
}

// readExcelMonitoring reads the MONITORING sheet from an Excel file and returns
// all rows (2 header rows + data rows) as [][]any, plus the last data date.
func readExcelMonitoring(filePath string) ([][]any, time.Time, error) {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	xlRows, err := f.GetRows("MONITORING")
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("reading MONITORING sheet: %w", err)
	}

	if len(xlRows) < 3 {
		return nil, time.Time{}, fmt.Errorf("MONITORING sheet has fewer than 3 rows")
	}

	const totalCols = 41 // A through AO

	var allRows [][]any
	var lastDate time.Time
	var suppressedErrors int

	for rowIdx, xlRow := range xlRows {
		row := make([]any, totalCols)
		for colIdx := range totalCols {
			if colIdx >= len(xlRow) || xlRow[colIdx] == "" {
				row[colIdx] = nil
				continue
			}
			cellVal := xlRow[colIdx]

			if rowIdx >= 2 && colIdx == 0 {
				// Date column: parse and format as dd.mm.yyyy for Google Sheets.
				t, err := parseExcelDate(cellVal)
				if err != nil {
					slog.Debug("skipping row with unparseable date", "row", rowIdx+1, "value", cellVal, "error", err)
					row = nil
					break
				}
				row[0] = t.Format("02.01.2006")
				lastDate = t
			} else if rowIdx >= 2 && colIdx > 0 {
				// Data cells: convert to float where possible, suppress Excel errors.
				val := parseExcelNumber(cellVal)
				if val == nil {
					suppressedErrors++
				}
				row[colIdx] = val
			} else {
				// Header rows: convert to float where possible, keep as string otherwise.
				row[colIdx] = parseExcelNumber(cellVal)
			}
		}
		if row != nil {
			allRows = append(allRows, row)
		}
	}

	if suppressedErrors > 0 {
		slog.Info("suppressed Excel error values during import",
			"count", suppressedErrors,
			"explanation", "cells with # prefixes (e.g. #REF!, #DIV/0!) replaced with nil",
		)
	}

	if lastDate.IsZero() {
		return nil, time.Time{}, fmt.Errorf("no valid dates found in MONITORING sheet")
	}

	return allRows, lastDate, nil
}

// parseExcelNumber tries to convert a string to a float64, stripping commas
// from formatted numbers. Excel error values (#REF!, #DIV/0!, #N/A, etc.)
// are replaced with nil to avoid Google Sheets interpreting them as errors.
// Returns the original string if not a number and not an error.
func parseExcelNumber(s string) any {
	if strings.HasPrefix(s, "#") {
		return nil
	}
	cleaned := strings.ReplaceAll(s, ",", "")
	d, err := decimal.NewFromString(cleaned)
	if err != nil {
		return s
	}
	f, _ := d.Float64()
	return f
}

// parseExcelDate parses date strings in formats used by excelize output.
func parseExcelDate(s string) (time.Time, error) {
	for _, layout := range []string{
		"02.01.2006",     // dd.mm.yyyy — known MONITORING format
		"2006-01-02",     // ISO
		"2006-01-02T15:04:05Z",
		"01-02-06",       // MM-DD-YY
		"1/2/06",         // US short
		"1/2/2006",       // US long
		"01-02-2006",     // MM-DD-YYYY
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse date: %q", s)
}

// buildMonitoringHistory converts Excel MONITORING rows into an export.MonitoringHistory
// for use by ExportWithHistory to fill historical change gaps.
func buildMonitoringHistory(excelRows [][]any) export.MonitoringHistory {
	colIDs := export.MonitoringColumnIndicatorIDs()
	hist := make(export.MonitoringHistory, len(excelRows))
	var skippedNoDate, skippedParseFail, skippedNoVals int

	for i, row := range excelRows {
		if i < 2 || len(row) == 0 {
			continue // skip header rows and empty rows
		}
		dateStr, ok := row[0].(string)
		if !ok {
			skippedNoDate++
			continue
		}
		t, err := time.Parse("02.01.2006", dateStr)
		if err != nil {
			skippedParseFail++
			continue
		}
		date := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		vals := make(map[int]decimal.Decimal)
		for j := 0; j < len(colIDs) && j+1 < len(row); j++ {
			if colIDs[j] == 0 {
				continue
			}
			if f, ok := row[j+1].(float64); ok {
				vals[colIDs[j]] = decimal.NewFromFloat(f)
			}
		}
		if len(vals) > 0 {
			hist[date] = vals
		} else {
			skippedNoVals++
		}
	}

	if skipped := skippedNoDate + skippedParseFail + skippedNoVals; skipped > 0 {
		slog.Info("buildMonitoringHistory: skipped rows",
			"parsed", len(hist),
			"skippedNoDate", skippedNoDate,
			"skippedParseFail", skippedParseFail,
			"skippedNoVals", skippedNoVals,
		)
	}

	return hist
}

// runImportIndicatorsFromSheets reads the MONITORING tab from the configured Google Sheet
// and upserts each (date, indicator_id, value) row into fund_indicators. Used to seed
// historical indicator values that pre-date snapshot persistence — values for indicator
// IDs that aren't in the MONITORING column mapping (e.g. I49) are silently absent.
func runImportIndicatorsFromSheets(c *cli.Context) error {
	ctx := c.Context
	cfg := config.Load()

	if cfg.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.GoogleSheetsSpreadsheetID == "" || cfg.GoogleCredentialsJSON == "" {
		return fmt.Errorf("GOOGLE_SHEETS_SPREADSHEET_ID and GOOGLE_CREDENTIALS_JSON are required")
	}

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

	if err := database.RunMigrations(ctx, pool, migrations.FS); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	snapshotRepo := snapshot.NewPgRepository(pool)
	indicatorRepo := indicator.NewPgRepository(pool)

	entityID, err := snapshotRepo.EnsureEntity(ctx, "mtlf", "Montelibero Fund", "Montelibero Fund statistics")
	if err != nil {
		return fmt.Errorf("ensuring entity: %w", err)
	}

	sheetsWriter, err := export.NewSheetsWriter(ctx, cfg.GoogleSheetsSpreadsheetID, cfg.GoogleCredentialsJSON)
	if err != nil {
		return fmt.Errorf("initializing Google Sheets client: %w", err)
	}

	rows, err := sheetsWriter.ReadMonitoring(ctx)
	if err != nil {
		return fmt.Errorf("reading MONITORING sheet: %w", err)
	}
	if len(rows) < 3 {
		return fmt.Errorf("MONITORING sheet has fewer than 3 rows (got %d)", len(rows))
	}

	colIDs := export.MonitoringColumnIndicatorIDs()

	const maxConsecutiveErrors = 5
	var processed, consecutive int
	var skippedEmpty, skippedBadDate, skippedNoIndicators, suppressedCells int

	for i, row := range rows {
		if i < 2 {
			continue // header rows
		}
		if len(row) == 0 {
			skippedEmpty++
			continue
		}

		dateStr, ok := row[0].(string)
		if !ok || dateStr == "" {
			skippedEmpty++
			continue
		}
		date, err := parseSheetDate(dateStr)
		if err != nil {
			slog.Debug("skipping row with unparseable date", "rowIndex", i+1, "value", dateStr, "error", err)
			skippedBadDate++
			continue
		}

		var inds []indicator.Indicator
		for j := 0; j < len(colIDs) && j+1 < len(row); j++ {
			id := colIDs[j]
			if id == 0 {
				continue
			}
			val, outcome := parseSheetNumber(row[j+1])
			if outcome == sheetCellParseFailed {
				suppressedCells++
				continue
			}
			if outcome != sheetCellOK {
				continue
			}
			inds = append(inds, indicator.Indicator{ID: id, Value: val})
		}

		if len(inds) == 0 {
			skippedNoIndicators++
			continue
		}

		if err := indicatorRepo.Save(ctx, entityID, date, inds); err != nil {
			consecutive++
			slog.Error("failed to save indicators", "date", date.Format("2006-01-02"), "error", err)
			if consecutive >= maxConsecutiveErrors {
				return fmt.Errorf("aborting after %d consecutive save errors, last: %w", consecutive, err)
			}
			continue
		}

		consecutive = 0
		processed++
		if processed%50 == 0 {
			slog.Info("import progress", "processed", processed)
		}
	}

	if suppressedCells > 0 {
		slog.Info("suppressed cells with unparseable values during import",
			"count", suppressedCells,
			"explanation", "non-numeric, non-error strings or unhandled types — values dropped, not zero-filled",
		)
	}

	slog.Info("MONITORING indicator import complete",
		"processed", processed,
		"skippedEmpty", skippedEmpty,
		"skippedBadDate", skippedBadDate,
		"skippedNoIndicators", skippedNoIndicators,
		"suppressedCells", suppressedCells,
		"total_rows", len(rows)-2,
	)
	return nil
}

// parseSheetDate parses dd.mm.yyyy and d.m.yyyy (both seen in MONITORING column A),
// plus ISO and US fallbacks. Returns midnight UTC.
func parseSheetDate(s string) (time.Time, error) {
	for _, layout := range []string{"02.01.2006", "2.1.2006", "2006-01-02", "1/2/2006", "01-02-2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse date %q", s)
}

// sheetCellOutcome distinguishes the three possible results of parsing a MONITORING cell.
type sheetCellOutcome int

const (
	// sheetCellOK: parsed to a valid decimal value.
	sheetCellOK sheetCellOutcome = iota
	// sheetCellEmpty: cell empty or Excel error string (#REF!, #DIV/0!, …) — silently skipped.
	sheetCellEmpty
	// sheetCellParseFailed: cell had non-empty content that didn't parse as a number — surfaced as suppressedCells.
	sheetCellParseFailed
)

// parseSheetNumber accepts float64 (UNFORMATTED_VALUE) or numeric strings.
// Excel error strings and empty cells return sheetCellEmpty.
// Non-empty unparseable content returns sheetCellParseFailed so the caller can count it.
func parseSheetNumber(v any) (decimal.Decimal, sheetCellOutcome) {
	switch x := v.(type) {
	case float64:
		return decimal.NewFromFloat(x), sheetCellOK
	case int64:
		return decimal.NewFromInt(x), sheetCellOK
	case string:
		if x == "" || strings.HasPrefix(x, "#") {
			return decimal.Zero, sheetCellEmpty
		}
		cleaned := strings.ReplaceAll(x, ",", "")
		d, err := decimal.NewFromString(cleaned)
		if err != nil {
			return decimal.Zero, sheetCellParseFailed
		}
		return d, sheetCellOK
	case nil:
		return decimal.Zero, sheetCellEmpty
	}
	return decimal.Zero, sheetCellParseFailed
}

// runBackfillIndicators recomputes deterministic indicators for every existing snapshot
// and writes them to fund_indicators. Indicators excluded from indicator.DeterministicIDs
// (live tokenomics, dividend chain, MTLRECT live price) are skipped — past values for
// those are unrecoverable and remain absent until the next daily `stat report` run.
func runBackfillIndicators(c *cli.Context) error {
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

	snapshotRepo := snapshot.NewPgRepository(pool)
	indicatorRepo := indicator.NewPgRepository(pool)

	entityID, err := snapshotRepo.EnsureEntity(ctx, "mtlf", "Montelibero Fund", "Montelibero Fund statistics")
	if err != nil {
		return fmt.Errorf("ensuring entity: %w", err)
	}

	metas, err := snapshotRepo.ListMeta(ctx, "mtlf")
	if err != nil {
		return fmt.Errorf("listing snapshot metadata: %w", err)
	}

	// Iterate oldest-first so partial progress is sequential.
	sort.Slice(metas, func(i, j int) bool { return metas[i].SnapshotDate.Before(metas[j].SnapshotDate) })

	// Indicators read live values from snapshot.LiveMetrics. Non-deterministic
	// indicators that lack stored values resolve to zero and are filtered via
	// DeterministicIDs below.
	indicatorSvc := indicator.NewService(nil)

	const maxConsecutiveErrors = 5
	var processed, failed, consecutive int

	for i, m := range metas {
		date := time.Date(m.SnapshotDate.Year(), m.SnapshotDate.Month(), m.SnapshotDate.Day(), 0, 0, 0, 0, time.UTC)

		snap, err := snapshotRepo.GetByDate(ctx, "mtlf", date)
		if err != nil {
			if errors.Is(err, snapshot.ErrNotFound) {
				// Snapshot vanished between ListMeta and now — skip without counting.
				continue
			}
			failed++
			consecutive++
			slog.Error("backfill: load snapshot", "date", date.Format("2006-01-02"), "error", err)
			if consecutive >= maxConsecutiveErrors {
				return fmt.Errorf("aborting after %d consecutive errors, last: %w", consecutive, err)
			}
			continue
		}

		var fundData domain.FundStructureData
		if err := json.Unmarshal(snap.Data, &fundData); err != nil {
			failed++
			consecutive++
			slog.Error("backfill: parse snapshot", "date", date.Format("2006-01-02"), "error", err)
			if consecutive >= maxConsecutiveErrors {
				return fmt.Errorf("aborting after %d consecutive parse errors, last: %w", consecutive, err)
			}
			continue
		}

		all, err := indicatorSvc.CalculateAll(ctx, fundData)
		if err != nil {
			failed++
			consecutive++
			slog.Error("backfill: calculate indicators", "date", date.Format("2006-01-02"), "error", err)
			if consecutive >= maxConsecutiveErrors {
				return fmt.Errorf("aborting after %d consecutive calc errors, last: %w", consecutive, err)
			}
			continue
		}

		deterministic := lo.Filter(all, func(ind indicator.Indicator, _ int) bool {
			return indicator.DeterministicIDs[ind.ID]
		})

		if err := indicatorRepo.Save(ctx, entityID, date, deterministic); err != nil {
			failed++
			consecutive++
			slog.Error("backfill: persist indicators", "date", date.Format("2006-01-02"), "error", err)
			if consecutive >= maxConsecutiveErrors {
				return fmt.Errorf("aborting after %d consecutive save errors, last: %w", consecutive, err)
			}
			continue
		}

		consecutive = 0
		processed++
		if (i+1)%50 == 0 {
			slog.Info("backfill progress", "processed", processed, "failed", failed, "total", len(metas))
		}
	}

	slog.Info("backfill complete", "processed", processed, "failed", failed, "total", len(metas))
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

	snapshotRepo := snapshot.NewPgRepository(pool)
	indicatorRepo := indicator.NewPgRepository(pool)

	// The serve path is read-only: no fund generation, no Horizon. Pass nil for the
	// FundStructureService — Service.Generate is never invoked here.
	snapshotSvc := snapshot.NewService(nil, snapshotRepo)

	if _, err := snapshotRepo.EnsureEntity(ctx, "mtlf", "Montelibero Fund", "Montelibero Fund statistics"); err != nil {
		return fmt.Errorf("ensuring entity: %w", err)
	}

	srv := api.NewServer(cfg.HTTPPort, snapshotSvc, indicatorRepo)

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
