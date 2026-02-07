package fund

import (
	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// calculateAccountTotalEURMTL computes the total EURMTL value for an account.
// For NFTs (balance=0.0000001), PriceInEURMTL holds the total valuation (from the NFT valuation account),
// so it is summed directly without multiplication.
// For regular tokens, multiplies balance by unit price. Also adds XLM value if the EURMTL rate is available.
func calculateAccountTotalEURMTL(tokens []domain.TokenPriceWithBalance, xlmBalance string, xlmPriceInEURMTL *string) decimal.Decimal {
	total := lo.Reduce(tokens, func(acc decimal.Decimal, t domain.TokenPriceWithBalance, _ int) decimal.Decimal {
		if t.IsNFT {
			return domain.SafeSum(acc, domain.SafeParse(lo.FromPtr(t.PriceInEURMTL)))
		}
		return domain.SafeSum(acc, domain.SafeMultiply(t.Balance, lo.FromPtr(t.PriceInEURMTL)))
	}, decimal.Zero)

	// Add XLM value
	if xlmPriceInEURMTL != nil {
		xlmValue := domain.SafeMultiply(xlmBalance, *xlmPriceInEURMTL)
		total = domain.SafeSum(total, xlmValue)
	}

	return total
}

// calculateAccountTotalXLM computes the total XLM value for an account.
func calculateAccountTotalXLM(tokens []domain.TokenPriceWithBalance, xlmBalance string) decimal.Decimal {
	total := lo.Reduce(tokens, func(acc decimal.Decimal, t domain.TokenPriceWithBalance, _ int) decimal.Decimal {
		if t.IsNFT {
			return domain.SafeSum(acc, domain.SafeParse(lo.FromPtr(t.PriceInXLM)))
		}
		return domain.SafeSum(acc, domain.SafeMultiply(t.Balance, lo.FromPtr(t.PriceInXLM)))
	}, decimal.Zero)

	// Add XLM balance directly (it IS the native asset)
	xlm := domain.SafeParse(xlmBalance)
	total = domain.SafeSum(total, xlm)

	return total
}

// calculateFundTotals computes aggregate fund totals from main accounts only.
func calculateFundTotals(accounts []domain.FundAccountPortfolio) domain.AggregatedTotals {
	totalEURMTL := lo.Reduce(accounts, func(acc decimal.Decimal, a domain.FundAccountPortfolio, _ int) decimal.Decimal {
		return acc.Add(a.TotalEURMTL)
	}, decimal.Zero)

	totalXLM := lo.Reduce(accounts, func(acc decimal.Decimal, a domain.FundAccountPortfolio, _ int) decimal.Decimal {
		return acc.Add(a.TotalXLM)
	}, decimal.Zero)

	tokenCount := lo.Reduce(accounts, func(acc int, a domain.FundAccountPortfolio, _ int) int {
		return acc + len(a.Tokens)
	}, 0)

	return domain.AggregatedTotals{
		TotalEURMTL:  totalEURMTL,
		TotalXLM:     totalXLM,
		AccountCount: len(accounts),
		TokenCount:   tokenCount,
	}
}
