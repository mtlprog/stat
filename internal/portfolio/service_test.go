package portfolio

import (
	"context"
	"testing"

	"github.com/mtlprog/stat/internal/horizon"
)

type mockHorizonClient struct {
	account horizon.HorizonAccount
	err     error
}

func (m *mockHorizonClient) FetchAccount(_ context.Context, _ string) (horizon.HorizonAccount, error) {
	return m.account, m.err
}

func TestFetchPortfolioMixedBalances(t *testing.T) {
	mock := &mockHorizonClient{
		account: horizon.HorizonAccount{
			ID: "GABC123",
			Balances: []horizon.HorizonBalance{
				{AssetType: "credit_alphanum4", AssetCode: "MTL", AssetIssuer: "GISSUER1", Balance: "100.0000000"},
				{AssetType: "credit_alphanum12", AssetCode: "EURMTL", AssetIssuer: "GISSUER2", Balance: "500.5000000"},
				{AssetType: "native", Balance: "1000.0000000"},
				{AssetType: "liquidity_pool_shares", Balance: "50.0000000"},
			},
		},
	}

	svc := NewService(mock)
	portfolio, err := svc.FetchPortfolio(context.Background(), "GABC123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if portfolio.AccountID != "GABC123" {
		t.Errorf("AccountID = %q, want GABC123", portfolio.AccountID)
	}
	if portfolio.XLMBalance != "1000.0000000" {
		t.Errorf("XLMBalance = %q, want 1000.0000000", portfolio.XLMBalance)
	}
	if len(portfolio.Tokens) != 2 {
		t.Fatalf("tokens count = %d, want 2 (LP shares excluded)", len(portfolio.Tokens))
	}
	if portfolio.Tokens[0].Asset.Code != "MTL" {
		t.Errorf("tokens[0].Asset.Code = %q, want MTL", portfolio.Tokens[0].Asset.Code)
	}
	if portfolio.Tokens[1].Asset.Code != "EURMTL" {
		t.Errorf("tokens[1].Asset.Code = %q, want EURMTL", portfolio.Tokens[1].Asset.Code)
	}
}

func TestFetchPortfolioEmptyAccount(t *testing.T) {
	mock := &mockHorizonClient{
		account: horizon.HorizonAccount{ID: "GEMPTY"},
	}

	svc := NewService(mock)
	portfolio, err := svc.FetchPortfolio(context.Background(), "GEMPTY")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(portfolio.Tokens) != 0 {
		t.Errorf("tokens count = %d, want 0", len(portfolio.Tokens))
	}
	if portfolio.XLMBalance != "" {
		t.Errorf("XLMBalance = %q, want empty", portfolio.XLMBalance)
	}
}

func TestFetchPortfolioOnlyXLM(t *testing.T) {
	mock := &mockHorizonClient{
		account: horizon.HorizonAccount{
			ID: "GXLM",
			Balances: []horizon.HorizonBalance{
				{AssetType: "native", Balance: "5000.0000000"},
			},
		},
	}

	svc := NewService(mock)
	portfolio, err := svc.FetchPortfolio(context.Background(), "GXLM")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(portfolio.Tokens) != 0 {
		t.Errorf("tokens count = %d, want 0", len(portfolio.Tokens))
	}
	if portfolio.XLMBalance != "5000.0000000" {
		t.Errorf("XLMBalance = %q, want 5000.0000000", portfolio.XLMBalance)
	}
}

func TestFetchPortfolioLPSharesFiltered(t *testing.T) {
	mock := &mockHorizonClient{
		account: horizon.HorizonAccount{
			ID: "GLP",
			Balances: []horizon.HorizonBalance{
				{AssetType: "liquidity_pool_shares", Balance: "100.0000000"},
				{AssetType: "liquidity_pool_shares", Balance: "200.0000000"},
				{AssetType: "native", Balance: "10.0000000"},
			},
		},
	}

	svc := NewService(mock)
	portfolio, err := svc.FetchPortfolio(context.Background(), "GLP")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(portfolio.Tokens) != 0 {
		t.Errorf("tokens count = %d, want 0 (all LP shares)", len(portfolio.Tokens))
	}
}

func TestFetchPortfolioAssetTypeDetermination(t *testing.T) {
	mock := &mockHorizonClient{
		account: horizon.HorizonAccount{
			ID: "GTYPE",
			Balances: []horizon.HorizonBalance{
				{AssetType: "credit_alphanum4", AssetCode: "MTL", AssetIssuer: "G1", Balance: "1"},
				{AssetType: "credit_alphanum12", AssetCode: "EURMTL", AssetIssuer: "G2", Balance: "2"},
				{AssetType: "native", Balance: "3"},
			},
		},
	}

	svc := NewService(mock)
	portfolio, err := svc.FetchPortfolio(context.Background(), "GTYPE")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(portfolio.Tokens[0].Asset.Type) != "credit_alphanum4" {
		t.Errorf("tokens[0] type = %q, want credit_alphanum4", portfolio.Tokens[0].Asset.Type)
	}
	if string(portfolio.Tokens[1].Asset.Type) != "credit_alphanum12" {
		t.Errorf("tokens[1] type = %q, want credit_alphanum12", portfolio.Tokens[1].Asset.Type)
	}
}
