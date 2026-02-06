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

// TokenPriceResult holds the EURMTL and XLM prices/values for a token.
type TokenPriceResult struct {
	PriceEURMTL   string
	PriceXLM      string
	ValueEURMTL   string
	ValueXLM      string
	DetailsEURMTL *domain.PriceDetails
	DetailsXLM    *domain.PriceDetails
}

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

// GetBidPrice fetches the best bid price from the orderbook for the given asset pair.
func (s *Service) GetBidPrice(ctx context.Context, asset, baseAsset domain.AssetInfo) (decimal.Decimal, error) {
	ob, err := s.horizon.FetchOrderbook(ctx, asset, baseAsset, 1)
	if err != nil {
		return decimal.Zero, fmt.Errorf("fetching orderbook for bid: %w", err)
	}
	if len(ob.Bids) == 0 {
		return decimal.Zero, ErrNoPrice
	}
	price, err := decimal.NewFromString(ob.Bids[0].Price)
	if err != nil {
		return decimal.Zero, fmt.Errorf("parsing bid price: %w", err)
	}
	return price, nil
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
			Details: &domain.PriceDetails{
				Source:         "best",
				PathPrice:      &pathPriceStr,
				OrderbookPrice: &obPriceStr,
				ChosenSource:   "path",
				PathSubDetails: pathResult.price.Details,
				OBSubDetails:   obResult.price.Details,
			},
		}, nil
	}

	var priceType string
	if obResult.price.Details != nil {
		priceType = obResult.price.Details.PriceType
	}

	return domain.TokenPairPrice{
		TokenA:            obResult.price.TokenA,
		TokenB:            obResult.price.TokenB,
		Price:             obResult.price.Price,
		DestinationAmount: obResult.price.DestinationAmount,
		Timestamp:         time.Now(),
		Details: &domain.PriceDetails{
			Source:         "best",
			PriceType:      priceType,
			PathPrice:      &pathPriceStr,
			OrderbookPrice: &obPriceStr,
			ChosenSource:   "orderbook",
			PathSubDetails: pathResult.price.Details,
			OBSubDetails:   obResult.price.Details,
		},
	}, nil
}

// GetTokenPrices returns EURMTL and XLM prices/values for a token, including cross-rate derivation.
func (s *Service) GetTokenPrices(ctx context.Context, asset domain.AssetInfo, balance string) (TokenPriceResult, error) {
	var result TokenPriceResult

	eurmtlResult, eurmtlErr := s.GetPrice(ctx, asset, domain.EURMTLAsset(), "1")
	if eurmtlErr == nil {
		result.PriceEURMTL = eurmtlResult.Price
		result.DetailsEURMTL = eurmtlResult.Details
	}

	xlmResult, xlmErr := s.GetPrice(ctx, asset, domain.XLMAsset(), "1")
	if xlmErr == nil {
		result.PriceXLM = xlmResult.Price
		result.DetailsXLM = xlmResult.Details
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
					eurmtlPrice, parseErr := decimal.NewFromString(result.PriceEURMTL)
					if parseErr != nil {
						slog.Warn("cross-rate: EURMTL price unparseable", "asset", asset.Code, "price", result.PriceEURMTL, "error", parseErr)
					} else {
						result.PriceXLM = eurmtlPrice.Mul(rate).String()
					}
				} else {
					// Have XLM, derive EURMTL
					xlmPrice, parseErr := decimal.NewFromString(result.PriceXLM)
					if parseErr != nil {
						slog.Warn("cross-rate: XLM price unparseable", "asset", asset.Code, "price", result.PriceXLM, "error", parseErr)
					} else {
						result.PriceEURMTL = xlmPrice.Div(rate).String()
					}
				}
			}
		}
	}

	if result.PriceEURMTL != "" {
		result.ValueEURMTL = domain.MultiplyWithPrecision(result.PriceEURMTL, balance)
	}
	if result.PriceXLM != "" {
		result.ValueXLM = domain.MultiplyWithPrecision(result.PriceXLM, balance)
	}

	if eurmtlErr != nil && xlmErr != nil {
		return TokenPriceResult{}, fmt.Errorf("both EURMTL and XLM price lookups failed: eurmtl: %w, xlm: %v", eurmtlErr, xlmErr)
	}

	return result, nil
}
