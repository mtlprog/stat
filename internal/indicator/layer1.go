package indicator

import (
	"context"
	"log/slog"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// Layer1Calculator computes derived indicators (I3, I4, I5, I6, I7, I10, I49).
type Layer1Calculator struct {
	Horizon     HorizonPriceSource
	Circulation CirculationHorizon
}

// HorizonPriceSource provides access to Horizon for orderbook queries.
type HorizonPriceSource interface {
	GetBidPrice(ctx context.Context, asset, baseAsset domain.AssetInfo) (decimal.Decimal, error)
}

// CirculationHorizon provides access to Horizon for asset circulation data.
type CirculationHorizon interface {
	FetchAssetAmount(ctx context.Context, asset domain.AssetInfo) (decimal.Decimal, error)
	FetchAccountBalance(ctx context.Context, accountID string, asset domain.AssetInfo) (decimal.Decimal, error)
	FetchAllPoolReservesForAsset(ctx context.Context, asset domain.AssetInfo) (decimal.Decimal, error)
}

func (c *Layer1Calculator) IDs() []int          { return []int{3, 4, 5, 6, 7, 10, 49} }
func (c *Layer1Calculator) Dependencies() []int { return []int{51, 52, 53, 58, 59, 60} }

func (c *Layer1Calculator) Calculate(ctx context.Context, data domain.FundStructureData, deps map[int]Indicator, _ *HistoricalData) ([]Indicator, error) {
	// I3: Assets Value MTLF = I51 + I52 + I53 + I58 + I59 + I60
	i3 := deps[51].Value.Add(deps[52].Value).Add(deps[53].Value).
		Add(deps[58].Value).Add(deps[59].Value).Add(deps[60].Value)

	// I4: Operating Balance = sum of (EURMTL balances + XLM balances converted to EURMTL) across subfond accounts
	i4 := calculateOperatingBalance(data)

	// I6: MTL in circulation = total supply - issuer balance - AMM pool reserves
	i6 := fetchCirculation(ctx, c.Circulation, domain.NewAssetInfo("MTL", domain.IssuerAddress))

	// I7: MTLRECT in circulation = total supply - issuer balance - AMM pool reserves
	i7 := fetchCirculation(ctx, c.Circulation, domain.NewAssetInfo("MTLRECT", domain.IssuerAddress))

	// I5: Total shares = I6 + I7
	i5 := i6.Add(i7)

	// I10: Share Market Price (MTL bid in EURMTL)
	i10 := decimal.Zero
	if c.Horizon != nil {
		mtlAsset := domain.AssetInfo{Code: "MTL", Issuer: domain.IssuerAddress, Type: domain.AssetTypeCreditAlphanum4}
		bid, err := c.Horizon.GetBidPrice(ctx, mtlAsset, domain.EURMTLAsset())
		if err != nil {
			slog.Warn("failed to fetch bid price", "asset", "MTL", "error", err)
		} else {
			i10 = bid
		}
	}

	// I49: MTLRECT Market Price
	i49 := decimal.Zero
	if c.Horizon != nil {
		mtlrectAsset := domain.AssetInfo{Code: "MTLRECT", Issuer: domain.IssuerAddress, Type: domain.AssetTypeCreditAlphanum12}
		bid, err := c.Horizon.GetBidPrice(ctx, mtlrectAsset, domain.EURMTLAsset())
		if err != nil {
			slog.Warn("failed to fetch bid price", "asset", "MTLRECT", "error", err)
		} else {
			i49 = bid
		}
	}

	return []Indicator{
		NewIndicator(3, i3, "", ""),
		NewIndicator(4, i4, "", ""),
		NewIndicator(5, i5, "", ""),
		NewIndicator(6, i6, "", ""),
		NewIndicator(7, i7, "", ""),
		NewIndicator(10, i10, "", ""),
		NewIndicator(49, i49, "", ""),
	}, nil
}

// fetchCirculation computes the circulating supply of an asset:
// total issued - issuer's own balance - AMM pool reserves.
func fetchCirculation(ctx context.Context, h CirculationHorizon, asset domain.AssetInfo) decimal.Decimal {
	if h == nil {
		return decimal.Zero
	}

	total, err := h.FetchAssetAmount(ctx, asset)
	if err != nil {
		slog.Warn("failed to fetch asset total supply", "asset", asset.Code, "error", err)
		return decimal.Zero
	}

	issuerBal, err := h.FetchAccountBalance(ctx, domain.IssuerAddress, asset)
	if err != nil {
		slog.Warn("failed to fetch issuer asset balance", "asset", asset.Code, "error", err)
		return decimal.Zero
	}

	poolReserves, err := h.FetchAllPoolReservesForAsset(ctx, asset)
	if err != nil {
		slog.Warn("failed to fetch pool reserves", "asset", asset.Code, "error", err)
		return decimal.Zero
	}

	circulation := total.Sub(issuerBal).Sub(poolReserves)
	if circulation.IsNegative() {
		return decimal.Zero
	}
	return circulation
}

func calculateOperatingBalance(data domain.FundStructureData) decimal.Decimal {
	total := decimal.Zero
	for _, acc := range data.Accounts {
		if acc.Type == domain.AccountTypeSubfond {
			for _, token := range acc.Tokens {
				if token.Asset.Code == "EURMTL" {
					total = total.Add(domain.SafeParse(token.Balance))
				}
			}
			xlmPrice := domain.SafeParse(lo.FromPtr(acc.XLMPriceInEURMTL))
			xlmBal := domain.SafeParse(acc.XLMBalance)
			total = total.Add(xlmBal.Mul(xlmPrice))
		}
	}
	return total
}
