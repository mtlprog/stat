package export

import (
	"context"
	"fmt"

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

// Write ensures required sheets exist, then clears and rewrites them.
func (w *SheetsWriter) Write(ctx context.Context, rows []IndicatorRow) error {
	if err := w.ensureSheets(ctx, "IND_ALL", "IND_MAIN"); err != nil {
		return err
	}

	indAllValues := buildIndAll(rows)
	indMainValues := buildIndMain(rows)

	_, err := w.svc.Spreadsheets.Values.BatchClear(
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
// Columns: Name | Value | measure | Week | Month | Quarter | Year
func buildIndMain(rows []IndicatorRow) [][]any {
	data := [][]any{
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

// ensureSheets creates any of the named sheets that do not already exist.
func (w *SheetsWriter) ensureSheets(ctx context.Context, names ...string) error {
	spreadsheet, err := w.svc.Spreadsheets.Get(w.spreadsheetID).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("getting spreadsheet metadata: %w", err)
	}

	existing := make(map[string]bool, len(spreadsheet.Sheets))
	for _, s := range spreadsheet.Sheets {
		existing[s.Properties.Title] = true
	}

	var requests []*sheets.Request
	for _, name := range names {
		if !existing[name] {
			requests = append(requests, &sheets.Request{
				AddSheet: &sheets.AddSheetRequest{
					Properties: &sheets.SheetProperties{Title: name},
				},
			})
		}
	}

	if len(requests) == 0 {
		return nil
	}

	_, err = w.svc.Spreadsheets.BatchUpdate(
		w.spreadsheetID,
		&sheets.BatchUpdateSpreadsheetRequest{Requests: requests},
	).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("creating sheets: %w", err)
	}

	return nil
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
