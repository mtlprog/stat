package export

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/indicator"
)

func TestBuildMonitoringRows(t *testing.T) {
	at := time.Date(2026, 2, 24, 12, 0, 0, 0, time.UTC)

	rows := []IndicatorRow{
		{Indicator: indicator.Indicator{ID: 1, Value: decimal.NewFromFloat(4020758.507)}},
		{Indicator: indicator.Indicator{ID: 2, Value: decimal.NewFromFloat(35.69)}},
		{Indicator: indicator.Indicator{ID: 3, Value: decimal.NewFromFloat(2033307.288)}},
		{Indicator: indicator.Indicator{ID: 5, Value: decimal.NewFromFloat(639256.44)}},
		{Indicator: indicator.Indicator{ID: 10, Value: decimal.NewFromFloat(6.29)}},
		{Indicator: indicator.Indicator{ID: 11, Value: decimal.NewFromFloat(4434.70)}},
		{Indicator: indicator.Indicator{ID: 39, Value: decimal.NewFromInt(24000)}},
		{Indicator: indicator.Indicator{ID: 43, Value: decimal.NewFromFloat(12.34)}},
		{Indicator: indicator.Indicator{ID: 61, Value: decimal.NewFromInt(95000)}},
		{Indicator: indicator.Indicator{ID: 62, Value: decimal.NewFromFloat(310.0)}},
	}

	headerRows, dataRow := buildMonitoringRows(rows, at)

	// Check header structure
	if len(headerRows) != 2 {
		t.Fatalf("expected 2 header rows, got %d", len(headerRows))
	}

	colNumRow := headerRows[0]
	headerRow := headerRows[1]

	// 54 columns: Date + 53 data columns
	if len(colNumRow) != 54 {
		t.Errorf("col num row: expected 54 columns, got %d", len(colNumRow))
	}
	if len(headerRow) != 54 {
		t.Errorf("header row: expected 54 columns, got %d", len(headerRow))
	}
	if len(dataRow) != 54 {
		t.Errorf("data row: expected 54 columns, got %d", len(dataRow))
	}

	// Row 1: column A is blank, mapped slots show indicator ID, placeholders
	// fall back to position number.
	if colNumRow[0] != "" {
		t.Errorf("col num row[0]: expected empty, got %v", colNumRow[0])
	}
	if colNumRow[1] != 1.0 {
		t.Errorf("col num row[1]: expected 1.0 (I1), got %v", colNumRow[1])
	}
	if colNumRow[9] != 9.0 {
		t.Errorf("col num row[9]: expected 9.0 (placeholder position fallback), got %v", colNumRow[9])
	}
	if colNumRow[25] != 26.0 {
		t.Errorf("col num row[25]: expected 26.0 (I26 cumulative), got %v", colNumRow[25])
	}
	if colNumRow[26] != 25.0 {
		t.Errorf("col num row[26]: expected 25.0 (I25 daily), got %v", colNumRow[26])
	}
	if colNumRow[41] != 62.0 {
		t.Errorf("col num row[41]: expected 62.0 (I62 Shareholders), got %v", colNumRow[41])
	}
	if colNumRow[42] != 43.0 {
		t.Errorf("col num row[42]: expected 43.0 (I43 Total ROI), got %v", colNumRow[42])
	}
	if colNumRow[53] != 61.0 {
		t.Errorf("col num row[53]: expected 61.0 (I61 BTC Rate), got %v", colNumRow[53])
	}

	// Row 2: header names
	if headerRow[0] != "Date" {
		t.Errorf("header row[0]: expected 'Date', got %v", headerRow[0])
	}
	if headerRow[1] != "Market Cap EUR" {
		t.Errorf("header row[1]: expected 'Market Cap EUR', got %v", headerRow[1])
	}
	if headerRow[41] != "Shareholders" {
		t.Errorf("header row[41]: expected 'Shareholders', got %v", headerRow[41])
	}

	// Date column
	if dataRow[0] != "24.02.2026" {
		t.Errorf("data row date: expected '24.02.2026', got %v", dataRow[0])
	}

	// Mapped indicator I1 (Market Cap EUR) at index 1
	if v, ok := dataRow[1].(float64); !ok || v != 4020758.507 {
		t.Errorf("data row I1: expected 4020758.507, got %v", dataRow[1])
	}

	// Mapped indicator I2 (Market Cap BTC) at index 2
	if v, ok := dataRow[2].(float64); !ok || v != 35.69 {
		t.Errorf("data row I2: expected 35.69, got %v", dataRow[2])
	}

	// Regulatory Price (index 9) — fixed value 4.0
	if v, ok := dataRow[9].(float64); !ok || v != 4.0 {
		t.Errorf("data row Regulatory Price: expected 4.0, got %v", dataRow[9])
	}

	// Dividends in btcmtl (index 13) — unmapped, fixedValue nil
	if dataRow[13] != nil {
		t.Errorf("data row Dividends in btcmtl: expected nil, got %v", dataRow[13])
	}

	// Dividends (index 11) and Dividends in eurmtl (index 12) — both map to I11
	if v, ok := dataRow[11].(float64); !ok || v != 4434.70 {
		t.Errorf("data row Dividends: expected 4434.70, got %v", dataRow[11])
	}
	if v, ok := dataRow[12].(float64); !ok || v != 4434.70 {
		t.Errorf("data row Dividends in eurmtl: expected 4434.70, got %v", dataRow[12])
	}

	// Missing mapped indicator I4 (Operating Balance, index 4) — should be nil
	if dataRow[4] != nil {
		t.Errorf("data row missing I4: expected nil, got %v (%T)", dataRow[4], dataRow[4])
	}

	// BPP slot (index 39) — mapped to I39, manually-managed constant
	if v, ok := dataRow[39].(float64); !ok || v != 24000.0 {
		t.Errorf("data row BPP/I39: expected 24000.0, got %v", dataRow[39])
	}

	// MTLAP slot (index 40) — mapped to I40 but no I40 row supplied in this fixture, expect nil
	if dataRow[40] != nil {
		t.Errorf("data row MTLAP (I40 missing from fixture): expected nil, got %v", dataRow[40])
	}

	// Shareholders / I62 (index 41) — legacy last column, stays frozen at 41
	if v, ok := dataRow[41].(float64); !ok || v != 310.0 {
		t.Errorf("data row Shareholders: expected 310.0, got %v", dataRow[41])
	}

	// I43 Total ROI (index 42) — first of the appended new columns
	if headerRow[42] != "Total ROI" {
		t.Errorf("header row[42]: expected 'Total ROI', got %v", headerRow[42])
	}
	if v, ok := dataRow[42].(float64); !ok || v != 12.34 {
		t.Errorf("data row I43: expected 12.34, got %v", dataRow[42])
	}

	// I61 BTC Rate (index 53) — last column
	if headerRow[53] != "BTC Rate" {
		t.Errorf("header row[53]: expected 'BTC Rate', got %v", headerRow[53])
	}
	if v, ok := dataRow[53].(float64); !ok || v != 95000.0 {
		t.Errorf("data row I61: expected 95000.0, got %v", dataRow[53])
	}
}

func TestMonitoringColumnCount(t *testing.T) {
	if len(monitoringColumns) != 53 {
		t.Errorf("expected 53 monitoring columns, got %d", len(monitoringColumns))
	}
}
