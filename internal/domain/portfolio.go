package domain

// TokenBalance represents a single token balance on a Stellar account.
type TokenBalance struct {
	Asset   AssetInfo `json:"asset"`
	Balance string    `json:"balance"`
	Limit   string    `json:"limit,omitempty"`
}

// AccountPortfolio holds the raw balances for a Stellar account.
type AccountPortfolio struct {
	AccountID  string         `json:"accountId"`
	Tokens     []TokenBalance `json:"tokens"`
	XLMBalance string         `json:"xlmBalance"`
}

// TokenPriceWithBalance combines a token balance with its market price and value.
type TokenPriceWithBalance struct {
	Asset               AssetInfo    `json:"asset"`
	Balance             string       `json:"balance"`
	PriceInEURMTL       *string      `json:"priceInEURMTL"`
	PriceInXLM          *string      `json:"priceInXLM"`
	ValueInEURMTL       *string      `json:"valueInEURMTL"`
	ValueInXLM          *string      `json:"valueInXLM"`
	DetailsEURMTL       PriceDetails `json:"detailsEURMTL,omitempty"`
	DetailsXLM          PriceDetails `json:"detailsXLM,omitempty"`
	IsNFT               bool         `json:"isNFT,omitempty"`
	NFTValuationAccount string       `json:"nftValuationAccount,omitempty"`
}
