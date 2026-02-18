package indicator

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/snapshot"
)

// DividendHorizon provides access to Horizon for dividend calculations.
type DividendHorizon interface {
	FetchMonthlyEURMTLOutflow(ctx context.Context, accountID string, fundAddresses []string) (decimal.Decimal, error)
}

// DividendCalculator computes dividend-related indicators (I11, I15, I16, I17, I33, I34, I54, I55).
type DividendCalculator struct {
	Horizon DividendHorizon
}

func (c *DividendCalculator) IDs() []int          { return []int{11, 15, 16, 17, 33, 34, 54, 55} }
func (c *DividendCalculator) Dependencies() []int { return []int{5, 10} }

func (c *DividendCalculator) Calculate(ctx context.Context, _ domain.FundStructureData, deps map[int]Indicator, hist *HistoricalData) ([]Indicator, error) {
	i5 := deps[5].Value   // Total Shares
	i10 := deps[10].Value // Share Market Price

	// I11: Monthly Dividends — outgoing EURMTL from MAIN ISSUER to non-fund accounts, last 30 days
	i11 := decimal.Zero
	if c.Horizon != nil {
		fundAddresses := lo.Map(domain.AccountRegistry(), func(a domain.FundAccount, _ int) string { return a.Address })
		amt, err := c.Horizon.FetchMonthlyEURMTLOutflow(ctx, domain.IssuerAddress, fundAddresses)
		if err != nil {
			slog.Warn("failed to fetch monthly EURMTL outflow", "error", err)
		} else {
			i11 = amt
		}
	}

	// I15: DPS = I11 / I5
	i15 := decimal.Zero
	if !i5.IsZero() {
		i15 = i11.Div(i5)
	}

	// I55: Price Year Ago — scan MTL token price from snapshot 365 days ago
	i55 := decimal.Zero
	if hist != nil {
		i55 = fetchPriceYearAgo(ctx, hist)
	}

	// I54: Annual DPS = I15 * 12 (annualized monthly DPS)
	i54 := i15.Mul(decimal.NewFromInt(12))

	// I16: ADY1 (placeholder — formula not yet defined by client)
	i16 := decimal.Zero

	// I17: ADY2 = (I54 / I55) * 100
	i17 := decimal.Zero
	if !i55.IsZero() {
		i17 = i54.Div(i55).Mul(decimal.NewFromInt(100))
	}

	// I33: EPS (placeholder — returns zero, requires median of monthly dividends)
	i33 := decimal.Zero

	// I34: P/E = I10 / I54
	i34 := decimal.Zero
	if !i54.IsZero() {
		i34 = i10.Div(i54)
	}

	return []Indicator{
		NewIndicator(11, i11, "", ""),
		NewIndicator(15, i15, "", ""),
		NewIndicator(16, i16, "", ""),
		NewIndicator(17, i17, "", ""),
		NewIndicator(33, i33, "", ""),
		NewIndicator(34, i34, "", ""),
		NewIndicator(54, i54, "", ""),
		NewIndicator(55, i55, "", ""),
	}, nil
}

// fetchPriceYearAgo retrieves the MTL price from the snapshot taken 365 days ago.
// It scans stored token prices in the snapshot (analogous to findBTCPrice in layer0).
func fetchPriceYearAgo(ctx context.Context, hist *HistoricalData) decimal.Decimal {
	yearAgo := time.Now().AddDate(-1, 0, 0)
	snap, err := hist.Repo.GetByDate(ctx, hist.Slug, yearAgo)
	if err != nil {
		if err != snapshot.ErrNotFound {
			slog.Warn("failed to fetch historical snapshot for price year ago", "date", yearAgo.Format("2006-01-02"), "error", err)
		}
		return decimal.Zero
	}

	var historicalData domain.FundStructureData
	if err := json.Unmarshal(snap.Data, &historicalData); err != nil {
		slog.Warn("failed to parse historical snapshot data", "error", err)
		return decimal.Zero
	}

	return findMTLPrice(historicalData)
}

// findMTLPrice scans all accounts in the snapshot for a stored MTL token price.
func findMTLPrice(data domain.FundStructureData) decimal.Decimal {
	allAccounts := lo.Flatten([][]domain.FundAccountPortfolio{
		data.Accounts,
		data.MutualFunds,
		data.OtherAccounts,
	})
	for _, acc := range allAccounts {
		for _, token := range acc.Tokens {
			if token.Asset.Code == "MTL" && token.Asset.Issuer == domain.IssuerAddress && token.PriceInEURMTL != nil {
				price := domain.SafeParse(*token.PriceInEURMTL)
				if !price.IsZero() {
					return price
				}
			}
		}
	}
	return decimal.Zero
}
