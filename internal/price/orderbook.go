package price

import (
	"context"
	"log/slog"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/horizon"
)

// getOrderbookPrice attempts price discovery via direct orderbook + AMM.
func (s *Service) getOrderbookPrice(ctx context.Context, source, dest domain.AssetInfo) (domain.TokenPairPrice, error) {
	obData, err := s.fetchOrderbookData(ctx, source, dest)
	if err != nil {
		return domain.TokenPairPrice{}, err
	}

	// Select best price: bid preferred over ask (Section 3.3.2)
	var priceStr string
	var priceType string

	switch obData.BestSource {
	case "orderbook":
		if obData.Orderbook.Bid != nil {
			priceStr = *obData.Orderbook.Bid
			priceType = "bid"
		} else if obData.Orderbook.Ask != nil {
			priceStr = *obData.Orderbook.Ask
			priceType = "ask"
		}
	case "amm":
		if obData.AMM.Bid != nil {
			priceStr = *obData.AMM.Bid
			priceType = "bid"
		} else if obData.AMM.Ask != nil {
			priceStr = *obData.AMM.Ask
			priceType = "ask"
		}
	}

	if priceStr == "" {
		return domain.TokenPairPrice{}, ErrNoPrice
	}

	return domain.TokenPairPrice{
		TokenA:            source.Canonical(),
		TokenB:            dest.Canonical(),
		Price:             priceStr,
		DestinationAmount: priceStr,
		Timestamp:         time.Now(),
		Details: &domain.OrderbookDetails{
			Source:        "orderbook",
			PriceType:     priceType,
			OrderbookData: obData,
		},
	}, nil
}

// fetchOrderbookData retrieves orderbook + AMM data and selects the best source.
func (s *Service) fetchOrderbookData(ctx context.Context, source, dest domain.AssetInfo) (domain.OrderbookData, error) {
	data := domain.OrderbookData{BestSource: "none"}

	// Fetch traditional orderbook
	ob, err := s.horizon.FetchOrderbook(ctx, source, dest, 1)
	if err == nil {
		if len(ob.Asks) > 0 {
			data.Orderbook.Ask = &ob.Asks[0].Price
		}
		if len(ob.Bids) > 0 {
			data.Orderbook.Bid = &ob.Bids[0].Price
		}
	} else {
		slog.Warn("orderbook fetch failed", "source", source.Code, "dest", dest.Code, "error", err)
	}

	// Fetch AMM liquidity pool
	pools, poolErr := s.horizon.FetchLiquidityPools(ctx, source, dest)
	if poolErr != nil {
		slog.Warn("liquidity pool fetch failed", "source", source.Code, "dest", dest.Code, "error", poolErr)
	}
	if poolErr == nil && len(pools) > 0 {
		pool := pools[0]
		spot := calculateAMMSpot(pool, source)
		if spot != nil {
			data.AMM.Ask = spot
			data.AMM.Bid = spot
			data.AMMPoolID = &pool.ID
		}
	}

	// Select best source: lower ask wins (Section 3.3.4)
	obHasPrice := data.Orderbook.Ask != nil || data.Orderbook.Bid != nil
	ammHasPrice := data.AMM.Ask != nil

	switch {
	case obHasPrice && ammHasPrice:
		obAsk := parseDecimalOrZero(data.Orderbook.Ask)
		ammAsk := parseDecimalOrZero(data.AMM.Ask)
		if obAsk.LessThanOrEqual(ammAsk) {
			data.BestSource = "orderbook"
		} else {
			data.BestSource = "amm"
		}
	case obHasPrice:
		data.BestSource = "orderbook"
	case ammHasPrice:
		data.BestSource = "amm"
	}

	return data, nil
}

func calculateAMMSpot(pool horizon.HorizonLiquidityPool, source domain.AssetInfo) *string {
	if len(pool.Reserves) != 2 {
		return nil
	}

	var reserveA, reserveB decimal.Decimal
	sourceCanonical := source.Canonical()

	// Find which reserve matches the source asset
	for _, r := range pool.Reserves {
		amount, err := decimal.NewFromString(r.Amount)
		if err != nil || amount.IsZero() {
			return nil
		}
		// Pool reserve asset format: "CODE:ISSUER" or "native"
		if r.Asset == sourceCanonical {
			reserveA = amount
		} else {
			reserveB = amount
		}
	}

	if reserveA.IsZero() || reserveB.IsZero() {
		return nil
	}

	// spotPrice = reserveB / reserveA (Section 3.3.3)
	spot := reserveB.Div(reserveA).String()
	return &spot
}

func parseDecimalOrZero(s *string) decimal.Decimal {
	if s == nil {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		slog.Warn("unparseable price in orderbook comparison", "value", *s, "error", err)
		return decimal.Zero
	}
	return d
}
