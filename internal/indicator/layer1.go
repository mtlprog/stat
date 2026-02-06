package indicator

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// Layer1Calculator computes derived indicators (I3, I4, I5, I6, I7, I10, I49).
type Layer1Calculator struct {
	Horizon HorizonPriceSource
}

// HorizonPriceSource provides access to Horizon for orderbook queries.
type HorizonPriceSource interface {
	GetBidPrice(ctx context.Context, asset, baseAsset domain.AssetInfo) (decimal.Decimal, error)
}

func (c *Layer1Calculator) IDs() []int           { return []int{3, 4, 5, 6, 7, 10, 49} }
func (c *Layer1Calculator) Dependencies() []int  { return []int{51, 52, 53, 58, 59, 60} }

func (c *Layer1Calculator) Calculate(ctx context.Context, data domain.FundStructureData, deps map[int]Indicator, _ *HistoricalData) ([]Indicator, error) {
	// I3: Assets Value MTLF = I51 + I52 + I53 + I58 + I59 + I60
	i3 := deps[51].Value.Add(deps[52].Value).Add(deps[53].Value).
		Add(deps[58].Value).Add(deps[59].Value).Add(deps[60].Value)

	// I4: Operating Balance = sum of EURMTL + XLM across subfonds
	i4 := calculateOperatingBalance(data)

	// I6: MTL in circulation (simplified: total emitted - pools)
	i6 := decimal.NewFromInt(0)
	// I7: MTLRECT in circulation (simplified)
	i7 := decimal.NewFromInt(0)

	// I5: Total shares = I6 + I7
	i5 := i6.Add(i7)

	// I10: Share Market Price (MTL bid in EURMTL)
	i10 := decimal.Zero
	if c.Horizon != nil {
		mtlAsset := domain.AssetInfo{Code: "MTL", Issuer: domain.EURMTLAsset.Issuer, Type: domain.AssetTypeCreditAlphanum4}
		bid, err := c.Horizon.GetBidPrice(ctx, mtlAsset, domain.EURMTLAsset)
		if err == nil {
			i10 = bid
		}
	}

	// I49: MTLRECT Market Price
	i49 := decimal.Zero
	if c.Horizon != nil {
		mtlrectAsset := domain.AssetInfo{Code: "MTLRECT", Issuer: domain.EURMTLAsset.Issuer, Type: domain.AssetTypeCreditAlphanum12}
		bid, err := c.Horizon.GetBidPrice(ctx, mtlrectAsset, domain.EURMTLAsset)
		if err == nil {
			i49 = bid
		}
	}

	return []Indicator{
		{ID: 3, Name: "Assets Value MTLF", Value: i3, Unit: "EURMTL"},
		{ID: 4, Name: "Operating Balance", Value: i4, Unit: "EURMTL"},
		{ID: 5, Name: "Total Shares", Value: i5, Unit: "shares"},
		{ID: 6, Name: "MTL in Circulation", Value: i6, Unit: "MTL"},
		{ID: 7, Name: "MTLRECT in Circulation", Value: i7, Unit: "MTLRECT"},
		{ID: 10, Name: "Share Market Price", Value: i10, Unit: "EURMTL"},
		{ID: 49, Name: "MTLRECT Market Price", Value: i49, Unit: "EURMTL"},
	}, nil
}

func calculateOperatingBalance(data domain.FundStructureData) decimal.Decimal {
	total := decimal.Zero
	for _, acc := range data.Accounts {
		if acc.Type == domain.AccountTypeSubfond {
			// Sum EURMTL balance + XLM value
			for _, token := range acc.Tokens {
				if token.Asset.Code == "EURMTL" {
					total = total.Add(domain.SafeParse(token.Balance))
				}
			}
			xlmPrice := domain.SafeParse(ptrToStr(acc.XLMPriceInEURMTL))
			xlmBal := domain.SafeParse(acc.XLMBalance)
			total = total.Add(xlmBal.Mul(xlmPrice))
		}
	}
	return total
}

func ptrToStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
