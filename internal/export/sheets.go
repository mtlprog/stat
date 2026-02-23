package export

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	sheets "google.golang.org/api/sheets/v4"
)

// SheetsWriter implements SheetWriter using the Google Sheets API.
type SheetsWriter struct {
	spreadsheetID string
	svc           *sheets.Service
}

// NewSheetsWriter creates a SheetsWriter authenticated with a service account JSON.
func NewSheetsWriter(ctx context.Context, spreadsheetID, credentialsJSON string) (*SheetsWriter, error) {
	creds, err := google.CredentialsFromJSON(
		ctx,
		[]byte(credentialsJSON),
		sheets.SpreadsheetsScope,
	)
	if err != nil {
		return nil, fmt.Errorf("parsing google credentials: %w", err)
	}

	svc, err := sheets.NewService(ctx, option.WithCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("creating sheets service: %w", err)
	}

	return &SheetsWriter{spreadsheetID: spreadsheetID, svc: svc}, nil
}

// sheetMeta holds the sheet ID and IDs of any existing banded ranges.
type sheetMeta struct {
	id         int64
	bandingIDs []int64
}

// Write ensures required sheets exist, then clears, rewrites, and formats them.
func (w *SheetsWriter) Write(ctx context.Context, rows []IndicatorRow) error {
	meta, err := w.ensureSheets(ctx, "IND_ALL", "IND_MAIN")
	if err != nil {
		return err
	}

	now := time.Now()
	indAllValues := buildIndAll(rows)
	indMainValues := buildIndMain(rows, now)

	mainCount := 0
	for _, r := range rows {
		if r.IsMain {
			mainCount++
		}
	}

	_, err = w.svc.Spreadsheets.Values.BatchClear(
		w.spreadsheetID,
		&sheets.BatchClearValuesRequest{
			Ranges: []string{"IND_ALL!A:L", "IND_MAIN!A:G"},
		},
	).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("clearing sheets: %w", err)
	}

	_, err = w.svc.Spreadsheets.Values.BatchUpdate(
		w.spreadsheetID,
		&sheets.BatchUpdateValuesRequest{
			ValueInputOption: "USER_ENTERED",
			Data: []*sheets.ValueRange{
				{Range: "IND_ALL!A1", Values: indAllValues},
				{Range: "IND_MAIN!A1", Values: indMainValues},
			},
		},
	).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("writing sheets: %w", err)
	}

	if err := w.applyFormatting(ctx, meta["IND_ALL"], meta["IND_MAIN"], len(rows), mainCount); err != nil {
		return fmt.Errorf("applying formatting: %w", err)
	}

	return nil
}

// buildIndAll builds the IND_ALL sheet data.
// Columns: N | Name | Code | Value | measure | Week | Month | Quarter | Year | Descr | Formula | MAIN
func buildIndAll(rows []IndicatorRow) [][]any {
	data := make([][]any, 0, len(rows)+1)
	data = append(data, []any{
		"N", "Name", "Code", "Value", "measure",
		"Week", "Month", "Quarter", "Year",
		"Descr", "Formula", "MAIN",
	})

	for _, row := range rows {
		mainVal := 0
		if row.IsMain {
			mainVal = 1
		}
		data = append(data, []any{
			row.ID, row.Name, "",
			toFloat(row.Value), row.Unit,
			ptrFloat(row.WeekChange),
			ptrFloat(row.MonthChange),
			ptrFloat(row.QuarterChange),
			ptrFloat(row.YearChange),
			"", "", mainVal,
		})
	}

	return data
}

// buildIndMain builds the IND_MAIN sheet data (only MAIN indicators).
// Row 1: date stamp. Row 2: headers. Row 3+: data.
// Columns: Name | Value | measure | Week | Month | Quarter | Year
func buildIndMain(rows []IndicatorRow, at time.Time) [][]any {
	data := [][]any{
		{"", at.UTC().Format("02.01.2006 15:04:05")},
		{"Name", "Value", "measure", "Week", "Month", "Quarter", "Year"},
	}

	for _, row := range rows {
		if !row.IsMain {
			continue
		}
		data = append(data, []any{
			row.Name,
			toFloat(row.Value),
			row.Unit,
			ptrFloat(row.WeekChange),
			ptrFloat(row.MonthChange),
			ptrFloat(row.QuarterChange),
			ptrFloat(row.YearChange),
		})
	}

	return data
}

// ensureSheets creates any missing sheets and returns metadata (ID, banding IDs) for each.
func (w *SheetsWriter) ensureSheets(ctx context.Context, names ...string) (map[string]sheetMeta, error) {
	spreadsheet, err := w.svc.Spreadsheets.Get(w.spreadsheetID).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("getting spreadsheet metadata: %w", err)
	}

	result := make(map[string]sheetMeta, len(names))
	existing := make(map[string]sheetMeta, len(spreadsheet.Sheets))
	for _, s := range spreadsheet.Sheets {
		m := sheetMeta{id: s.Properties.SheetId}
		for _, b := range s.BandedRanges {
			m.bandingIDs = append(m.bandingIDs, b.BandedRangeId)
		}
		existing[s.Properties.Title] = m
	}

	var requests []*sheets.Request
	for _, name := range names {
		if m, ok := existing[name]; ok {
			result[name] = m
		} else {
			requests = append(requests, &sheets.Request{
				AddSheet: &sheets.AddSheetRequest{
					Properties: &sheets.SheetProperties{Title: name},
				},
			})
		}
	}

	if len(requests) == 0 {
		return result, nil
	}

	resp, err := w.svc.Spreadsheets.BatchUpdate(
		w.spreadsheetID,
		&sheets.BatchUpdateSpreadsheetRequest{Requests: requests},
	).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("creating sheets: %w", err)
	}

	for _, reply := range resp.Replies {
		if reply.AddSheet != nil {
			p := reply.AddSheet.Properties
			result[p.Title] = sheetMeta{id: p.SheetId}
		}
	}

	return result, nil
}

// applyFormatting applies visual formatting to both sheets via a single BatchUpdate.
func (w *SheetsWriter) applyFormatting(ctx context.Context, indAll, indMain sheetMeta, allCount, mainCount int) error {
	navyBlue := &sheets.Color{Red: 0.122, Green: 0.220, Blue: 0.392}
	white := &sheets.Color{Red: 1, Green: 1, Blue: 1}
	lightBlue := &sheets.Color{Red: 0.898, Green: 0.929, Blue: 0.992}

	var reqs []*sheets.Request

	// ---- IND_ALL ----

	// Header row (row 0): navy background, bold white text
	reqs = append(reqs, cellFormatReq(indAll.id, 0, 1, 0, 12,
		&sheets.CellFormat{
			BackgroundColor: navyBlue,
			TextFormat:      &sheets.TextFormat{Bold: true, ForegroundColor: white},
		},
		"userEnteredFormat(backgroundColor,textFormat)"))

	// Freeze header row
	reqs = append(reqs, freezeRowsReq(indAll.id, 1))

	// Value column D (col 3): number format
	reqs = append(reqs, cellFormatReq(indAll.id, 1, int64(allCount+1), 3, 4,
		&sheets.CellFormat{NumberFormat: &sheets.NumberFormat{Type: "NUMBER", Pattern: "#,##0.00"}},
		"userEnteredFormat.numberFormat"))

	// Change columns F–I (cols 5–8): percent format
	reqs = append(reqs, cellFormatReq(indAll.id, 1, int64(allCount+1), 5, 9,
		&sheets.CellFormat{NumberFormat: &sheets.NumberFormat{Type: "PERCENT", Pattern: "0.00%"}},
		"userEnteredFormat.numberFormat"))

	// Delete existing bandings, then add fresh banded rows
	for _, bid := range indAll.bandingIDs {
		reqs = append(reqs, &sheets.Request{
			DeleteBanding: &sheets.DeleteBandingRequest{BandedRangeId: bid},
		})
	}
	reqs = append(reqs, bandingReq(indAll.id, 1, 0, 12, white, lightBlue))

	// Autosize all columns
	reqs = append(reqs, autosizeColsReq(indAll.id, 0, 12))

	// ---- IND_MAIN ----

	// Header row (row 1, after the date row): navy background, bold white text
	reqs = append(reqs, cellFormatReq(indMain.id, 1, 2, 0, 7,
		&sheets.CellFormat{
			BackgroundColor: navyBlue,
			TextFormat:      &sheets.TextFormat{Bold: true, ForegroundColor: white},
		},
		"userEnteredFormat(backgroundColor,textFormat)"))

	// Freeze date + header rows
	reqs = append(reqs, freezeRowsReq(indMain.id, 2))

	// Value column B (col 1): number format
	reqs = append(reqs, cellFormatReq(indMain.id, 2, int64(mainCount+2), 1, 2,
		&sheets.CellFormat{NumberFormat: &sheets.NumberFormat{Type: "NUMBER", Pattern: "#,##0.00"}},
		"userEnteredFormat.numberFormat"))

	// Change columns D–G (cols 3–6): percent format
	reqs = append(reqs, cellFormatReq(indMain.id, 2, int64(mainCount+2), 3, 7,
		&sheets.CellFormat{NumberFormat: &sheets.NumberFormat{Type: "PERCENT", Pattern: "0.00%"}},
		"userEnteredFormat.numberFormat"))

	// Delete existing bandings, then add fresh banded rows
	for _, bid := range indMain.bandingIDs {
		reqs = append(reqs, &sheets.Request{
			DeleteBanding: &sheets.DeleteBandingRequest{BandedRangeId: bid},
		})
	}
	reqs = append(reqs, bandingReq(indMain.id, 2, 0, 7, white, lightBlue))

	// Autosize all columns
	reqs = append(reqs, autosizeColsReq(indMain.id, 0, 7))

	_, err := w.svc.Spreadsheets.BatchUpdate(
		w.spreadsheetID,
		&sheets.BatchUpdateSpreadsheetRequest{Requests: reqs},
	).Context(ctx).Do()
	return err
}

// cellFormatReq builds a RepeatCellRequest for a rectangular range.
func cellFormatReq(sheetID, startRow, endRow, startCol, endCol int64, format *sheets.CellFormat, fields string) *sheets.Request {
	return &sheets.Request{
		RepeatCell: &sheets.RepeatCellRequest{
			Range: &sheets.GridRange{
				SheetId:          sheetID,
				StartRowIndex:    startRow,
				EndRowIndex:      endRow,
				StartColumnIndex: startCol,
				EndColumnIndex:   endCol,
			},
			Cell:   &sheets.CellData{UserEnteredFormat: format},
			Fields: fields,
		},
	}
}

// freezeRowsReq builds an UpdateSheetPropertiesRequest to freeze the first n rows.
func freezeRowsReq(sheetID, n int64) *sheets.Request {
	return &sheets.Request{
		UpdateSheetProperties: &sheets.UpdateSheetPropertiesRequest{
			Properties: &sheets.SheetProperties{
				SheetId:        sheetID,
				GridProperties: &sheets.GridProperties{FrozenRowCount: n},
			},
			Fields: "gridProperties.frozenRowCount",
		},
	}
}

// bandingReq builds an AddBandingRequest for alternating row colors starting at startRow.
func bandingReq(sheetID, startRow, startCol, endCol int64, first, second *sheets.Color) *sheets.Request {
	return &sheets.Request{
		AddBanding: &sheets.AddBandingRequest{
			BandedRange: &sheets.BandedRange{
				Range: &sheets.GridRange{
					SheetId:          sheetID,
					StartRowIndex:    startRow,
					StartColumnIndex: startCol,
					EndColumnIndex:   endCol,
				},
				RowProperties: &sheets.BandingProperties{
					FirstBandColor:  first,
					SecondBandColor: second,
				},
			},
		},
	}
}

// autosizeColsReq builds an AutoResizeDimensionsRequest for columns startCol..endCol.
func autosizeColsReq(sheetID, startCol, endCol int64) *sheets.Request {
	return &sheets.Request{
		AutoResizeDimensions: &sheets.AutoResizeDimensionsRequest{
			Dimensions: &sheets.DimensionRange{
				SheetId:    sheetID,
				Dimension:  "COLUMNS",
				StartIndex: startCol,
				EndIndex:   endCol,
			},
		},
	}
}

func toFloat(d decimal.Decimal) float64 {
	f, _ := d.Float64()
	return f
}

func ptrFloat(d *decimal.Decimal) any {
	if d == nil {
		return nil
	}
	f, _ := d.Float64()
	return f
}
