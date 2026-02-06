package horizon

// HorizonAccount represents the JSON response from GET /accounts/{id}.
type HorizonAccount struct {
	ID       string           `json:"id"`
	Balances []HorizonBalance `json:"balances"`
	Data     map[string]string `json:"data"`
}

// HorizonBalance represents a single balance entry in an account response.
type HorizonBalance struct {
	AssetType          string `json:"asset_type"`
	AssetCode          string `json:"asset_code"`
	AssetIssuer        string `json:"asset_issuer"`
	Balance            string `json:"balance"`
	Limit              string `json:"limit,omitempty"`
	LiquidityPoolID    string `json:"liquidity_pool_id,omitempty"`
}

// HorizonOrderbook represents the JSON response from GET /order_book.
type HorizonOrderbook struct {
	Bids []HorizonOrderbookEntry `json:"bids"`
	Asks []HorizonOrderbookEntry `json:"asks"`
}

// HorizonOrderbookEntry represents a single bid or ask in an orderbook.
type HorizonOrderbookEntry struct {
	Price  string `json:"price"`
	Amount string `json:"amount"`
}

// HorizonPathRecord represents a single path in a path finding response.
type HorizonPathRecord struct {
	SourceAssetType   string              `json:"source_asset_type"`
	SourceAssetCode   string              `json:"source_asset_code"`
	SourceAssetIssuer string              `json:"source_asset_issuer"`
	SourceAmount      string              `json:"source_amount"`
	DestinationAssetType   string         `json:"destination_asset_type"`
	DestinationAssetCode   string         `json:"destination_asset_code"`
	DestinationAssetIssuer string         `json:"destination_asset_issuer"`
	DestinationAmount      string         `json:"destination_amount"`
	Path                   []HorizonPathAsset `json:"path"`
}

// HorizonPathAsset represents an intermediate asset in a path.
type HorizonPathAsset struct {
	AssetType   string `json:"asset_type"`
	AssetCode   string `json:"asset_code"`
	AssetIssuer string `json:"asset_issuer"`
}

// HorizonPathResponse wraps the embedded records in a path finding response.
type HorizonPathResponse struct {
	Embedded struct {
		Records []HorizonPathRecord `json:"records"`
	} `json:"_embedded"`
}

// HorizonLiquidityPool represents a liquidity pool from the Horizon API.
type HorizonLiquidityPool struct {
	ID       string                      `json:"id"`
	Reserves []HorizonLiquidityPoolReserve `json:"reserves"`
}

// HorizonLiquidityPoolReserve represents a reserve in a liquidity pool.
type HorizonLiquidityPoolReserve struct {
	Asset  string `json:"asset"`
	Amount string `json:"amount"`
}
