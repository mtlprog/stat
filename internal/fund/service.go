package fund

import (
	"context"
	"fmt"
	"time"

	"github.com/samber/lo"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/valuation"
)

// PortfolioService defines the portfolio fetching interface.
type PortfolioService interface {
	FetchPortfolio(ctx context.Context, accountID string) (domain.AccountPortfolio, error)
}

// PriceService defines the price discovery interface.
type PriceService interface {
	GetPrice(ctx context.Context, asset, baseAsset domain.AssetInfo, amount string) (domain.TokenPairPrice, error)
	GetTokenPrices(ctx context.Context, asset domain.AssetInfo, balance string) (
		priceEURMTL, priceXLM, valueEURMTL, valueXLM string,
		detailsEURMTL, detailsXLM domain.PriceDetails,
		err error,
	)
}

// ValuationService defines the valuation scanning interface.
type ValuationService interface {
	FetchAllValuations(ctx context.Context) ([]domain.AssetValuation, error)
}

// ExternalPriceService defines the external price resolution interface.
type ExternalPriceService interface {
	ResolveValuation(ctx context.Context, val domain.AssetValuation) (domain.ResolvedAssetValuation, error)
}

// Service orchestrates the full fund structure pipeline.
type Service struct {
	portfolio PortfolioService
	price     PriceService
	valuation ValuationService
	external  ExternalPriceService
}

// NewService creates a new FundStructureService.
func NewService(portfolio PortfolioService, price PriceService, val ValuationService, ext ExternalPriceService) *Service {
	return &Service{
		portfolio: portfolio,
		price:     price,
		valuation: val,
		external:  ext,
	}
}

// GetFundStructure runs the full fund aggregation pipeline (Section 6).
func (s *Service) GetFundStructure(ctx context.Context) (domain.FundStructureData, error) {
	// Step 1: Fetch all valuations
	allValuations, err := s.valuation.FetchAllValuations(ctx)
	if err != nil {
		return domain.FundStructureData{}, fmt.Errorf("fetching valuations: %w", err)
	}

	// Step 2: Process each account
	var allPortfolios []domain.FundAccountPortfolio
	for _, acc := range domain.AccountRegistry {
		portfolio, err := s.processAccount(ctx, acc, allValuations)
		if err != nil {
			return domain.FundStructureData{}, fmt.Errorf("processing account %s: %w", acc.Name, err)
		}
		allPortfolios = append(allPortfolios, portfolio)

		// 200ms delay between accounts
		select {
		case <-ctx.Done():
			return domain.FundStructureData{}, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}

	// Step 3: Partition into three groups
	mainAccounts, mutualAccounts, otherAccounts := partitionAccounts(allPortfolios)

	return domain.FundStructureData{
		Accounts:         mainAccounts,
		MutualFunds:      mutualAccounts,
		OtherAccounts:    otherAccounts,
		AggregatedTotals: calculateFundTotals(mainAccounts),
	}, nil
}

func (s *Service) processAccount(ctx context.Context, acc domain.FundAccount, allValuations []domain.AssetValuation) (domain.FundAccountPortfolio, error) {
	// Fetch balances
	rawPortfolio, err := s.portfolio.FetchPortfolio(ctx, acc.Address)
	if err != nil {
		return domain.FundAccountPortfolio{}, err
	}

	// Merge valuations with owner priority
	accountValuations := mergeValuations(acc.Address, allValuations)

	// Get prices for all tokens
	var tokens []domain.TokenPriceWithBalance
	for _, tb := range rawPortfolio.Tokens {
		token, err := s.priceToken(ctx, tb, acc.Address, accountValuations)
		if err != nil {
			// Log and continue — don't fail the whole account for one token
			tokens = append(tokens, domain.TokenPriceWithBalance{
				Asset:   tb.Asset,
				Balance: tb.Balance,
			})
			continue
		}
		tokens = append(tokens, token)

		// 100ms delay between tokens
		select {
		case <-ctx.Done():
			return domain.FundAccountPortfolio{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}

	// Get XLM price in EURMTL
	var xlmPriceInEURMTL *string
	xlmResult, err := s.price.GetPrice(ctx, domain.XLMAsset, domain.EURMTLAsset, "1")
	if err == nil {
		xlmPriceInEURMTL = &xlmResult.Price
	}

	return domain.FundAccountPortfolio{
		ID:               acc.Address,
		Name:             acc.Name,
		Type:             acc.Type,
		Description:      acc.Description,
		Tokens:           tokens,
		XLMBalance:       rawPortfolio.XLMBalance,
		XLMPriceInEURMTL: xlmPriceInEURMTL,
		TotalEURMTL:      calculateAccountTotalEURMTL(tokens, rawPortfolio.XLMBalance, xlmPriceInEURMTL),
		TotalXLM:         calculateAccountTotalXLM(tokens, rawPortfolio.XLMBalance),
	}, nil
}

func (s *Service) priceToken(ctx context.Context, tb domain.TokenBalance, accountID string, accountValuations []domain.AssetValuation) (domain.TokenPriceWithBalance, error) {
	isNFT := valuation.IsNFT(tb.Balance)

	// Get market prices
	priceEURMTL, priceXLM, valueEURMTL, valueXLM, detailsEURMTL, detailsXLM, err := s.price.GetTokenPrices(ctx, tb.Asset, tb.Balance)
	if err != nil {
		return domain.TokenPriceWithBalance{}, err
	}

	result := domain.TokenPriceWithBalance{
		Asset:         tb.Asset,
		Balance:       tb.Balance,
		PriceInEURMTL: strToPtr(priceEURMTL),
		PriceInXLM:    strToPtr(priceXLM),
		ValueInEURMTL: strToPtr(valueEURMTL),
		ValueInXLM:    strToPtr(valueXLM),
		DetailsEURMTL: detailsEURMTL,
		DetailsXLM:    detailsXLM,
		IsNFT:         isNFT,
	}

	// Check for manual valuation override
	val := valuation.LookupValuation(tb.Asset.Code, tb.Balance, accountID, accountValuations)
	if val != nil {
		resolved, err := s.external.ResolveValuation(ctx, *val)
		if err == nil {
			result.PriceInEURMTL = &resolved.ValueInEURMTL
			if isNFT {
				result.ValueInEURMTL = &resolved.ValueInEURMTL
			} else {
				v := domain.MultiplyWithPrecision(tb.Balance, resolved.ValueInEURMTL)
				result.ValueInEURMTL = &v
			}
			result.NFTValuationAccount = val.SourceAccount

			// Derive XLM value from EURMTL valuation
			xlmRate, xlmErr := s.price.GetPrice(ctx, domain.EURMTLAsset, domain.XLMAsset, "1")
			if xlmErr == nil {
				xlmPrice := domain.DivideWithPrecision(resolved.ValueInEURMTL, xlmRate.Price)
				result.PriceInXLM = &xlmPrice
				if isNFT {
					result.ValueInXLM = &xlmPrice
				} else {
					xlmVal := domain.MultiplyWithPrecision(tb.Balance, xlmPrice)
					result.ValueInXLM = &xlmVal
				}
			}
		}
	}

	return result, nil
}

func partitionAccounts(portfolios []domain.FundAccountPortfolio) (main, mutual, other []domain.FundAccountPortfolio) {
	groups := lo.GroupBy(portfolios, func(p domain.FundAccountPortfolio) string {
		switch p.Type {
		case domain.AccountTypeMutual:
			return "mutual"
		case domain.AccountTypeOther:
			return "other"
		default:
			return "main"
		}
	})
	return groups["main"], groups["mutual"], groups["other"]
}

func strToPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
