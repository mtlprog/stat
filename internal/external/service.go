package external

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// Service manages external price quotes and resolves asset valuations.
type Service struct {
	coingecko *CoinGeckoClient
	repo      QuoteRepository
}

// NewService creates a new ExternalPriceService.
func NewService(coingecko *CoinGeckoClient, repo QuoteRepository) *Service {
	return &Service{
		coingecko: coingecko,
		repo:      repo,
	}
}

// FetchAndStoreQuotes fetches all external prices from CoinGecko and stores them in the database.
func (s *Service) FetchAndStoreQuotes(ctx context.Context) error {
	prices, err := s.coingecko.FetchPrices(ctx)
	if err != nil {
		return fmt.Errorf("fetching external prices: %w", err)
	}

	for symbol, priceInEUR := range prices {
		if err := s.repo.SaveQuote(ctx, symbol, priceInEUR); err != nil {
			return fmt.Errorf("storing quote for %s: %w", symbol, err)
		}
	}

	return nil
}

// ResolveValuation resolves an asset valuation to a EURMTL value using stored external quotes.
func (s *Service) ResolveValuation(ctx context.Context, val domain.AssetValuation) (domain.ResolvedAssetValuation, error) {
	resolved := domain.ResolvedAssetValuation{AssetValuation: val}

	switch val.RawValue.Type {
	case domain.ValuationValueEURMTL:
		// Direct EURMTL value
		resolved.ValueInEURMTL = val.RawValue.Value
		return resolved, nil

	case domain.ValuationValueExternal:
		quote, err := s.repo.GetQuote(ctx, val.RawValue.Symbol)
		if err != nil {
			return domain.ResolvedAssetValuation{}, fmt.Errorf("getting quote for %s: %w", val.RawValue.Symbol, err)
		}

		priceInEUR := quote.PriceInEUR

		// For compound values (e.g., "AU 1g"), multiply by quantity
		if val.RawValue.Quantity != nil {
			qty := decimal.NewFromFloat(*val.RawValue.Quantity)
			priceInEUR = priceInEUR.Mul(qty)
		}

		resolved.ValueInEURMTL = priceInEUR.String()
		return resolved, nil

	default:
		return domain.ResolvedAssetValuation{}, fmt.Errorf("unknown valuation value type: %s", val.RawValue.Type)
	}
}
