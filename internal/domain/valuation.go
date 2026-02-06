package domain

// ValuationType distinguishes between total-price and per-unit valuations.
type ValuationType string

const (
	ValuationTypeNFT  ValuationType = "nft"  // _COST: total price for entire holding
	ValuationTypeUnit ValuationType = "unit" // _1COST: price per unit
)

// ValuationValueType identifies how a DATA entry value should be interpreted.
type ValuationValueType string

const (
	ValuationValueEURMTL   ValuationValueType = "eurmtl"
	ValuationValueExternal ValuationValueType = "external"
)

// ValuationValue represents the parsed value from a Stellar DATA entry.
type ValuationValue struct {
	Type     ValuationValueType `json:"type"`
	Value    string             `json:"value,omitempty"`    // For eurmtl type
	Symbol   string             `json:"symbol,omitempty"`   // For external type: BTC, ETH, XLM, Sats, USD, AU
	Quantity *float64           `json:"quantity,omitempty"` // For compound external values (e.g., AU 1g)
	Unit     string             `json:"unit,omitempty"`     // g, oz
}

// AssetValuation represents a manual valuation read from a Stellar DATA entry.
type AssetValuation struct {
	TokenCode     string         `json:"tokenCode"`
	ValuationType ValuationType  `json:"valuationType"`
	RawValue      ValuationValue `json:"rawValue"`
	SourceAccount string         `json:"sourceAccount"`
}

// ResolvedAssetValuation extends AssetValuation with a resolved EUR/EURMTL price.
type ResolvedAssetValuation struct {
	AssetValuation
	ValueInEURMTL string `json:"valueInEURMTL"`
}
