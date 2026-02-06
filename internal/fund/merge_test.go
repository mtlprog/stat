package fund

import (
	"testing"

	"github.com/mtlprog/stat/internal/domain"
)

func TestMergeValuationsOwnerWins(t *testing.T) {
	all := []domain.AssetValuation{
		{TokenCode: "TOKEN", ValuationType: domain.ValuationTypeUnit, SourceAccount: "GOTHER", RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "200"}},
		{TokenCode: "TOKEN", ValuationType: domain.ValuationTypeUnit, SourceAccount: "GOWNER", RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "100"}},
	}

	merged := mergeValuations("GOWNER", all)

	// Owner's valuation should be first
	if len(merged) != 1 {
		t.Fatalf("merged count = %d, want 1 (owner wins, other excluded)", len(merged))
	}
	if merged[0].RawValue.Value != "100" {
		t.Errorf("Value = %q, want 100 (owner)", merged[0].RawValue.Value)
	}
}

func TestMergeValuationsNoConflict(t *testing.T) {
	all := []domain.AssetValuation{
		{TokenCode: "TOKEN_A", ValuationType: domain.ValuationTypeUnit, SourceAccount: "GOWNER"},
		{TokenCode: "TOKEN_B", ValuationType: domain.ValuationTypeNFT, SourceAccount: "GOTHER"},
	}

	merged := mergeValuations("GOWNER", all)
	if len(merged) != 2 {
		t.Errorf("merged count = %d, want 2 (no conflict)", len(merged))
	}
}

func TestMergeValuationsEmpty(t *testing.T) {
	merged := mergeValuations("GOWNER", nil)
	if len(merged) != 0 {
		t.Errorf("merged count = %d, want 0", len(merged))
	}
}
