package portfolio

import (
	"context"
	"fmt"

	"github.com/samber/lo"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/horizon"
)

// HorizonClient defines the subset of Horizon API used by PortfolioService.
type HorizonClient interface {
	FetchAccount(ctx context.Context, accountID string) (horizon.HorizonAccount, error)
}

// Service fetches and converts raw Stellar account balances into domain portfolios.
type Service struct {
	horizon HorizonClient
}

// NewService creates a new PortfolioService.
func NewService(horizon HorizonClient) *Service {
	return &Service{horizon: horizon}
}

// FetchPortfolio retrieves balances for a Stellar account and converts them into an AccountPortfolio.
// LP shares are excluded; XLM is extracted separately.
func (s *Service) FetchPortfolio(ctx context.Context, accountID string) (domain.AccountPortfolio, error) {
	account, err := s.horizon.FetchAccount(ctx, accountID)
	if err != nil {
		return domain.AccountPortfolio{}, fmt.Errorf("fetching portfolio for %s: %w", accountID, err)
	}

	var xlmBalance string

	tokens := lo.FilterMap(account.Balances, func(b horizon.HorizonBalance, _ int) (domain.TokenBalance, bool) {
		// Extract XLM separately
		if b.AssetType == "native" {
			xlmBalance = b.Balance
			return domain.TokenBalance{}, false
		}

		// Exclude LP shares
		if b.AssetType == "liquidity_pool_shares" {
			return domain.TokenBalance{}, false
		}

		return domain.TokenBalance{
			Asset: domain.AssetInfo{
				Code:   b.AssetCode,
				Issuer: b.AssetIssuer,
				Type:   domain.AssetTypeFromCode(b.AssetCode),
			},
			Balance: b.Balance,
			Limit:   b.Limit,
		}, true
	})

	return domain.AccountPortfolio{
		AccountID:  accountID,
		Tokens:     tokens,
		XLMBalance: xlmBalance,
	}, nil
}
