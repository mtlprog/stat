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
		rows, err := exportSvc.Export(ctx, data)
		if err != nil {
			return fmt.Errorf("exporting to Google Sheets: %w", err)
		}
		slog.Info("Google Sheets IND_ALL/IND_MAIN export completed")

		if err := sheetsWriter.AppendMonitoring(ctx, rows); err != nil {
			return fmt.Errorf("appending MONITORING row: %w", err)
		}
		slog.Info("Google Sheets MONITORING row appended")
	}

	return nil
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

	horizonClient := horizon.NewClient(cfg.HorizonURL, cfg.HorizonRetryMax, cfg.HorizonRetryBaseDelay)
	priceSvc := price.NewService(horizonClient)
	hist := &indicator.HistoricalData{Repo: snapshotRepo, Slug: "mtlf"}

	sheetsWriter, err := export.NewSheetsWriter(ctx, cfg.GoogleSheetsSpreadsheetID, cfg.GoogleCredentialsJSON)
	if err != nil {
		return fmt.Errorf("initializing Google Sheets writer: %w", err)
	}

	// Two indicator services: partial (nil Horizon) for old snapshots,
	// full (with Horizon) for snapshots that have live_metrics.
	partialIndicatorSvc := indicator.NewService(nil, nil, hist)
	fullIndicatorSvc := indicator.NewService(priceSvc, horizonClient, hist)

	// IDs that produce correct values from snapshot data alone, even when Horizon
	// is nil. Layer0 (I51-I53, I56-I61) reads only account balances/prices stored
	// in the snapshot. Layer1 I3 (Assets Value) and I4 (Operating Balance) depend
	// on Layer0 outputs, not on Horizon. Other Layer1+ indicators (I5-I7, I10, etc.)
	// require Horizon for circulation/dividend data and will be zero — excluded here.
	snapshotOnlyIDs := map[int]bool{
		3: true, 4: true,
		51: true, 52: true, 53: true, 56: true, 57: true, 58: true, 59: true, 60: true, 61: true,
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
			slog.Warn("monitoring: snapshot not found", "date", date.Format("2006-01-02"), "error", err)
			continue
		}

		var fundData domain.FundStructureData
		if err := json.Unmarshal(snap.Data, &fundData); err != nil {
			slog.Warn("monitoring: failed to unmarshal snapshot", "date", date.Format("2006-01-02"), "error", err)
			continue
		}

		// Snapshots with live_metrics get full indicator calculation;
		// old snapshots without it get only snapshot-computable indicators.
		hasLiveMetrics := fundData.LiveMetrics != nil
		var rows []export.IndicatorRow

		if hasLiveMetrics {
			indicators, err := fullIndicatorSvc.CalculateAll(ctx, fundData)
			if err != nil {
				slog.Warn("monitoring: failed to calculate indicators", "date", date.Format("2006-01-02"), "error", err)
				continue
			}
			rows = lo.Map(indicators, func(ind indicator.Indicator, _ int) export.IndicatorRow {
				return export.IndicatorRow{Indicator: ind}
			})
		} else {
			indicators, err := partialIndicatorSvc.CalculateAll(ctx, fundData)
			if err != nil {
				slog.Warn("monitoring: failed to calculate indicators", "date", date.Format("2006-01-02"), "error", err)
				continue
			}
			rows = lo.FilterMap(indicators, func(ind indicator.Indicator, _ int) (export.IndicatorRow, bool) {
				if !snapshotOnlyIDs[ind.ID] {
					return export.IndicatorRow{}, false
				}
				return export.IndicatorRow{Indicator: ind}, true
			})
		}

		if err := sheetsWriter.AppendMonitoringRowOnly(ctx, rows, date); err != nil {
			slog.Warn("monitoring: failed to append row", "date", date.Format("2006-01-02"), "error", err)
			continue
		}

		slog.Info("appended MONITORING row", "date", date.Format("2006-01-02"), "full", hasLiveMetrics)

		// Respect Google Sheets API rate limits (60 read requests/min).
		time.Sleep(3 * time.Second)
	}

	// Apply MONITORING formatting once after all rows are written.
	if err := sheetsWriter.ApplyMonitoringFormatting(ctx); err != nil {
		slog.Warn("failed to apply MONITORING formatting", "error", err)
	}

	// Update IND_ALL / IND_MAIN with current data.
	indicatorSvc := fullIndicatorSvc
	exportSvc := export.NewService(indicatorSvc, snapshotRepo, sheetsWriter)

	latestSnap, err := snapshotRepo.GetLatest(ctx, "mtlf")
	if err != nil {
		return fmt.Errorf("getting latest snapshot for export: %w", err)
	}

	var latestData domain.FundStructureData
	if err := json.Unmarshal(latestSnap.Data, &latestData); err != nil {
		return fmt.Errorf("unmarshaling latest snapshot: %w", err)
	}

	if _, err := exportSvc.Export(ctx, latestData); err != nil {
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
	horizonClient := horizon.NewClient(cfg.HorizonURL, cfg.HorizonRetryMax, cfg.HorizonRetryBaseDelay)
	priceSvc := price.NewService(horizonClient)
	hist := &indicator.HistoricalData{Repo: snapshotRepo, Slug: "mtlf"}
	fullIndicatorSvc := indicator.NewService(priceSvc, horizonClient, hist)

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
			slog.Warn("database error fetching snapshot", "date", d.Format("2006-01-02"), "error", err)
			if consecutiveErrors >= maxConsecutiveErrors {
				return fmt.Errorf("aborting after %d consecutive errors, last: %w", consecutiveErrors, err)
			}
			continue
		}
		consecutiveErrors = 0

		var fundData domain.FundStructureData
		if err := json.Unmarshal(snap.Data, &fundData); err != nil {
			slog.Warn("failed to unmarshal snapshot", "date", d.Format("2006-01-02"), "error", err)
			continue
		}

		indicators, err := fullIndicatorSvc.CalculateAll(ctx, fundData)
		if err != nil {
			slog.Warn("failed to calculate indicators", "date", d.Format("2006-01-02"), "error", err)
			continue
		}

		rows := lo.Map(indicators, func(ind indicator.Indicator, _ int) export.IndicatorRow {
			return export.IndicatorRow{Indicator: ind}
		})

		if err := sheetsWriter.AppendMonitoringRowOnly(ctx, rows, d); err != nil {
			slog.Warn("failed to append MONITORING row", "date", d.Format("2006-01-02"), "error", err)
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

	exportSvc := export.NewService(fullIndicatorSvc, snapshotRepo, sheetsWriter)
	monHist := buildMonitoringHistory(excelRows)
	if _, err := exportSvc.ExportWithHistory(ctx, latestData, monHist); err != nil {
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
					slog.Warn("skipping row with unparseable date", "row", rowIdx+1, "value", cellVal, "error", err)
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
		slog.Warn("suppressed Excel error values during import",
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
		slog.Warn("buildMonitoringHistory: skipped rows",
			"parsed", len(hist),
			"skippedNoDate", skippedNoDate,
			"skippedParseFail", skippedParseFail,
			"skippedNoVals", skippedNoVals,
		)
	}

	return hist
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
