package price

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// ErrNoPrice indicates that no price could be determined.
var ErrNoPrice = errors.New("no price available")

// Service implements token price discovery.
type Service struct {
	horizon HorizonClient
	cache   *priceCache
}

// NewService creates a new PriceService.
func NewService(horizon HorizonClient) *Service {
	return &Service{
		horizon: horizon,
		cache:   newPriceCache(),
	}
}

// GetPrice determines the price of `asset` in terms of `baseAsset`.
// For amount="1" (spot price), both path finding and orderbook are queried; the higher price wins.
// For amount!="1" (full balance), only path finding is used.
func (s *Service) GetPrice(ctx context.Context, asset, baseAsset domain.AssetInfo, amount string) (domain.TokenPairPrice, error) {
	key := cacheKey(asset, baseAsset, amount)
	if cached, ok := s.cache.get(key); ok {
		return cached, nil
	}

	var result domain.TokenPairPrice
	var err error

	if amount == "1" {
		result, err = s.getSpotPrice(ctx, asset, baseAsset)
	} else {
		result, err = s.getPathPrice(ctx, asset, baseAsset, amount)
	}

	if err != nil {
		return domain.TokenPairPrice{}, err
	}

	s.cache.set(key, result)
	return result, nil
}

// getSpotPrice queries both path finding and orderbook, returning the higher price.
func (s *Service) getSpotPrice(ctx context.Context, asset, baseAsset domain.AssetInfo) (domain.TokenPairPrice, error) {
	type priceResult struct {
		price domain.TokenPairPrice
		err   error
	}

	pathCh := make(chan priceResult, 1)
	obCh := make(chan priceResult, 1)

	go func() {
		p, err := s.getPathPrice(ctx, asset, baseAsset, "1")
		pathCh <- priceResult{p, err}
	}()

	go func() {
		p, err := s.getOrderbookPrice(ctx, asset, baseAsset)
		obCh <- priceResult{p, err}
	}()

	pathResult := <-pathCh
	obResult := <-obCh

	pathOK := pathResult.err == nil
	obOK := obResult.err == nil

	if !pathOK && !obOK {
		if pathResult.err != nil {
			return domain.TokenPairPrice{}, pathResult.err
		}
		return domain.TokenPairPrice{}, obResult.err
	}

	if pathOK && !obOK {
		return pathResult.price, nil
	}
	if !pathOK && obOK {
		return obResult.price, nil
	}

	// Both succeeded: choose the higher price
	pathPrice, pathParseErr := decimal.NewFromString(pathResult.price.Price)
	obPrice, obParseErr := decimal.NewFromString(obResult.price.Price)

	// If one price is unparseable, prefer the other
	if pathParseErr != nil && obParseErr != nil {
		return domain.TokenPairPrice{}, fmt.Errorf("both price sources returned unparseable prices")
	}
	if pathParseErr != nil {
		return obResult.price, nil
	}
	if obParseErr != nil {
		return pathResult.price, nil
	}

	pathPriceStr := pathResult.price.Price
	obPriceStr := obResult.price.Price

	if pathPrice.GreaterThanOrEqual(obPrice) {
		return domain.TokenPairPrice{
			TokenA:            pathResult.price.TokenA,
			TokenB:            pathResult.price.TokenB,
			Price:             pathResult.price.Price,
			DestinationAmount: pathResult.price.DestinationAmount,
			Timestamp:         time.Now(),
			Details: &domain.BestDetails{
				Source:           "best",
				PathPrice:        &pathPriceStr,
				OrderbookPrice:   &obPriceStr,
				ChosenSource:     "path",
				PathDetails:      toPathDetails(pathResult.price.Details),
				OrderbookDetails: toOrderbookDetails(obResult.price.Details),
			},
		}, nil
	}

	obDetails := toOrderbookDetails(obResult.price.Details)
	var priceType string
	if obDetails != nil {
		priceType = obDetails.PriceType
	}

	return domain.TokenPairPrice{
		TokenA:            obResult.price.TokenA,
		TokenB:            obResult.price.TokenB,
		Price:             obResult.price.Price,
		DestinationAmount: obResult.price.DestinationAmount,
		Timestamp:         time.Now(),
		Details: &domain.BestDetails{
			Source:           "best",
			PriceType:        priceType,
			PathPrice:        &pathPriceStr,
			OrderbookPrice:   &obPriceStr,
			ChosenSource:     "orderbook",
			PathDetails:      toPathDetails(pathResult.price.Details),
			OrderbookDetails: obDetails,
		},
	}, nil
}

// GetTokenPrices returns EURMTL and XLM prices/values for a token, including cross-rate derivation.
func (s *Service) GetTokenPrices(ctx context.Context, asset domain.AssetInfo, balance string) (
	priceEURMTL, priceXLM, valueEURMTL, valueXLM string,
	detailsEURMTL, detailsXLM domain.PriceDetails,
	err error,
) {
	eurmtlResult, eurmtlErr := s.GetPrice(ctx, asset, domain.EURMTLAsset(), "1")
	if eurmtlErr == nil {
		priceEURMTL = eurmtlResult.Price
		detailsEURMTL = eurmtlResult.Details
	}

	xlmResult, xlmErr := s.GetPrice(ctx, asset, domain.XLMAsset(), "1")
	if xlmErr == nil {
		priceXLM = xlmResult.Price
		detailsXLM = xlmResult.Details
	}

	// Cross-rate calculation: derive missing price via EURMTL/XLM rate
	if (eurmtlErr == nil && xlmErr != nil) || (eurmtlErr != nil && xlmErr == nil) {
		crossRate, crossErr := s.GetPrice(ctx, domain.EURMTLAsset(), domain.XLMAsset(), "1")
		if crossErr == nil {
			rate, rateErr := decimal.NewFromString(crossRate.Price)
			if rateErr != nil {
				slog.Warn("cross-rate price unparseable", "price", crossRate.Price, "error", rateErr)
			} else if !rate.IsZero() {
				if eurmtlErr == nil && xlmErr != nil {
					// Have EURMTL, derive XLM
					eurmtlPrice, parseErr := decimal.NewFromString(priceEURMTL)
					if parseErr != nil {
						slog.Warn("cross-rate: EURMTL price unparseable", "asset", asset.Code, "price", priceEURMTL, "error", parseErr)
					} else {
						priceXLM = eurmtlPrice.Mul(rate).String()
					}
				} else {
					// Have XLM, derive EURMTL
					xlmPrice, parseErr := decimal.NewFromString(priceXLM)
					if parseErr != nil {
						slog.Warn("cross-rate: XLM price unparseable", "asset", asset.Code, "price", priceXLM, "error", parseErr)
					} else {
						priceEURMTL = xlmPrice.Div(rate).String()
					}
				}
			}
		}
	}

	if priceEURMTL != "" {
		valueEURMTL = domain.MultiplyWithPrecision(priceEURMTL, balance)
	}
	if priceXLM != "" {
		valueXLM = domain.MultiplyWithPrecision(priceXLM, balance)
	}

	// Get full-balance value if amount != 1
	bal, balErr := decimal.NewFromString(balance)
	if balErr != nil {
		slog.Warn("unparseable balance", "asset", asset.Code, "balance", balance, "error", balErr)
	}
	if balErr == nil && !bal.IsZero() && !bal.Equal(decimal.NewFromInt(1)) {
		if priceEURMTL != "" {
			fullResult, fullErr := s.GetPrice(ctx, asset, domain.EURMTLAsset(), balance)
			if fullErr == nil {
				valueEURMTL = fullResult.DestinationAmount
			}
		}
		if priceXLM != "" {
			fullResult, fullErr := s.GetPrice(ctx, asset, domain.XLMAsset(), balance)
			if fullErr == nil {
				valueXLM = fullResult.DestinationAmount
			}
		}
	}

	if eurmtlErr != nil && xlmErr != nil {
		return "", "", "", "", nil, nil, fmt.Errorf("both EURMTL and XLM price lookups failed: eurmtl: %w, xlm: %v", eurmtlErr, xlmErr)
	}

	return priceEURMTL, priceXLM, valueEURMTL, valueXLM, detailsEURMTL, detailsXLM, nil
}

func toPathDetails(d domain.PriceDetails) *domain.PathDetails {
	if d == nil {
		return nil
	}
	if pd, ok := d.(*domain.PathDetails); ok {
		return pd
	}
	return nil
}

func toOrderbookDetails(d domain.PriceDetails) *domain.OrderbookDetails {
	if d == nil {
		return nil
	}
	if od, ok := d.(*domain.OrderbookDetails); ok {
		return od
	}
	return nil
}
