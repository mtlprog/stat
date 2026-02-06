package indicator

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

func TestRegistryExecutionOrder(t *testing.T) {
	registry := NewRegistry()

	// Register in reverse order — should still execute in dependency order
	registry.Register(&Layer2Calculator{})
	registry.Register(&Layer1Calculator{})
	registry.Register(&Layer0Calculator{})
	registry.Register(&DividendCalculator{})
	registry.Register(&AnalyticsCalculator{})
	registry.Register(&TokenomicsCalculator{})

	data := testFundStructureData()
	indicators, err := registry.CalculateAll(context.Background(), data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(indicators) == 0 {
		t.Fatal("expected indicators, got none")
	}

	// Check that I1 exists (depends on I5, I10 which depend on I51, etc.)
	found := false
	for _, ind := range indicators {
		if ind.ID == 1 {
			found = true
			break
		}
	}
	if !found {
		t.Error("I1 (Market Cap) not found in results")
	}
}

func TestLayer0Calculator(t *testing.T) {
	calc := &Layer0Calculator{}
	data := testFundStructureData()

	indicators, err := calc.Calculate(context.Background(), data, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should produce I51 (DEFI), I58 (MAIN ISSUER), I61 (BTC rate) at minimum
	ids := make(map[int]bool)
	for _, ind := range indicators {
		ids[ind.ID] = true
	}

	if !ids[51] {
		t.Error("missing I51 (DEFI)")
	}
	if !ids[58] {
		t.Error("missing I58 (MAIN ISSUER)")
	}
	if !ids[61] {
		t.Error("missing I61 (BTC rate)")
	}
}

func TestLayer2Formulas(t *testing.T) {
	calc := &Layer2Calculator{}

	deps := map[int]Indicator{
		3:  {ID: 3, Value: decimal.NewFromInt(100000)},  // Assets Value
		5:  {ID: 5, Value: decimal.NewFromInt(10000)},    // Total Shares
		10: {ID: 10, Value: decimal.NewFromFloat(8.5)},   // Share Price
		61: {ID: 61, Value: decimal.NewFromFloat(55000)},  // BTC Rate
	}

	indicators, err := calc.Calculate(context.Background(), domain.FundStructureData{}, deps, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	indicatorMap := make(map[int]Indicator)
	for _, ind := range indicators {
		indicatorMap[ind.ID] = ind
	}

	// I1 = I5 * I10 = 10000 * 8.5 = 85000
	if i1 := indicatorMap[1]; !i1.Value.Equal(decimal.NewFromInt(85000)) {
		t.Errorf("I1 = %s, want 85000", i1.Value)
	}

	// I8 = I3 / I5 = 100000 / 10000 = 10
	if i8 := indicatorMap[8]; !i8.Value.Equal(decimal.NewFromInt(10)) {
		t.Errorf("I8 = %s, want 10", i8.Value)
	}

	// I30 = I10 / I8 = 8.5 / 10 = 0.85
	if i30 := indicatorMap[30]; !i30.Value.Equal(decimal.NewFromFloat(0.85)) {
		t.Errorf("I30 = %s, want 0.85", i30.Value)
	}
}

func TestLayer2DivisionByZero(t *testing.T) {
	calc := &Layer2Calculator{}

	deps := map[int]Indicator{
		3:  {ID: 3, Value: decimal.Zero},
		5:  {ID: 5, Value: decimal.Zero},
		10: {ID: 10, Value: decimal.Zero},
		61: {ID: 61, Value: decimal.Zero},
	}

	indicators, err := calc.Calculate(context.Background(), domain.FundStructureData{}, deps, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, ind := range indicators {
		if !ind.Value.Equal(decimal.Zero) {
			t.Errorf("I%d = %s, want 0 (division by zero protection)", ind.ID, ind.Value)
		}
	}
}

func testFundStructureData() domain.FundStructureData {
	return domain.FundStructureData{
		Accounts: []domain.FundAccountPortfolio{
			{Name: "MAIN ISSUER", Type: domain.AccountTypeIssuer, TotalEURMTL: 50000, TotalXLM: 200000},
			{Name: "DEFI", Type: domain.AccountTypeSubfond, TotalEURMTL: 30000, TotalXLM: 120000},
			{Name: "MCITY", Type: domain.AccountTypeSubfond, TotalEURMTL: 10000, TotalXLM: 40000},
			{Name: "MABIZ", Type: domain.AccountTypeSubfond, TotalEURMTL: 5000, TotalXLM: 20000},
			{Name: "BOSS", Type: domain.AccountTypeSubfond, TotalEURMTL: 2000, TotalXLM: 8000},
			{Name: "ADMIN", Type: domain.AccountTypeOperational, TotalEURMTL: 3000, TotalXLM: 12000},
		},
		MutualFunds: []domain.FundAccountPortfolio{
			{Name: "APART", Type: domain.AccountTypeMutual, TotalEURMTL: 15000},
			{Name: "MFB", Type: domain.AccountTypeMutual, TotalEURMTL: 8000},
		},
		AggregatedTotals: domain.AggregatedTotals{
			TotalEURMTL:  100000,
			TotalXLM:     400000,
			AccountCount: 6,
			TokenCount:   45,
		},
	}
}
