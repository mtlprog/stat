package valuation

import (
	"testing"

	"github.com/mtlprog/stat/internal/domain"
)

func TestLookupValuationNFTPrefersCost(t *testing.T) {
	valuations := []domain.AssetValuation{
		{TokenCode: "MTLART", ValuationType: domain.ValuationTypeNFT, SourceAccount: "GOWNER", RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "100"}},
		{TokenCode: "MTLART", ValuationType: domain.ValuationTypeUnit, SourceAccount: "GOWNER", RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "50"}},
	}

	result := LookupValuation("MTLART", "0.0000001", "GOWNER", valuations)
	if result == nil {
		t.Fatal("expected valuation, got nil")
	}
	if result.ValuationType != domain.ValuationTypeNFT {
		t.Errorf("got type %q, want nft (NFTs prefer _COST)", result.ValuationType)
	}
}

func TestLookupValuationRegularPrefers1COST(t *testing.T) {
	valuations := []domain.AssetValuation{
		{TokenCode: "TOKEN", ValuationType: domain.ValuationTypeNFT, SourceAccount: "GOWNER", RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "100"}},
		{TokenCode: "TOKEN", ValuationType: domain.ValuationTypeUnit, SourceAccount: "GOWNER", RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "50"}},
	}

	result := LookupValuation("TOKEN", "1000.0000000", "GOWNER", valuations)
	if result == nil {
		t.Fatal("expected valuation, got nil")
	}
	if result.ValuationType != domain.ValuationTypeUnit {
		t.Errorf("got type %q, want unit (regular tokens prefer _1COST)", result.ValuationType)
	}
}

func TestLookupValuationOwnerPriority(t *testing.T) {
	valuations := []domain.AssetValuation{
		{TokenCode: "TOKEN", ValuationType: domain.ValuationTypeUnit, SourceAccount: "GOTHER", RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "200"}},
		{TokenCode: "TOKEN", ValuationType: domain.ValuationTypeUnit, SourceAccount: "GOWNER", RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "100"}},
	}

	result := LookupValuation("TOKEN", "10", "GOWNER", valuations)
	if result == nil {
		t.Fatal("expected valuation, got nil")
	}
	if result.RawValue.Value != "100" {
		t.Errorf("Value = %q, want 100 (owner priority)", result.RawValue.Value)
	}
}

func TestLookupValuationFallback(t *testing.T) {
	// Only _COST available for a regular token, should fall back
	valuations := []domain.AssetValuation{
		{TokenCode: "TOKEN", ValuationType: domain.ValuationTypeNFT, SourceAccount: "GOTHER", RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "500"}},
	}

	result := LookupValuation("TOKEN", "100", "GOWNER", valuations)
	if result == nil {
		t.Fatal("expected fallback valuation, got nil")
	}
	if result.ValuationType != domain.ValuationTypeNFT {
		t.Errorf("got type %q, want nft (fallback)", result.ValuationType)
	}
}

func TestLookupValuationNotFound(t *testing.T) {
	valuations := []domain.AssetValuation{
		{TokenCode: "OTHER", ValuationType: domain.ValuationTypeUnit, SourceAccount: "GOTHER"},
	}

	result := LookupValuation("TOKEN", "100", "GOWNER", valuations)
	if result != nil {
		t.Error("expected nil for non-matching token")
	}
}
