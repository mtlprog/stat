package indicator

import (
	"context"
	"math"
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

func TestAnalyticsCalculatorROI(t *testing.T) {
	calc := &AnalyticsCalculator{}
	deps := map[int]Indicator{
		3:  {ID: 3, Value: decimal.NewFromInt(100000)},
		5:  {ID: 5, Value: decimal.NewFromInt(10000)},
		10: {ID: 10, Value: decimal.NewFromFloat(12.0)},
		54: {ID: 54, Value: decimal.NewFromFloat(1.0)},
		55: {ID: 55, Value: decimal.NewFromFloat(10.0)},
		61: {ID: 61, Value: decimal.NewFromFloat(55000)},
	}

	indicators, err := calc.Calculate(context.Background(), domain.FundStructureData{}, deps, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// I43: ((12-10)+1)/10 * 100 = 30%
	indicatorMap := make(map[int]Indicator)
	for _, ind := range indicators {
		indicatorMap[ind.ID] = ind
	}

	i43 := indicatorMap[43]
	expected := decimal.NewFromInt(30)
	if !i43.Value.Equal(expected) {
		t.Errorf("I43 (ROI) = %s, want %s", i43.Value, expected)
	}
}

func TestAnalyticsCalculatorZeroPriceYearAgo(t *testing.T) {
	calc := &AnalyticsCalculator{}
	deps := map[int]Indicator{
		3:  {ID: 3, Value: decimal.NewFromInt(100000)},
		5:  {ID: 5, Value: decimal.NewFromInt(10000)},
		10: {ID: 10, Value: decimal.NewFromFloat(12.0)},
		54: {ID: 54, Value: decimal.Zero},
		55: {ID: 55, Value: decimal.Zero},
		61: {ID: 61, Value: decimal.NewFromFloat(55000)},
	}

	indicators, err := calc.Calculate(context.Background(), domain.FundStructureData{}, deps, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, ind := range indicators {
		if ind.ID == 43 && !ind.Value.IsZero() {
			t.Errorf("I43 should be 0 when I55 is 0")
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
	median := "200"
	participants := "150"
	mtlap := "42"
	dailyVol := "1234.56"
	vol30d := "56789.01"
	data := domain.FundStructureData{
		LiveMetrics: &domain.FundLiveMetrics{
			MTLShareholders:       &holders,
			MTLShareholdersMedian: &median,
			EURMTLParticipants:    &participants,
			MTLAPHolders:          &mtlap,
			EURMTLDailyVolume:     &dailyVol,
			EURMTL30dVolume:       &vol30d,
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
		{27, decimal.NewFromInt(4), "MTL+MTLRECT shareholders"},
		{21, decimal.NewFromInt(2500), "I5/I27"},
		{22, decimal.NewFromInt(21250), "I1/I27"},
		{40, decimal.NewFromInt(42), "MTLAP holders"},
		{23, decimal.NewFromInt(200), "median shareholding"},
		{25, decimal.RequireFromString("1234.56"), "EURMTL daily volume"},
		{26, decimal.RequireFromString("56789.01"), "EURMTL 30d volume"},
	}
	for _, e := range expectations {
		if !got[e.id].Value.Equal(e.want) {
			t.Errorf("I%d (%s) = %s, want %s", e.id, e.desc, got[e.id].Value, e.want)
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

func TestDownsideStdDevNoNegatives(t *testing.T) {
	returns := []decimal.Decimal{
		decimal.NewFromFloat(0.05),
		decimal.NewFromFloat(0.10),
		decimal.NewFromFloat(0.15),
	}

	result := DownsideStdDev(returns, decimal.Zero)
	if !result.IsZero() {
		t.Errorf("DownsideStdDev with no negatives = %s, want 0", result)
	}
}

func TestDownsideStdDevWithNegatives(t *testing.T) {
	returns := []decimal.Decimal{
		decimal.NewFromFloat(-0.05),
		decimal.NewFromFloat(-0.10),
		decimal.NewFromFloat(0.05),
		decimal.NewFromFloat(0.10),
	}

	result := DownsideStdDev(returns, decimal.Zero)
	if result.IsZero() {
		t.Error("DownsideStdDev with negatives should be non-zero")
	}
	f, _ := result.Float64()
	if f < 0 || f > 1 {
		t.Errorf("DownsideStdDev = %f, expected small positive value", f)
	}
}

func TestNormalQuantileEdgeCases(t *testing.T) {
	// p=0 and p=1 should return 0
	if q := NormalQuantile(0); q != 0 {
		t.Errorf("NormalQuantile(0) = %f, want 0", q)
	}
	if q := NormalQuantile(1); q != 0 {
		t.Errorf("NormalQuantile(1) = %f, want 0", q)
	}

	// q(0.95) should be ~1.645
	q95 := NormalQuantile(0.95)
	if math.Abs(q95-1.645) > 0.01 {
		t.Errorf("NormalQuantile(0.95) = %f, want ~1.645", q95)
	}

	// Symmetry: q(p) = -q(1-p)
	q025 := NormalQuantile(0.025)
	q975 := NormalQuantile(0.975)
	if math.Abs(q025+q975) > 0.01 {
		t.Errorf("NormalQuantile symmetry: q(0.025)=%f + q(0.975)=%f should be ~0", q025, q975)
	}
}

func TestVarianceSingleElement(t *testing.T) {
	result := Variance([]decimal.Decimal{decimal.NewFromInt(42)})
	if !result.IsZero() {
		t.Errorf("Variance of single element = %s, want 0", result)
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
