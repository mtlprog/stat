package valuation

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/horizon"
)

type mockAccountFetcher struct {
	accounts map[string]horizon.HorizonAccount
}

func (m *mockAccountFetcher) FetchAccount(_ context.Context, accountID string) (horizon.HorizonAccount, error) {
	if acc, ok := m.accounts[accountID]; ok {
		return acc, nil
	}
	return horizon.HorizonAccount{ID: accountID, Data: map[string]string{}}, nil
}

func TestScanAccountValuationsNumeric(t *testing.T) {
	fetcher := &mockAccountFetcher{
		accounts: map[string]horizon.HorizonAccount{
			"GTEST": {
				ID: "GTEST",
				Data: map[string]string{
					"AUMTL_1COST": base64.StdEncoding.EncodeToString([]byte("100")),
					"WBTC_COST":   base64.StdEncoding.EncodeToString([]byte("55000")),
				},
			},
		},
	}

	valuations, err := ScanAccountValuations(context.Background(), fetcher, "GTEST")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(valuations) != 2 {
		t.Fatalf("got %d valuations, want 2", len(valuations))
	}

	// Check AUMTL_1COST is parsed as unit type
	found := false
	for _, v := range valuations {
		if v.TokenCode == "AUMTL" && v.ValuationType == domain.ValuationTypeUnit {
			found = true
			if v.RawValue.Value != "100" {
				t.Errorf("AUMTL value = %q, want 100", v.RawValue.Value)
			}
		}
	}
	if !found {
		t.Error("AUMTL_1COST valuation not found")
	}
}

func TestScanAccountValuationsExternal(t *testing.T) {
	fetcher := &mockAccountFetcher{
		accounts: map[string]horizon.HorizonAccount{
			"GTEST": {
				ID: "GTEST",
				Data: map[string]string{
					"WBTC_1COST": base64.StdEncoding.EncodeToString([]byte("BTC")),
				},
			},
		},
	}

	valuations, err := ScanAccountValuations(context.Background(), fetcher, "GTEST")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(valuations) != 1 {
		t.Fatalf("got %d valuations, want 1", len(valuations))
	}

	if valuations[0].RawValue.Type != domain.ValuationValueExternal {
		t.Errorf("type = %q, want external", valuations[0].RawValue.Type)
	}
	if valuations[0].RawValue.Symbol != "BTC" {
		t.Errorf("symbol = %q, want BTC", valuations[0].RawValue.Symbol)
	}
}

func TestScanAccountValuationsCompound(t *testing.T) {
	fetcher := &mockAccountFetcher{
		accounts: map[string]horizon.HorizonAccount{
			"GTEST": {
				ID: "GTEST",
				Data: map[string]string{
					"AUMTL_1COST": base64.StdEncoding.EncodeToString([]byte("AU 1g")),
				},
			},
		},
	}

	valuations, err := ScanAccountValuations(context.Background(), fetcher, "GTEST")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(valuations) != 1 {
		t.Fatalf("got %d valuations, want 1", len(valuations))
	}

	v := valuations[0]
	if v.RawValue.Symbol != "AU" {
		t.Errorf("symbol = %q, want AU", v.RawValue.Symbol)
	}
	if v.RawValue.Quantity == nil || *v.RawValue.Quantity != 1.0 {
		t.Errorf("quantity = %v, want 1.0", v.RawValue.Quantity)
	}
}

func TestScanAccountValuationsSkipsNonCost(t *testing.T) {
	fetcher := &mockAccountFetcher{
		accounts: map[string]horizon.HorizonAccount{
			"GTEST": {
				ID: "GTEST",
				Data: map[string]string{
					"homepage":    base64.StdEncoding.EncodeToString([]byte("https://example.com")),
					"AUMTL_1COST": base64.StdEncoding.EncodeToString([]byte("100")),
				},
			},
		},
	}

	valuations, err := ScanAccountValuations(context.Background(), fetcher, "GTEST")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only AUMTL_1COST should be picked up, not homepage
	if len(valuations) != 1 {
		t.Errorf("got %d valuations, want 1 (only _COST entries)", len(valuations))
	}
}

func TestScanAccountValuationsInvalidBase64(t *testing.T) {
	fetcher := &mockAccountFetcher{
		accounts: map[string]horizon.HorizonAccount{
			"GTEST": {
				ID: "GTEST",
				Data: map[string]string{
					"TOKEN_1COST": "not-valid-base64!!!",
				},
			},
		},
	}

	valuations, err := ScanAccountValuations(context.Background(), fetcher, "GTEST")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should skip invalid base64 gracefully
	if len(valuations) != 0 {
		t.Errorf("got %d valuations, want 0 (invalid base64)", len(valuations))
	}
}
