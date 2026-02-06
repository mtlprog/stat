package fund

import (
	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// calculateAccountTotalEURMTL computes the total EURMTL value for an account.
func calculateAccountTotalEURMTL(tokens []domain.TokenPriceWithBalance, xlmBalance string, xlmPriceInEURMTL *string) float64 {
	total := lo.Reduce(tokens, func(acc decimal.Decimal, t domain.TokenPriceWithBalance, _ int) decimal.Decimal {
		if t.IsNFT {
			return domain.SafeSum(acc, domain.SafeParse(ptrToString(t.PriceInEURMTL)))
		}
		return domain.SafeSum(acc, domain.SafeMultiply(t.Balance, ptrToString(t.PriceInEURMTL)))
	}, decimal.Zero)

	// Add XLM value
	if xlmPriceInEURMTL != nil {
		xlmValue := domain.SafeMultiply(xlmBalance, *xlmPriceInEURMTL)
		total = domain.SafeSum(total, xlmValue)
	}

	f, _ := total.Float64()
	return f
}

// calculateAccountTotalXLM computes the total XLM value for an account.
func calculateAccountTotalXLM(tokens []domain.TokenPriceWithBalance, xlmBalance string) float64 {
	total := lo.Reduce(tokens, func(acc decimal.Decimal, t domain.TokenPriceWithBalance, _ int) decimal.Decimal {
		if t.IsNFT {
			return domain.SafeSum(acc, domain.SafeParse(ptrToString(t.PriceInXLM)))
		}
		return domain.SafeSum(acc, domain.SafeMultiply(t.Balance, ptrToString(t.PriceInXLM)))
	}, decimal.Zero)

	// Add XLM balance directly (it IS the native asset)
	xlm := domain.SafeParse(xlmBalance)
	total = domain.SafeSum(total, xlm)

	f, _ := total.Float64()
	return f
}

// calculateFundTotals computes aggregate fund totals from main accounts only.
func calculateFundTotals(accounts []domain.FundAccountPortfolio) domain.AggregatedTotals {
	totalEURMTL := lo.Reduce(accounts, func(acc float64, a domain.FundAccountPortfolio, _ int) float64 {
		return acc + a.TotalEURMTL
	}, 0.0)

	totalXLM := lo.Reduce(accounts, func(acc float64, a domain.FundAccountPortfolio, _ int) float64 {
		return acc + a.TotalXLM
	}, 0.0)

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

func ptrToString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
