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

// Write clears and rewrites the IND_ALL and IND_MAIN sheets.
func (w *SheetsWriter) Write(ctx context.Context, rows []IndicatorRow) error {
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
