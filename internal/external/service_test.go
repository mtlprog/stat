package external

import (
	"context"
	"testing"
	"time"

	"github.com/mtlprog/stat/internal/domain"
)

type mockQuoteRepo struct {
	quotes map[string]Quote
}

func (m *mockQuoteRepo) SaveQuote(_ context.Context, symbol string, priceInEUR float64) error {
	m.quotes[symbol] = Quote{Symbol: symbol, PriceInEUR: priceInEUR, UpdatedAt: time.Now()}
	return nil
}

func (m *mockQuoteRepo) GetQuote(_ context.Context, symbol string) (Quote, error) {
	q, ok := m.quotes[symbol]
	if !ok {
		return Quote{}, context.DeadlineExceeded // simulate not found
	}
	return q, nil
}

func (m *mockQuoteRepo) GetAllQuotes(_ context.Context) ([]Quote, error) {
	var result []Quote
	for _, q := range m.quotes {
		result = append(result, q)
	}
	return result, nil
}

func TestResolveValuationDirectEURMTL(t *testing.T) {
	repo := &mockQuoteRepo{quotes: make(map[string]Quote)}
	svc := NewService(nil, repo)

	val := domain.AssetValuation{
		TokenCode:     "TOKEN",
		ValuationType: domain.ValuationTypeUnit,
		RawValue:      domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "100"},
	}

	resolved, err := svc.ResolveValuation(context.Background(), val)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.ValueInEURMTL != "100" {
		t.Errorf("ValueInEURMTL = %q, want 100", resolved.ValueInEURMTL)
	}
}

func TestResolveValuationBTC(t *testing.T) {
	repo := &mockQuoteRepo{quotes: map[string]Quote{
		"BTC": {Symbol: "BTC", PriceInEUR: 55000, UpdatedAt: time.Now()},
	}}
	svc := NewService(nil, repo)

	val := domain.AssetValuation{
		TokenCode:     "WBTC",
		ValuationType: domain.ValuationTypeUnit,
		RawValue:      domain.ValuationValue{Type: domain.ValuationValueExternal, Symbol: "BTC"},
	}

	resolved, err := svc.ResolveValuation(context.Background(), val)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.ValueInEURMTL != "55000" {
		t.Errorf("ValueInEURMTL = %q, want 55000", resolved.ValueInEURMTL)
	}
}

func TestResolveValuationAUCompound(t *testing.T) {
	repo := &mockQuoteRepo{quotes: map[string]Quote{
		"AU": {Symbol: "AU", PriceInEUR: 57.88, UpdatedAt: time.Now()}, // price per gram
	}}
	svc := NewService(nil, repo)

	qty := 2.5
	val := domain.AssetValuation{
		TokenCode:     "AUMTL",
		ValuationType: domain.ValuationTypeUnit,
		RawValue:      domain.ValuationValue{Type: domain.ValuationValueExternal, Symbol: "AU", Quantity: &qty, Unit: "g"},
	}

	resolved, err := svc.ResolveValuation(context.Background(), val)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 57.88 * 2.5 = 144.7
	if resolved.ValueInEURMTL != "144.7" {
		t.Errorf("ValueInEURMTL = %q, want 144.7", resolved.ValueInEURMTL)
	}
}

func TestResolveValuationMissingQuote(t *testing.T) {
	repo := &mockQuoteRepo{quotes: make(map[string]Quote)}
	svc := NewService(nil, repo)

	val := domain.AssetValuation{
		TokenCode:     "TOKEN",
		ValuationType: domain.ValuationTypeUnit,
		RawValue:      domain.ValuationValue{Type: domain.ValuationValueExternal, Symbol: "BTC"},
	}

	_, err := svc.ResolveValuation(context.Background(), val)
	if err == nil {
		t.Error("expected error for missing quote")
	}
}
