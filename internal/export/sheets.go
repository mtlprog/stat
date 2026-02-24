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
// Styling matches the original MTL_report_1.xlsx layout.
func (w *SheetsWriter) applyFormatting(ctx context.Context, indAll, indMain sheetMeta, allCount, mainCount int) error {
	lightGreen := &sheets.Color{Red: 0.851, Green: 0.918, Blue: 0.827} // #D9EAD3
	lightYellow := &sheets.Color{Red: 1.0, Green: 0.898, Blue: 0.6}   // #FFE599
	lightGray := &sheets.Color{Red: 0.851, Green: 0.851, Blue: 0.851} // #D9D9D9

	var reqs []*sheets.Request

	// ---- IND_ALL ----
	allEnd := int64(allCount + 1)

	// Header row: light green background, bold Arial 10pt, centered
	reqs = append(reqs, cellFormatReq(indAll.id, 0, 1, 0, 12,
		&sheets.CellFormat{
			BackgroundColor:     lightGreen,
			TextFormat:          &sheets.TextFormat{Bold: true, FontSize: 10, FontFamily: "Arial"},
			HorizontalAlignment: "CENTER",
		},
		"userEnteredFormat(backgroundColor,textFormat,horizontalAlignment)"))

	// MAIN column L (col 11) header: gray background
	reqs = append(reqs, cellFormatReq(indAll.id, 0, 1, 11, 12,
		&sheets.CellFormat{BackgroundColor: lightGray},
		"userEnteredFormat.backgroundColor"))

	// Freeze 1 row + 12 columns (pane at M2)
	reqs = append(reqs, freezePaneReq(indAll.id, 1, 12))

	// Value column D (col 3): #,##0, bold
	reqs = append(reqs, cellFormatReq(indAll.id, 1, allEnd, 3, 4,
		&sheets.CellFormat{
			NumberFormat: &sheets.NumberFormat{Type: "NUMBER", Pattern: "#,##0"},
			TextFormat:   &sheets.TextFormat{Bold: true},
		},
		"userEnteredFormat(numberFormat,textFormat)"))

	// Code column C (col 2) and measure column E (col 4): centered
	reqs = append(reqs, cellFormatReq(indAll.id, 1, allEnd, 2, 3,
		&sheets.CellFormat{HorizontalAlignment: "CENTER"},
		"userEnteredFormat.horizontalAlignment"))
	reqs = append(reqs, cellFormatReq(indAll.id, 1, allEnd, 4, 5,
		&sheets.CellFormat{HorizontalAlignment: "CENTER"},
		"userEnteredFormat.horizontalAlignment"))

	// Change columns F–I (cols 5–8): 0.00%
	reqs = append(reqs, cellFormatReq(indAll.id, 1, allEnd, 5, 9,
		&sheets.CellFormat{NumberFormat: &sheets.NumberFormat{Type: "PERCENT", Pattern: "0.00%"}},
		"userEnteredFormat.numberFormat"))

	// Thin border: left on column F (col 5), right on column I (col 8)
	reqs = append(reqs, cellFormatReq(indAll.id, 0, allEnd, 5, 6,
		&sheets.CellFormat{
			Borders: &sheets.Borders{Left: &sheets.Border{Style: "SOLID", Color: &sheets.Color{}}},
		},
		"userEnteredFormat.borders.left"))
	reqs = append(reqs, cellFormatReq(indAll.id, 0, allEnd, 8, 9,
		&sheets.CellFormat{
			Borders: &sheets.Borders{Right: &sheets.Border{Style: "SOLID", Color: &sheets.Color{}}},
		},
		"userEnteredFormat.borders.right"))

	// Delete existing bandings (original has no banding)
	for _, bid := range indAll.bandingIDs {
		reqs = append(reqs, &sheets.Request{
			DeleteBanding: &sheets.DeleteBandingRequest{BandedRangeId: bid},
		})
	}

	// Column widths (pixels, converted from Excel character units × 8)
	for col, px := range map[int64]int64{
		0: 26, 1: 178, 2: 112, 3: 72, 4: 62, 5: 68, 6: 56, 7: 41, 8: 60, 9: 268, 10: 119, 11: 51,
	} {
		reqs = append(reqs, colWidthReq(indAll.id, col, px))
	}

	// ---- IND_MAIN ----
	mainEnd := int64(mainCount + 2)

	// Date row (row 0) + header row (row 1): light yellow, bold, v=center
	reqs = append(reqs, cellFormatReq(indMain.id, 0, 2, 0, 7,
		&sheets.CellFormat{
			BackgroundColor:   lightYellow,
			TextFormat:        &sheets.TextFormat{Bold: true, FontFamily: "Arial"},
			VerticalAlignment: "MIDDLE",
		},
		"userEnteredFormat(backgroundColor,textFormat,verticalAlignment)"))

	// Date + header alignment: cols A–B right-aligned, cols C–G centered
	reqs = append(reqs, cellFormatReq(indMain.id, 0, 2, 0, 2,
		&sheets.CellFormat{HorizontalAlignment: "RIGHT"},
		"userEnteredFormat.horizontalAlignment"))
	reqs = append(reqs, cellFormatReq(indMain.id, 0, 2, 2, 7,
		&sheets.CellFormat{HorizontalAlignment: "CENTER"},
		"userEnteredFormat.horizontalAlignment"))

	// Freeze 2 rows + 3 columns (pane at D3)
	reqs = append(reqs, freezePaneReq(indMain.id, 2, 3))

	// Data column A (col 0): right-aligned, v=center
	reqs = append(reqs, cellFormatReq(indMain.id, 2, mainEnd, 0, 1,
		&sheets.CellFormat{
			HorizontalAlignment: "RIGHT",
			VerticalAlignment:   "MIDDLE",
		},
		"userEnteredFormat(horizontalAlignment,verticalAlignment)"))

	// Data column B (col 1): font 12pt bold, v=center, #,##0.00
	reqs = append(reqs, cellFormatReq(indMain.id, 2, mainEnd, 1, 2,
		&sheets.CellFormat{
			TextFormat:        &sheets.TextFormat{Bold: true, FontSize: 12},
			VerticalAlignment: "MIDDLE",
			NumberFormat:      &sheets.NumberFormat{Type: "NUMBER", Pattern: "#,##0.00"},
		},
		"userEnteredFormat(textFormat,verticalAlignment,numberFormat)"))

	// Data column C (col 2): centered, v=center
	reqs = append(reqs, cellFormatReq(indMain.id, 2, mainEnd, 2, 3,
		&sheets.CellFormat{
			HorizontalAlignment: "CENTER",
			VerticalAlignment:   "MIDDLE",
		},
		"userEnteredFormat(horizontalAlignment,verticalAlignment)"))

	// Data columns D–E (cols 3–4): 0.00%
	reqs = append(reqs, cellFormatReq(indMain.id, 2, mainEnd, 3, 5,
		&sheets.CellFormat{NumberFormat: &sheets.NumberFormat{Type: "PERCENT", Pattern: "0.00%"}},
		"userEnteredFormat.numberFormat"))

	// Data columns F–G (cols 5–6): 0%
	reqs = append(reqs, cellFormatReq(indMain.id, 2, mainEnd, 5, 7,
		&sheets.CellFormat{NumberFormat: &sheets.NumberFormat{Type: "PERCENT", Pattern: "0%"}},
		"userEnteredFormat.numberFormat"))

	// Delete existing bandings (original has no banding)
	for _, bid := range indMain.bandingIDs {
		reqs = append(reqs, &sheets.Request{
			DeleteBanding: &sheets.DeleteBandingRequest{BandedRangeId: bid},
		})
	}

	// Column widths
	for col, px := range map[int64]int64{
		0: 240, 1: 106, 2: 76, 3: 81, 4: 68, 5: 58, 6: 50,
	} {
		reqs = append(reqs, colWidthReq(indMain.id, col, px))
	}

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

// freezePaneReq freezes the first rows and cols (pane at cell [rows, cols]).
func freezePaneReq(sheetID, rows, cols int64) *sheets.Request {
	return &sheets.Request{
		UpdateSheetProperties: &sheets.UpdateSheetPropertiesRequest{
			Properties: &sheets.SheetProperties{
				SheetId: sheetID,
				GridProperties: &sheets.GridProperties{
					FrozenRowCount:    rows,
					FrozenColumnCount: cols,
				},
			},
			Fields: "gridProperties.frozenRowCount,gridProperties.frozenColumnCount",
		},
	}
}

// colWidthReq sets the pixel width of a single column.
func colWidthReq(sheetID, col, pixels int64) *sheets.Request {
	return &sheets.Request{
		UpdateDimensionProperties: &sheets.UpdateDimensionPropertiesRequest{
			Range: &sheets.DimensionRange{
				SheetId:    sheetID,
				Dimension:  "COLUMNS",
				StartIndex: col,
				EndIndex:   col + 1,
			},
			Properties: &sheets.DimensionProperties{PixelSize: pixels},
			Fields:     "pixelSize",
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
