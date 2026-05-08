package indicator

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

func TestBPPCalculatorEmitsConstant(t *testing.T) {
	calc := &BPPCalculator{}

	indicators, err := calc.Calculate(context.Background(), domain.FundStructureData{}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(indicators) != 1 || indicators[0].ID != 39 {
		t.Fatalf("expected one indicator I39, got %+v", indicators)
	}
	if !indicators[0].Value.Equal(decimal.NewFromInt(bppValue)) {
		t.Errorf("I39 value = %s, want %d", indicators[0].Value, bppValue)
	}
	if indicators[0].Unit != "EURMTL" {
		t.Errorf("I39 unit = %q, want EURMTL (from registry)", indicators[0].Unit)
	}
}

func TestBPPCalculatorIsDeterministic(t *testing.T) {
	if !DeterministicIDs[39] {
		t.Error("I39 should be in DeterministicIDs (constant — re-derivable for any snapshot)")
	}
}
