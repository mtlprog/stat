package domain

import "github.com/shopspring/decimal"

// FundAccountPortfolio represents a fully priced and valued account portfolio.
type FundAccountPortfolio struct {
	ID               string                  `json:"id"`
	Name             string                  `json:"name"`
	Type             AccountType             `json:"type"`
	Description      string                  `json:"description"`
	Tokens           []TokenPriceWithBalance `json:"tokens"`
	XLMBalance       string                  `json:"xlmBalance"`
	XLMPriceInEURMTL *string                 `json:"xlmPriceInEURMTL"`
	TotalEURMTL      decimal.Decimal         `json:"totalEURMTL"`
	TotalXLM         decimal.Decimal         `json:"totalXLM"`
}

// AggregatedTotals holds the fund-level totals (excluding mutual and other accounts).
type AggregatedTotals struct {
	TotalEURMTL  decimal.Decimal `json:"totalEURMTL"`
	TotalXLM     decimal.Decimal `json:"totalXLM"`
	AccountCount int             `json:"accountCount"`
	TokenCount   int             `json:"tokenCount"`
}

// FundStructureData is the top-level output of the fund aggregation pipeline.
type FundStructureData struct {
	Accounts         []FundAccountPortfolio `json:"accounts"`
	MutualFunds      []FundAccountPortfolio `json:"mutualFunds"`
	OtherAccounts    []FundAccountPortfolio `json:"otherAccounts"`
	AggregatedTotals AggregatedTotals       `json:"aggregatedTotals"`
}
