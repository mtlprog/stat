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
		{Indicator: indicator.Indicator{ID: 40, Value: decimal.NewFromFloat(205.0)}},
	}

	headerRows, dataRow := buildMonitoringRows(rows, at)

	// Check header structure
	if len(headerRows) != 2 {
		t.Fatalf("expected 2 header rows, got %d", len(headerRows))
	}

	colNumRow := headerRows[0]
	headerRow := headerRows[1]

	// 41 columns: Date + 40 data columns
	if len(colNumRow) != 41 {
		t.Errorf("col num row: expected 41 columns, got %d", len(colNumRow))
	}
	if len(headerRow) != 41 {
		t.Errorf("header row: expected 41 columns, got %d", len(headerRow))
	}
	if len(dataRow) != 41 {
		t.Errorf("data row: expected 41 columns, got %d", len(dataRow))
	}

	// Row 1: column A is blank, then 1.0..40.0
	if colNumRow[0] != "" {
		t.Errorf("col num row[0]: expected empty, got %v", colNumRow[0])
	}
	if colNumRow[1] != 1.0 {
		t.Errorf("col num row[1]: expected 1.0, got %v", colNumRow[1])
	}
	if colNumRow[40] != 40.0 {
		t.Errorf("col num row[40]: expected 40.0, got %v", colNumRow[40])
	}

	// Row 2: header names
	if headerRow[0] != "Date" {
		t.Errorf("header row[0]: expected 'Date', got %v", headerRow[0])
	}
	if headerRow[1] != "Market Cap EUR" {
		t.Errorf("header row[1]: expected 'Market Cap EUR', got %v", headerRow[1])
	}
	if headerRow[40] != "MTLAP" {
		t.Errorf("header row[40]: expected 'MTLAP', got %v", headerRow[40])
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

	// Missing mapped indicator I4 (Operating Balance, index 4) — should be 0.0
	if v, ok := dataRow[4].(float64); !ok || v != 0.0 {
		t.Errorf("data row missing I4: expected 0.0, got %v (%T)", dataRow[4], dataRow[4])
	}

	// MTLAP (index 40) — last column
	if v, ok := dataRow[40].(float64); !ok || v != 205.0 {
		t.Errorf("data row MTLAP: expected 205.0, got %v", dataRow[40])
	}
}

func TestMonitoringColumnCount(t *testing.T) {
	if len(monitoringColumns) != 40 {
		t.Errorf("expected 40 monitoring columns, got %d", len(monitoringColumns))
	}
}
