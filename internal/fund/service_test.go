package fund

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/price"
)

type mockPortfolio struct {
	portfolios map[string]domain.AccountPortfolio
	err        error
}

func (m *mockPortfolio) FetchPortfolio(_ context.Context, accountID string) (domain.AccountPortfolio, error) {
	if m.err != nil {
		return domain.AccountPortfolio{}, m.err
	}
	if p, ok := m.portfolios[accountID]; ok {
		return p, nil
	}
	return domain.AccountPortfolio{AccountID: accountID, XLMBalance: "0"}, nil
}

type mockPrice struct{}

func (m *mockPrice) GetPrice(_ context.Context, _, _ domain.AssetInfo, _ string) (domain.TokenPairPrice, error) {
	return domain.TokenPairPrice{Price: "0.5"}, nil
}

func (m *mockPrice) GetTokenPrices(_ context.Context, _ domain.AssetInfo, _ string) (price.TokenPriceResult, error) {
	return price.TokenPriceResult{
		PriceEURMTL: "2.0",
		PriceXLM:    "10.0",
		ValueEURMTL: "20.0",
		ValueXLM:    "100.0",
	}, nil
}

type mockValuation struct {
	valuations []domain.AssetValuation
	err        error
}

func (m *mockValuation) FetchAllValuations(_ context.Context) ([]domain.AssetValuation, error) {
	return m.valuations, m.err
}

type mockExternal struct {
	resolved domain.ResolvedAssetValuation
	err      error
}

func (m *mockExternal) ResolveValuation(_ context.Context, val domain.AssetValuation) (domain.ResolvedAssetValuation, error) {
	if m.err != nil {
		return domain.ResolvedAssetValuation{}, m.err
	}
	if m.resolved.ValueInEURMTL != "" {
		return m.resolved, nil
	}
	return domain.ResolvedAssetValuation{AssetValuation: val, ValueInEURMTL: "100"}, nil
}

func TestGetFundStructurePartitioning(t *testing.T) {
	registry := domain.AccountRegistry()
	portfolios := make(map[string]domain.AccountPortfolio)
	for _, acc := range registry {
		portfolios[acc.Address] = domain.AccountPortfolio{
			AccountID:  acc.Address,
			Tokens:     []domain.TokenBalance{{Asset: domain.AssetInfo{Code: "EURMTL"}, Balance: "100"}},
			XLMBalance: "1000",
		}
	}

	svc := NewService(
		&mockPortfolio{portfolios: portfolios},
		&mockPrice{},
		&mockValuation{},
		&mockExternal{},
	)

	result, err := svc.GetFundStructure(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 6 main accounts (1 issuer + 4 subfond + 1 operational)
	if len(result.Accounts) != 6 {
		t.Errorf("Accounts = %d, want 6", len(result.Accounts))
	}
	// 2 mutual
	if len(result.MutualFunds) != 2 {
		t.Errorf("MutualFunds = %d, want 2", len(result.MutualFunds))
	}
	// 3 other
	if len(result.OtherAccounts) != 3 {
		t.Errorf("OtherAccounts = %d, want 3", len(result.OtherAccounts))
	}

	// Aggregated totals only from main accounts
	if result.AggregatedTotals.AccountCount != 6 {
		t.Errorf("AccountCount = %d, want 6", result.AggregatedTotals.AccountCount)
	}
	if result.AggregatedTotals.TotalEURMTL.Equal(decimal.Zero) {
		t.Error("TotalEURMTL should be non-zero")
	}
}

func TestPriceTokenNFTWithValuation(t *testing.T) {
	svc := &Service{
		price:    &mockPrice{},
		external: &mockExternal{resolved: domain.ResolvedAssetValuation{ValueInEURMTL: "500"}},
	}

	tb := domain.TokenBalance{
		Asset:   domain.AssetInfo{Code: "MYTOKEN", Issuer: domain.IssuerAddress, Type: domain.AssetTypeCreditAlphanum12},
		Balance: "0.0000001", // NFT balance
	}

	accountValuations := []domain.AssetValuation{
		{TokenCode: "MYTOKEN", ValuationType: domain.ValuationTypeNFT, RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "500"}, SourceAccount: "GACCOUNT"},
	}

	result, err := svc.priceToken(context.Background(), tb, "GACCOUNT", accountValuations)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.PriceInEURMTL == nil || *result.PriceInEURMTL != "500" {
		t.Errorf("PriceInEURMTL = %v, want 500", result.PriceInEURMTL)
	}
	if result.NFTValuationAccount != "GACCOUNT" {
		t.Errorf("NFTValuationAccount = %q, want GACCOUNT", result.NFTValuationAccount)
	}
}

func TestPriceTokenRegularWithValuation(t *testing.T) {
	svc := &Service{
		price:    &mockPrice{},
		external: &mockExternal{resolved: domain.ResolvedAssetValuation{ValueInEURMTL: "10"}},
	}

	tb := domain.TokenBalance{
		Asset:   domain.AssetInfo{Code: "MYTOKEN", Issuer: domain.IssuerAddress, Type: domain.AssetTypeCreditAlphanum12},
		Balance: "5.0000000",
	}

	accountValuations := []domain.AssetValuation{
		{TokenCode: "MYTOKEN", ValuationType: domain.ValuationTypeUnit, RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "10"}, SourceAccount: "GACCOUNT"},
	}

	result, err := svc.priceToken(context.Background(), tb, "GACCOUNT", accountValuations)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// For regular tokens, value = balance * price = 5 * 10 = 50
	if result.PriceInEURMTL == nil || *result.PriceInEURMTL != "10" {
		t.Errorf("PriceInEURMTL = %v, want 10", result.PriceInEURMTL)
	}
	if result.ValueInEURMTL == nil || *result.ValueInEURMTL != "50" {
		t.Errorf("ValueInEURMTL = %v, want 50", result.ValueInEURMTL)
	}
}

func TestPriceTokenValuationResolutionFallback(t *testing.T) {
	svc := &Service{
		price:    &mockPrice{},
		external: &mockExternal{err: errors.New("resolution failed")},
	}

	tb := domain.TokenBalance{
		Asset:   domain.AssetInfo{Code: "MYTOKEN", Issuer: domain.IssuerAddress, Type: domain.AssetTypeCreditAlphanum12},
		Balance: "0.0000001",
	}

	accountValuations := []domain.AssetValuation{
		{TokenCode: "MYTOKEN", ValuationType: domain.ValuationTypeNFT, RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "500"}, SourceAccount: "GACCOUNT"},
	}

	result, err := svc.priceToken(context.Background(), tb, "GACCOUNT", accountValuations)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// When resolution fails, should fall back to market price from GetTokenPrices
	if result.PriceInEURMTL == nil || *result.PriceInEURMTL != "2.0" {
		t.Errorf("PriceInEURMTL = %v, want 2.0 (market price fallback)", result.PriceInEURMTL)
	}
}

func TestNewServiceNilPortfolioPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil portfolio")
		}
	}()
	NewService(nil, &mockPrice{}, &mockValuation{}, &mockExternal{})
}

func TestNewServiceNilPricePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil price")
		}
	}()
	NewService(&mockPortfolio{}, nil, &mockValuation{}, &mockExternal{})
}

func TestNewServiceNilValuationPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil valuation")
		}
	}()
	NewService(&mockPortfolio{}, &mockPrice{}, nil, &mockExternal{})
}

func TestNewServiceNilExternalPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil external")
		}
	}()
	NewService(&mockPortfolio{}, &mockPrice{}, &mockValuation{}, nil)
}

func TestGetFundStructureValuationError(t *testing.T) {
	svc := NewService(
		&mockPortfolio{},
		&mockPrice{},
		&mockValuation{err: errors.New("horizon down")},
		&mockExternal{},
	)
	_, err := svc.GetFundStructure(context.Background())
	if err == nil {
		t.Error("expected error when FetchAllValuations fails")
	}
}

func TestGetFundStructurePortfolioError(t *testing.T) {
	svc := NewService(
		&mockPortfolio{err: errors.New("account not found")},
		&mockPrice{},
		&mockValuation{},
		&mockExternal{},
	)
	_, err := svc.GetFundStructure(context.Background())
	if err == nil {
		t.Error("expected error when FetchPortfolio fails")
	}
}

func TestPartitionAccounts(t *testing.T) {
	portfolios := []domain.FundAccountPortfolio{
		{Name: "A", Type: domain.AccountTypeIssuer},
		{Name: "B", Type: domain.AccountTypeSubfond},
		{Name: "C", Type: domain.AccountTypeMutual},
		{Name: "D", Type: domain.AccountTypeOther},
		{Name: "E", Type: domain.AccountTypeOperational},
	}

	main, mutual, other := partitionAccounts(portfolios)

	if len(main) != 3 {
		t.Errorf("main = %d, want 3", len(main))
	}
	if len(mutual) != 1 {
		t.Errorf("mutual = %d, want 1", len(mutual))
	}
	if len(other) != 1 {
		t.Errorf("other = %d, want 1", len(other))
	}
}
