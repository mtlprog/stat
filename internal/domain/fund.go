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

// FundLiveMetrics stores live-computed metrics captured at snapshot generation time.
// Indicator calculators read these values exclusively — they do not call Horizon.
// This makes snapshots fully reproducible and keeps the report runtime bounded.
type FundLiveMetrics struct {
	MTLMarketPrice        *string `json:"mtl_market_price,omitempty"`        // I10
	MTLRECTMarketPrice    *string `json:"mtlrect_market_price,omitempty"`    // I49
	MTLCirculation        *string `json:"mtl_circulation,omitempty"`         // I6
	MTLRECTCirculation    *string `json:"mtlrect_circulation,omitempty"`     // I7
	MonthlyDividends      *string `json:"monthly_dividends,omitempty"`       // I11
	EURMTLDailyVolume     *string `json:"eurmtl_daily_volume,omitempty"`     // I25
	EURMTL30dVolume       *string `json:"eurmtl_30d_volume,omitempty"`       // I26
	EURMTLParticipants    *string `json:"eurmtl_participants,omitempty"`     // I24
	MTLShareholders       *string `json:"mtl_shareholders,omitempty"`        // I27
	MTLShareholdersMedian *string `json:"mtl_shareholders_median,omitempty"` // I23
	MTLAPHolders          *string `json:"mtlap_holders,omitempty"`           // I40
}

// FundStructureData is the top-level output of the fund aggregation pipeline.
type FundStructureData struct {
	Accounts         []FundAccountPortfolio `json:"accounts"`
	MutualFunds      []FundAccountPortfolio `json:"mutualFunds"`
	OtherAccounts    []FundAccountPortfolio `json:"otherAccounts"`
	AggregatedTotals AggregatedTotals       `json:"aggregatedTotals"`
	Warnings         []string               `json:"warnings,omitempty"`
	LiveMetrics      *FundLiveMetrics       `json:"live_metrics,omitempty"`
}
