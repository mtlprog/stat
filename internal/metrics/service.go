package metrics

import (
	"context"
	"log/slog"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// Horizon provides the Horizon API calls needed for live metric computation.
type Horizon interface {
	FetchAssetAmount(ctx context.Context, asset domain.AssetInfo) (decimal.Decimal, error)
	FetchAllPoolReservesForAsset(ctx context.Context, asset domain.AssetInfo) (decimal.Decimal, error)
	FetchMonthlyEURMTLOutflow(ctx context.Context, accountID string, fundAddresses []string) (decimal.Decimal, error)
}

// PriceSource provides market price lookups.
type PriceSource interface {
	GetBidPrice(ctx context.Context, asset, baseAsset domain.AssetInfo) (decimal.Decimal, error)
}

// Service computes live metrics and injects them into FundStructureData at snapshot generation time,
// enabling accurate period-over-period comparison without live Horizon calls on historical snapshots.
type Service struct {
	horizon   Horizon
	price     PriceSource
	fundAddrs []string
}

// NewService creates a new metrics Service.
func NewService(h Horizon, p PriceSource, fundAddrs []string) *Service {
	return &Service{horizon: h, price: p, fundAddrs: fundAddrs}
}

// EnrichMetrics computes I10, I6, I7, and I11 and stores them in data.LiveMetrics.
// Errors are logged and skipped; partial metrics are still stored.
func (s *Service) EnrichMetrics(ctx context.Context, data *domain.FundStructureData) error {
	m := &domain.FundLiveMetrics{}

	// I10: MTL market price (bid price on DEX)
	mtlAsset := domain.NewAssetInfo("MTL", domain.IssuerAddress)
	if bid, err := s.price.GetBidPrice(ctx, mtlAsset, domain.EURMTLAsset()); err != nil {
		slog.Warn("metrics: failed to fetch MTL bid price", "error", err)
	} else {
		v := bid.String()
		m.MTLMarketPrice = &v
	}

	// I6: MTL in circulation = total supply - AMM pool reserves
	if c, err := s.fetchCirculation(ctx, mtlAsset); err != nil {
		slog.Warn("metrics: failed to compute MTL circulation", "error", err)
	} else {
		v := c.String()
		m.MTLCirculation = &v
	}

	// I7: MTLRECT in circulation = total supply - AMM pool reserves
	mtlrectAsset := domain.NewAssetInfo("MTLRECT", domain.IssuerAddress)
	if c, err := s.fetchCirculation(ctx, mtlrectAsset); err != nil {
		slog.Warn("metrics: failed to compute MTLRECT circulation", "error", err)
	} else {
		v := c.String()
		m.MTLRECTCirculation = &v
	}

	// I11: Monthly dividends (outgoing EURMTL from issuer to non-fund accounts, last 30 days)
	if divs, err := s.horizon.FetchMonthlyEURMTLOutflow(ctx, domain.IssuerAddress, s.fundAddrs); err != nil {
		slog.Warn("metrics: failed to fetch monthly dividends", "error", err)
	} else {
		v := divs.String()
		m.MonthlyDividends = &v
	}

	data.LiveMetrics = m
	return nil
}

func (s *Service) fetchCirculation(ctx context.Context, asset domain.AssetInfo) (decimal.Decimal, error) {
	total, err := s.horizon.FetchAssetAmount(ctx, asset)
	if err != nil {
		return decimal.Zero, err
	}
	pools, err := s.horizon.FetchAllPoolReservesForAsset(ctx, asset)
	if err != nil {
		return decimal.Zero, err
	}
	c := total.Sub(pools)
	if c.IsNegative() {
		return decimal.Zero, nil
	}
	return c, nil
}
