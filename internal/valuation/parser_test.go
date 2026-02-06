package valuation

import (
	"testing"

	"github.com/mtlprog/stat/internal/domain"
)

func TestParseDataEntryValue(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantType  domain.ValuationValueType
		wantValue string
		wantSym   string
		wantQty   *float64
		wantUnit  string
		wantErr   bool
	}{
		{"simple integer", "100", domain.ValuationValueEURMTL, "100", "", nil, "", false},
		{"decimal", "3.14", domain.ValuationValueEURMTL, "3.14", "", nil, "", false},
		{"european comma", "0,8", domain.ValuationValueEURMTL, "0.8", "", nil, "", false},
		{"european thousands", "1.234,56", domain.ValuationValueEURMTL, "1234.56", "", nil, "", false},
		{"standard decimal", "1.5", domain.ValuationValueEURMTL, "1.5", "", nil, "", false},
		{"large number", "50000", domain.ValuationValueEURMTL, "50000", "", nil, "", false},
		{"BTC symbol", "BTC", domain.ValuationValueExternal, "", "BTC", nil, "", false},
		{"ETH symbol", "ETH", domain.ValuationValueExternal, "", "ETH", nil, "", false},
		{"XLM symbol", "XLM", domain.ValuationValueExternal, "", "XLM", nil, "", false},
		{"Sats symbol", "Sats", domain.ValuationValueExternal, "", "Sats", nil, "", false},
		{"USD symbol", "USD", domain.ValuationValueExternal, "", "USD", nil, "", false},
		{"AU symbol", "AU", domain.ValuationValueExternal, "", "AU", nil, "", false},
		{"compound AU grams", "AU 1g", domain.ValuationValueExternal, "", "AU", floatPtr(1), "g", false},
		{"compound AU oz", "AU 2.5oz", domain.ValuationValueExternal, "", "AU", floatPtr(2.5), "oz", false},
		{"zero rejected", "0", "", "", "", nil, "", true},
		{"negative rejected", "-5", "", "", "", nil, "", true},
		{"empty rejected", "", "", "", "", nil, "", true},
		{"invalid text", "hello", "", "", "", nil, "", true},
		{"unknown symbol compound", "FOO 1g", "", "", "", nil, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDataEntryValue(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", got.Type, tt.wantType)
			}
			if got.Value != tt.wantValue {
				t.Errorf("Value = %q, want %q", got.Value, tt.wantValue)
			}
			if got.Symbol != tt.wantSym {
				t.Errorf("Symbol = %q, want %q", got.Symbol, tt.wantSym)
			}
			if tt.wantQty != nil {
				if got.Quantity == nil {
					t.Fatal("Quantity is nil, want non-nil")
				}
				if *got.Quantity != *tt.wantQty {
					t.Errorf("Quantity = %v, want %v", *got.Quantity, *tt.wantQty)
				}
			}
			if got.Unit != tt.wantUnit {
				t.Errorf("Unit = %q, want %q", got.Unit, tt.wantUnit)
			}
		})
	}
}

func floatPtr(f float64) *float64 {
	return &f
}
