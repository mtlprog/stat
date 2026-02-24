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

// monitoringIntegerCols lists column indices (0-based) that use #,##0 format.
// B=1, D=3, E=4, F=5, G=6, H=7, L=11, M=12, V=21, W=22
var monitoringIntegerCols = []int{1, 3, 4, 5, 6, 7, 11, 12, 21, 22}

// applyMonitoringFormatting applies visual formatting to the MONITORING sheet,
// matching the original Excel layout: light-green headers, centered text,
// frozen column A + rows 1-2, narrow columns with vertical header text.
func (w *SheetsWriter) applyMonitoringFormatting(ctx context.Context, mon sheetMeta) error {
	// #D9EAD3 — light green from the original Excel
	lightGreen := &sheets.Color{Red: 0.851, Green: 0.918, Blue: 0.827}

	const totalCols = 41

	var reqs []*sheets.Request

	// Row 1 (column numbers): light-green background, centered, font size 10
	reqs = append(reqs, cellFormatReq(mon.id, 0, 1, 0, totalCols,
		&sheets.CellFormat{
			BackgroundColor:     lightGreen,
			TextFormat:          &sheets.TextFormat{FontSize: 10},
			HorizontalAlignment: "CENTER",
			VerticalAlignment:   "MIDDLE",
		},
		"userEnteredFormat(backgroundColor,textFormat,horizontalAlignment,verticalAlignment)"))

	// Row 2 (header names): light-green background, bold, font size 8, centered,
	// vertical text rotation to fit narrow columns (matching Excel's 75px row height)
	reqs = append(reqs, cellFormatReq(mon.id, 1, 2, 0, totalCols,
		&sheets.CellFormat{
			BackgroundColor: lightGreen,
			TextFormat:      &sheets.TextFormat{Bold: true, FontSize: 8},
			TextRotation:    &sheets.TextRotation{Angle: 90},
			HorizontalAlignment: "CENTER",
			VerticalAlignment:   "BOTTOM",
		},
		"userEnteredFormat(backgroundColor,textFormat,textRotation,horizontalAlignment,verticalAlignment)"))

	// Row 2 height: 150px to match vertical headers
	reqs = append(reqs, &sheets.Request{
		UpdateDimensionProperties: &sheets.UpdateDimensionPropertiesRequest{
			Range: &sheets.DimensionRange{
				SheetId:    mon.id,
				Dimension:  "ROWS",
				StartIndex: 1,
				EndIndex:   2,
			},
			Properties: &sheets.DimensionProperties{PixelSize: 150},
			Fields:     "pixelSize",
		},
	})

	// Freeze column A + rows 1-2 (B3 freeze pane like the Excel)
	reqs = append(reqs, &sheets.Request{
		UpdateSheetProperties: &sheets.UpdateSheetPropertiesRequest{
			Properties: &sheets.SheetProperties{
				SheetId: mon.id,
				GridProperties: &sheets.GridProperties{
					FrozenRowCount:    2,
					FrozenColumnCount: 1,
				},
			},
			Fields: "gridProperties.frozenRowCount,gridProperties.frozenColumnCount",
		},
	})

	// All data cells: centered text
	reqs = append(reqs, cellFormatReq(mon.id, 2, 10000, 0, totalCols,
		&sheets.CellFormat{HorizontalAlignment: "CENTER"},
		"userEnteredFormat.horizontalAlignment"))

	// Date column A: date format d.m.yyyy
	reqs = append(reqs, cellFormatReq(mon.id, 2, 10000, 0, 1,
		&sheets.CellFormat{NumberFormat: &sheets.NumberFormat{Type: "DATE", Pattern: "d.m.yyyy"}},
		"userEnteredFormat.numberFormat"))

	// Integer columns: #,##0 format (no decimal places)
	for _, col := range monitoringIntegerCols {
		reqs = append(reqs, cellFormatReq(mon.id, 2, 10000, int64(col), int64(col+1),
			&sheets.CellFormat{NumberFormat: &sheets.NumberFormat{Type: "NUMBER", Pattern: "#,##0"}},
			"userEnteredFormat.numberFormat"))
	}

	// Remove any existing banding (we don't use banding — matching Excel)
	for _, bid := range mon.bandingIDs {
		reqs = append(reqs, &sheets.Request{
			DeleteBanding: &sheets.DeleteBandingRequest{BandedRangeId: bid},
		})
	}

	// Set narrow column widths to match Excel's compact layout
	// Column A (date): 80px, data columns: 60px
	reqs = append(reqs, &sheets.Request{
		UpdateDimensionProperties: &sheets.UpdateDimensionPropertiesRequest{
			Range: &sheets.DimensionRange{
				SheetId:    mon.id,
				Dimension:  "COLUMNS",
				StartIndex: 0,
				EndIndex:   1,
			},
			Properties: &sheets.DimensionProperties{PixelSize: 80},
			Fields:     "pixelSize",
		},
	})
	reqs = append(reqs, &sheets.Request{
		UpdateDimensionProperties: &sheets.UpdateDimensionPropertiesRequest{
			Range: &sheets.DimensionRange{
				SheetId:    mon.id,
				Dimension:  "COLUMNS",
				StartIndex: 1,
				EndIndex:   int64(totalCols),
			},
			Properties: &sheets.DimensionProperties{PixelSize: 60},
			Fields:     "pixelSize",
		},
	})

	_, err := w.svc.Spreadsheets.BatchUpdate(
		w.spreadsheetID,
		&sheets.BatchUpdateSpreadsheetRequest{Requests: reqs},
	).Context(ctx).Do()
	return err
}
