package export

import (
	"context"
	"fmt"
	"time"

	"github.com/samber/lo"
	sheets "google.golang.org/api/sheets/v4"
)

// monitoringCol describes one column in the MONITORING sheet.
// indicatorID == 0 means no mapped indicator; use fixedValue instead.
type monitoringCol struct {
	header      string
	indicatorID int
	fixedValue  any
}

// monitoringColumns defines the 40 data columns (B through AO) in order.
// Column A (Date) is prepended separately in buildMonitoringRows.
var monitoringColumns = []monitoringCol{
	{header: "Market Cap EUR", indicatorID: 1},
	{header: "Market Cap BTC", indicatorID: 2},
	{header: "Total Balance", indicatorID: 3},
	{header: "Operating Balance", indicatorID: 4},
	{header: "Shares", indicatorID: 5},
	{header: "MTL  in circulation", indicatorID: 6},
	{header: "MTLRECT in circulation", indicatorID: 7},
	{header: "Book Value", indicatorID: 8},
	{header: "Regulatory Price", indicatorID: 0, fixedValue: 4.0},
	{header: "Share Market Price", indicatorID: 10},
	{header: "Dividends", indicatorID: 11},
	{header: "Dividends in eurmtl", indicatorID: 11}, // same as above; only EURMTL dividends tracked currently
	{header: "Dividends in btcmtl", indicatorID: 0, fixedValue: nil},
	{header: "Dividends in usdm", indicatorID: 0, fixedValue: nil},
	{header: "Dividends per share", indicatorID: 15},
	{header: "Annual Dividend Yield 1", indicatorID: 16},
	{header: "Annual Dividend Yield 2", indicatorID: 17},
	{header: "Shareholders by eurmtl", indicatorID: 18},
	{header: "Shareholders by satsmtl", indicatorID: 0, fixedValue: nil},
	{header: "Shareholders by usdm", indicatorID: 0, fixedValue: nil},
	{header: "Average Shareholding", indicatorID: 21},
	{header: "Average Share Price", indicatorID: 22},
	{header: "Median shareholding size", indicatorID: 23},
	{header: "Tokenomics participants", indicatorID: 24},
	{header: "EURMTL overall payment per month", indicatorID: 26},
	{header: "EURMTL overall payment total", indicatorID: 25},
	{header: "More-one-share Shareholders ", indicatorID: 27},
	{header: "Montelibero Association Capitalization", indicatorID: 0, fixedValue: nil},
	{header: "Association Endowment Fund", indicatorID: 0, fixedValue: nil},
	{header: "Price-to-book ratio", indicatorID: 30},
	{header: "EBITDA", indicatorID: 0, fixedValue: nil},
	{header: "EBITDA margin", indicatorID: 0, fixedValue: nil},
	{header: "EPS", indicatorID: 33},
	{header: "P/E", indicatorID: 34},
	{header: "P/S", indicatorID: 0, fixedValue: nil},
	{header: "P/S (by cap)", indicatorID: 0, fixedValue: nil},
	{header: "Margin", indicatorID: 0, fixedValue: nil},
	{header: "Payout Ratio", indicatorID: 0, fixedValue: nil},
	{header: "BPP", indicatorID: 0, fixedValue: nil},
	{header: "MTLAP", indicatorID: 40},
}

// buildMonitoringRows builds header rows and a single data row for the MONITORING sheet.
func buildMonitoringRows(rows []IndicatorRow, at time.Time) (headerRows [][]any, dataRow []any) {
	byID := lo.KeyBy(rows, func(r IndicatorRow) int { return r.ID })

	// Row 1: column numbers 1..40 (A is blank)
	colNums := make([]any, 1+len(monitoringColumns))
	colNums[0] = ""
	for i := range monitoringColumns {
		colNums[i+1] = float64(i + 1)
	}

	// Row 2: header names
	headers := make([]any, 1+len(monitoringColumns))
	headers[0] = "Date"
	for i, col := range monitoringColumns {
		headers[i+1] = col.header
	}

	headerRows = [][]any{colNums, headers}

	// Data row
	data := make([]any, 1+len(monitoringColumns))
	data[0] = at.UTC().Format("02.01.2006")
	for i, col := range monitoringColumns {
		if col.indicatorID != 0 {
			if ind, ok := byID[col.indicatorID]; ok {
				data[i+1] = toFloat(ind.Value)
			} else {
				data[i+1] = float64(0)
			}
		} else {
			data[i+1] = col.fixedValue
		}
	}

	return headerRows, data
}

// AppendMonitoring ensures the MONITORING sheet exists, writes header rows if the sheet
// is new or empty, then appends one data row for the current run.
func (w *SheetsWriter) AppendMonitoring(ctx context.Context, rows []IndicatorRow) error {
	meta, err := w.ensureSheets(ctx, "MONITORING")
	if err != nil {
		return fmt.Errorf("ensuring MONITORING sheet: %w", err)
	}
	monMeta := meta["MONITORING"]

	now := time.Now().UTC()
	headerRows, dataRow := buildMonitoringRows(rows, now)

	existing, err := w.svc.Spreadsheets.Values.Get(
		w.spreadsheetID, "MONITORING!A1:A2",
	).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("reading MONITORING headers: %w", err)
	}

	if len(existing.Values) < 2 {
		_, err = w.svc.Spreadsheets.Values.Update(
			w.spreadsheetID,
			"MONITORING!A1",
			&sheets.ValueRange{Values: headerRows},
		).ValueInputOption("USER_ENTERED").Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("writing MONITORING headers: %w", err)
		}
	}

	_, err = w.svc.Spreadsheets.Values.Append(
		w.spreadsheetID,
		"MONITORING!A:AO",
		&sheets.ValueRange{Values: [][]any{dataRow}},
	).ValueInputOption("USER_ENTERED").InsertDataOption("INSERT_ROWS").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("appending MONITORING row: %w", err)
	}

	if err := w.applyMonitoringFormatting(ctx, monMeta); err != nil {
		return fmt.Errorf("formatting MONITORING sheet: %w", err)
	}

	return nil
}

// applyMonitoringFormatting applies visual formatting to the MONITORING sheet.
func (w *SheetsWriter) applyMonitoringFormatting(ctx context.Context, mon sheetMeta) error {
	navyBlue := &sheets.Color{Red: 0.122, Green: 0.220, Blue: 0.392}
	white := &sheets.Color{Red: 1, Green: 1, Blue: 1}
	lightBlue := &sheets.Color{Red: 0.898, Green: 0.929, Blue: 0.992}

	const totalCols = 41

	var reqs []*sheets.Request

	// Row 1 + Row 2: navy background, bold white text
	reqs = append(reqs, cellFormatReq(mon.id, 0, 2, 0, totalCols,
		&sheets.CellFormat{
			BackgroundColor: navyBlue,
			TextFormat:      &sheets.TextFormat{Bold: true, ForegroundColor: white},
		},
		"userEnteredFormat(backgroundColor,textFormat)"))

	// Freeze first two rows
	reqs = append(reqs, freezeRowsReq(mon.id, 2))

	// Number format for data columns B..AO (cols 1..40)
	reqs = append(reqs, cellFormatReq(mon.id, 2, 10000, 1, totalCols,
		&sheets.CellFormat{NumberFormat: &sheets.NumberFormat{Type: "NUMBER", Pattern: "#,##0.00"}},
		"userEnteredFormat.numberFormat"))

	// Banding
	for _, bid := range mon.bandingIDs {
		reqs = append(reqs, &sheets.Request{
			DeleteBanding: &sheets.DeleteBandingRequest{BandedRangeId: bid},
		})
	}
	reqs = append(reqs, bandingReq(mon.id, 2, 0, totalCols, white, lightBlue))

	// Autosize all columns
	reqs = append(reqs, autosizeColsReq(mon.id, 0, totalCols))

	_, err := w.svc.Spreadsheets.BatchUpdate(
		w.spreadsheetID,
		&sheets.BatchUpdateSpreadsheetRequest{Requests: reqs},
	).Context(ctx).Do()
	return err
}
