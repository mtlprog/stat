package indicator

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

func TestDividendCalculatorZeroDeps(t *testing.T) {
	calc := &DividendCalculator{}
	deps := map[int]Indicator{
		5:  {ID: 5, Value: decimal.NewFromInt(10000)},
		10: {ID: 10, Value: decimal.NewFromFloat(8.5)},
	}

	indicators, err := calc.Calculate(context.Background(), domain.FundStructureData{}, deps, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All should be zero since there's no dividend data
	for _, ind := range indicators {
		if !ind.Value.IsZero() {
			t.Errorf("I%d = %s, want 0 (no dividend data)", ind.ID, ind.Value)
		}
	}
}

func TestDividendCalculatorDivisionByZero(t *testing.T) {
	calc := &DividendCalculator{}
	deps := map[int]Indicator{
		5:  {ID: 5, Value: decimal.Zero},
		10: {ID: 10, Value: decimal.Zero},
	}

	indicators, err := calc.Calculate(context.Background(), domain.FundStructureData{}, deps, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, ind := range indicators {
		if !ind.Value.IsZero() {
			t.Errorf("I%d = %s, want 0 (division by zero protection)", ind.ID, ind.Value)
		}
	}
}

func TestTokenomicsCalculatorFromLiveMetrics(t *testing.T) {
	calc := &TokenomicsCalculator{}

	deps := map[int]Indicator{
		1: {ID: 1, Value: decimal.NewFromInt(85000)},
		5: {ID: 5, Value: decimal.NewFromInt(10000)},
	}

	holders := "4"
	holdersAny := "6"
	median := "200"
	participants := "150"
	mtlap := "42"
	dailyVol := "1234.56"
	totalVol := "56789.01"
	data := domain.FundStructureData{
		LiveMetrics: &domain.FundLiveMetrics{
			MTLShareholders:       &holders,
			MTLShareholdersAny:    &holdersAny,
			MTLShareholdersMedian: &median,
			EURMTLParticipants:    &participants,
			MTLAPHolders:          &mtlap,
			EURMTLDailyVolume:     &dailyVol,
			EURMTLPaymentTotal:    &totalVol,
		},
	}

	indicators, err := calc.Calculate(context.Background(), data, deps, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := make(map[int]Indicator)
	for _, ind := range indicators {
		got[ind.ID] = ind
	}

	expectations := []struct {
		id   int
		want decimal.Decimal
		desc string
	}{
		{24, decimal.NewFromInt(150), "EURMTL holders"},
		{27, decimal.NewFromInt(4), "MTL+MTLRECT shareholders ≥1"},
		{62, decimal.NewFromInt(6), "MTL+MTLRECT shareholders any"},
		{21, decimal.NewFromInt(2500), "I5/I27"},
		{22, decimal.NewFromInt(21250), "I1/I27"},
		{40, decimal.NewFromInt(42), "MTLAP holders"},
		{23, decimal.NewFromInt(200), "median shareholding"},
		{25, decimal.RequireFromString("1234.56"), "EURMTL daily volume"},
		{26, decimal.RequireFromString("56789.01"), "EURMTL cumulative volume"},
	}
	for _, e := range expectations {
		if !got[e.id].Value.Equal(e.want) {
			t.Errorf("I%d (%s) = %s, want %s", e.id, e.desc, got[e.id].Value, e.want)
		}
	}
}

func TestLayer1CalculatorFromLiveMetrics(t *testing.T) {
	calc := &Layer1Calculator{}

	mtlCirc := "105663.22"
	mtlrectCirc := "541972.30"
	mtlPrice := "8.5"
	mtlrectPrice := "0.4"
	data := domain.FundStructureData{
		LiveMetrics: &domain.FundLiveMetrics{
			MTLCirculation:     &mtlCirc,
			MTLRECTCirculation: &mtlrectCirc,
			MTLMarketPrice:     &mtlPrice,
			MTLRECTMarketPrice: &mtlrectPrice,
		},
	}
	deps := map[int]Indicator{
		51: {ID: 51, Value: decimal.NewFromInt(1_000_000)},
		52: {ID: 52, Value: decimal.NewFromInt(500_000)},
		53: {ID: 53, Value: decimal.NewFromInt(250_000)},
		58: {ID: 58, Value: decimal.NewFromInt(80_000)},
		59: {ID: 59, Value: decimal.NewFromInt(200)},
		60: {ID: 60, Value: decimal.NewFromInt(9_000)},
	}

	out, err := calc.Calculate(context.Background(), data, deps, nil)
	if err != nil {
		t.Fatalf("Calculate failed: %v", err)
	}
	got := make(map[int]Indicator)
	for _, ind := range out {
		got[ind.ID] = ind
	}

	expectations := []struct {
		id   int
		want decimal.Decimal
		desc string
	}{
		// I5/I6/I7 round to 0dp via IndicatorMeta.Precision (token counts) —
		// I5 sums the raw circulation values then rounds, so .22+.30=.52 rounds
		// up to 647636.
		{3, decimal.NewFromInt(1_839_200), "I3 sums I51+I52+I53+I58+I59+I60"},
		{6, decimal.NewFromInt(105663), "I6 from LiveMetrics, rounded to 0dp"},
		{7, decimal.NewFromInt(541972), "I7 from LiveMetrics, rounded to 0dp"},
		{5, decimal.NewFromInt(647636), "I5 = round(I6_raw + I7_raw) = round(647635.52)"},
		{10, decimal.RequireFromString("8.5"), "I10 from LiveMetrics"},
		{49, decimal.RequireFromString("0.4"), "I49 from LiveMetrics"},
	}
	for _, e := range expectations {
		if !got[e.id].Value.Equal(e.want) {
			t.Errorf("I%d (%s) = %s, want %s", e.id, e.desc, got[e.id].Value, e.want)
		}
	}
}

func TestLayer1CalculatorMissingLiveMetrics(t *testing.T) {
	calc := &Layer1Calculator{}
	deps := map[int]Indicator{
		51: {ID: 51, Value: decimal.NewFromInt(100)},
		52: {ID: 52, Value: decimal.NewFromInt(200)},
		53: {ID: 53, Value: decimal.NewFromInt(300)},
		58: {ID: 58, Value: decimal.NewFromInt(400)},
		59: {ID: 59, Value: decimal.NewFromInt(500)},
		60: {ID: 60, Value: decimal.NewFromInt(600)},
	}

	out, err := calc.Calculate(context.Background(), domain.FundStructureData{}, deps, nil)
	if err != nil {
		t.Fatalf("Calculate failed: %v", err)
	}
	got := make(map[int]Indicator)
	for _, ind := range out {
		got[ind.ID] = ind
	}
	// I3 still sums regardless of LiveMetrics.
	if !got[3].Value.Equal(decimal.NewFromInt(2100)) {
		t.Errorf("I3 = %s, want 2100", got[3].Value)
	}
	// Live indicators resolve to zero — documented backfill behaviour.
	for _, id := range []int{6, 7, 10, 49} {
		if !got[id].Value.IsZero() {
			t.Errorf("I%d = %s, want 0 (no LiveMetrics)", id, got[id].Value)
		}
	}
}

func TestTokenomicsCalculatorMissingLiveMetrics(t *testing.T) {
	calc := &TokenomicsCalculator{}

	deps := map[int]Indicator{
		1: {ID: 1, Value: decimal.NewFromInt(85000)},
		5: {ID: 5, Value: decimal.NewFromInt(10000)},
	}

	indicators, err := calc.Calculate(context.Background(), domain.FundStructureData{}, deps, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, ind := range indicators {
		if !ind.Value.IsZero() {
			t.Errorf("I%d = %s, want 0 (no LiveMetrics)", ind.ID, ind.Value)
		}
	}
}

func TestNewIndicatorUsesRegistry(t *testing.T) {
	ind := NewIndicator(1, decimal.NewFromInt(85000), "fallback", "fallback-unit")
	if ind.Name != "Market Cap EUR" {
		t.Errorf("Name = %q, want 'Market Cap EUR' from registry", ind.Name)
	}
	if ind.Unit != "EURMTL" {
		t.Errorf("Unit = %q, want 'EURMTL' from registry", ind.Unit)
	}
	if ind.Description == "" {
		t.Error("Description should be populated from registry, got empty string")
	}
}

func TestNewIndicatorFallback(t *testing.T) {
	ind := NewIndicator(9999, decimal.NewFromInt(1), "Custom", "custom-unit")
	if ind.Name != "Custom" {
		t.Errorf("Name = %q, want 'Custom' (fallback)", ind.Name)
	}
}

func TestIsRegistered(t *testing.T) {
	cases := []struct {
		id   int
		want bool
		desc string
	}{
		{1, true, "I1 — Market Cap, currently registered"},
		{40, true, "I40 — Association Participants (MTLAP holders)"},
		{62, true, "I62 — Shareholders"},
		{16, false, "I16 — deprecated, removed from registry"},
		{44, false, "I44 — deprecated Beta"},
		{9999, false, "never-existed ID"},
	}
	for _, c := range cases {
		if got := IsRegistered(c.id); got != c.want {
			t.Errorf("IsRegistered(%d) = %v, want %v (%s)", c.id, got, c.want, c.desc)
		}
	}
}

func TestRegistryDuplicateIDPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate ID registration")
		}
	}()

	registry := NewRegistry()
	registry.Register(&Layer0Calculator{})
	registry.Register(&Layer0Calculator{}) // duplicate IDs
}

type cyclicCalcA struct{}

func (c *cyclicCalcA) IDs() []int          { return []int{9901} }
func (c *cyclicCalcA) Dependencies() []int { return []int{9902} }
func (c *cyclicCalcA) Calculate(_ context.Context, _ domain.FundStructureData, _ map[int]Indicator, _ *HistoricalData) ([]Indicator, error) {
	return nil, nil
}

type cyclicCalcB struct{}

func (c *cyclicCalcB) IDs() []int          { return []int{9902} }
func (c *cyclicCalcB) Dependencies() []int { return []int{9901} }
func (c *cyclicCalcB) Calculate(_ context.Context, _ domain.FundStructureData, _ map[int]Indicator, _ *HistoricalData) ([]Indicator, error) {
	return nil, nil
}

func TestRegistryDependencyCycleDetected(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&cyclicCalcA{})
	registry.Register(&cyclicCalcB{})

	_, err := registry.CalculateAll(context.Background(), domain.FundStructureData{}, nil)
	if err == nil {
		t.Error("expected error for dependency cycle, got nil")
	}
}
