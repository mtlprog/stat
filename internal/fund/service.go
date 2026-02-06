package fund

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/samber/lo"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/price"
	"github.com/mtlprog/stat/internal/valuation"
)

// PortfolioService defines the portfolio fetching interface.
type PortfolioService interface {
	FetchPortfolio(ctx context.Context, accountID string) (domain.AccountPortfolio, error)
}

// PriceService defines the price discovery interface.
type PriceService interface {
	GetPrice(ctx context.Context, asset, baseAsset domain.AssetInfo, amount string) (domain.TokenPairPrice, error)
	GetTokenPrices(ctx context.Context, asset domain.AssetInfo, balance string) (price.TokenPriceResult, error)
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

// NewService creates a new fund structure Service. All dependencies are required.
func NewService(portfolio PortfolioService, priceSvc PriceService, val ValuationService, ext ExternalPriceService) *Service {
	if portfolio == nil {
		panic("fund.NewService: portfolio is nil")
	}
	if priceSvc == nil {
		panic("fund.NewService: price is nil")
	}
	if val == nil {
		panic("fund.NewService: valuation is nil")
	}
	if ext == nil {
		panic("fund.NewService: external is nil")
	}
	return &Service{
		portfolio: portfolio,
		price:     priceSvc,
		valuation: val,
		external:  ext,
	}
}

// GetFundStructure runs the full fund aggregation pipeline.
func (s *Service) GetFundStructure(ctx context.Context) (domain.FundStructureData, error) {
	allValuations, err := s.valuation.FetchAllValuations(ctx)
	if err != nil {
		return domain.FundStructureData{}, fmt.Errorf("fetching valuations: %w", err)
	}

	var allPortfolios []domain.FundAccountPortfolio
	var warnings []string
	for _, acc := range domain.AccountRegistry() {
		portfolio, accWarnings, err := s.processAccount(ctx, acc, allValuations)
		if err != nil {
			return domain.FundStructureData{}, fmt.Errorf("processing account %s: %w", acc.Name, err)
		}
		allPortfolios = append(allPortfolios, portfolio)
		warnings = append(warnings, accWarnings...)

		// 200ms delay between accounts
		select {
		case <-ctx.Done():
			return domain.FundStructureData{}, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}

	mainAccounts, mutualAccounts, otherAccounts := partitionAccounts(allPortfolios)

	return domain.FundStructureData{
		Accounts:         mainAccounts,
		MutualFunds:      mutualAccounts,
		OtherAccounts:    otherAccounts,
		AggregatedTotals: calculateFundTotals(mainAccounts),
		Warnings:         warnings,
	}, nil
}

func (s *Service) processAccount(ctx context.Context, acc domain.FundAccount, allValuations []domain.AssetValuation) (domain.FundAccountPortfolio, []string, error) {
	rawPortfolio, err := s.portfolio.FetchPortfolio(ctx, acc.Address)
	if err != nil {
		return domain.FundAccountPortfolio{}, nil, err
	}

	accountValuations := mergeValuations(acc.Address, allValuations)

	var tokens []domain.TokenPriceWithBalance
	var warnings []string
	for _, tb := range rawPortfolio.Tokens {
		token, err := s.priceToken(ctx, tb, acc.Address, accountValuations)
		if err != nil {
			w := fmt.Sprintf("failed to price %s on %s: %v", tb.Asset.Code, acc.Name, err)
			slog.Warn(w)
			warnings = append(warnings, w)
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
			return domain.FundAccountPortfolio{}, nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}

	var xlmPriceInEURMTL *string
	xlmResult, err := s.price.GetPrice(ctx, domain.XLMAsset(), domain.EURMTLAsset(), "1")
	if err == nil {
		xlmPriceInEURMTL = &xlmResult.Price
	} else {
		w := fmt.Sprintf("XLM price unavailable for %s, EURMTL total excludes XLM", acc.Name)
		slog.Warn(w, "error", err)
		warnings = append(warnings, w)
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
	}, warnings, nil
}

func (s *Service) priceToken(ctx context.Context, tb domain.TokenBalance, accountID string, accountValuations []domain.AssetValuation) (domain.TokenPriceWithBalance, error) {
	isNFT := valuation.IsNFT(tb.Balance)

	prices, priceErr := s.price.GetTokenPrices(ctx, tb.Asset, tb.Balance)

	result := domain.TokenPriceWithBalance{
		Asset:         tb.Asset,
		Balance:       tb.Balance,
		PriceInEURMTL: strToPtr(prices.PriceEURMTL),
		PriceInXLM:    strToPtr(prices.PriceXLM),
		ValueInEURMTL: strToPtr(prices.ValueEURMTL),
		ValueInXLM:    strToPtr(prices.ValueXLM),
		DetailsEURMTL: prices.DetailsEURMTL,
		DetailsXLM:    prices.DetailsXLM,
		IsNFT:         isNFT,
	}

	// Check for manual valuation override
	val := valuation.LookupValuation(tb.Asset.Code, tb.Balance, accountID, accountValuations)
	if val != nil {
		resolved, err := s.external.ResolveValuation(ctx, *val)
		if err != nil {
			slog.Warn("manual valuation resolution failed, using market price",
				"token", tb.Asset.Code,
				"valuationType", val.ValuationType,
				"sourceAccount", val.SourceAccount,
				"error", err,
			)
		} else {
			result.PriceInEURMTL = &resolved.ValueInEURMTL
			if isNFT {
				result.ValueInEURMTL = &resolved.ValueInEURMTL
			} else {
				v := domain.MultiplyWithPrecision(tb.Balance, resolved.ValueInEURMTL)
				result.ValueInEURMTL = &v
			}
			result.NFTValuationAccount = val.SourceAccount

			// Derive XLM value from EURMTL valuation
			// Use XLM→EURMTL rate (xlmPriceInEURMTL) so that:
			//   priceInXLM = priceInEURMTL / xlmPriceInEURMTL
			xlmRate, xlmErr := s.price.GetPrice(ctx, domain.XLMAsset(), domain.EURMTLAsset(), "1")
			if xlmErr != nil {
				slog.Warn("failed to derive XLM price for valuation override", "token", tb.Asset.Code, "error", xlmErr)
			} else {
				xlmPrice := domain.DivideWithPrecision(resolved.ValueInEURMTL, xlmRate.Price)
				result.PriceInXLM = &xlmPrice
				if isNFT {
					result.ValueInXLM = &xlmPrice
				} else {
					xlmVal := domain.MultiplyWithPrecision(tb.Balance, xlmPrice)
					result.ValueInXLM = &xlmVal
				}
			}

			// Manual valuation resolved successfully; market price error is irrelevant
			priceErr = nil
		}
	}

	if priceErr != nil {
		return domain.TokenPriceWithBalance{}, priceErr
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
