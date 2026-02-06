package fund

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

type mockPortfolio struct {
	portfolios map[string]domain.AccountPortfolio
}

func (m *mockPortfolio) FetchPortfolio(_ context.Context, accountID string) (domain.AccountPortfolio, error) {
	if p, ok := m.portfolios[accountID]; ok {
		return p, nil
	}
	return domain.AccountPortfolio{AccountID: accountID, XLMBalance: "0"}, nil
}

type mockPrice struct{}

func (m *mockPrice) GetPrice(_ context.Context, _, _ domain.AssetInfo, _ string) (domain.TokenPairPrice, error) {
	return domain.TokenPairPrice{Price: "0.5"}, nil
}

func (m *mockPrice) GetTokenPrices(_ context.Context, asset domain.AssetInfo, _ string) (
	string, string, string, string, domain.PriceDetails, domain.PriceDetails, error,
) {
	return "2.0", "10.0", "20.0", "100.0", nil, nil, nil
}

type mockValuation struct{}

func (m *mockValuation) FetchAllValuations(_ context.Context) ([]domain.AssetValuation, error) {
	return nil, nil
}

type mockExternal struct{}

func (m *mockExternal) ResolveValuation(_ context.Context, val domain.AssetValuation) (domain.ResolvedAssetValuation, error) {
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
