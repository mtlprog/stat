package domain

import "time"

// PriceSource holds ask/bid prices from a single source.
type PriceSource struct {
	Ask *string `json:"ask"`
	Bid *string `json:"bid"`
}

// OrderbookData combines traditional orderbook and AMM price data.
type OrderbookData struct {
	Orderbook  PriceSource `json:"orderbook"`
	AMM        PriceSource `json:"amm"`
	AMMPoolID  *string     `json:"poolId,omitempty"`
	BestSource string      `json:"bestSource"` // "orderbook", "amm", or "none"
}

// PriceDetails is a discriminated union for price source metadata.
// Implementations: PathDetails, OrderbookDetails, BestDetails.
type PriceDetails interface {
	PriceSource() string
}

// PathHop represents a single hop in a path finding route.
type PathHop struct {
	From      string         `json:"from"`
	To        string         `json:"to"`
	Orderbook *OrderbookData `json:"orderbook,omitempty"`
}

// PathDetails contains metadata from path finding price discovery.
type PathDetails struct {
	Source            string    `json:"source"` // always "path"
	SourceAmount      *string   `json:"sourceAmount,omitempty"`
	DestinationAmount *string   `json:"destinationAmount,omitempty"`
	Path              []PathHop `json:"path"`
}

func (d *PathDetails) PriceSource() string { return "path" }

// OrderbookDetails contains metadata from direct orderbook/AMM price discovery.
type OrderbookDetails struct {
	Source        string        `json:"source"`    // always "orderbook"
	PriceType     string        `json:"priceType"` // "bid" or "ask"
	OrderbookData OrderbookData `json:"orderbookData"`
}

func (d *OrderbookDetails) PriceSource() string { return "orderbook" }

// BestDetails contains metadata when both path and orderbook sources are compared.
type BestDetails struct {
	Source           string            `json:"source"`    // always "best"
	PriceType        string            `json:"priceType"` // "bid" or "ask"
	PathPrice        *string           `json:"pathPrice"`
	OrderbookPrice   *string           `json:"orderbookPrice"`
	ChosenSource     string            `json:"chosenSource"` // "path" or "orderbook"
	PathDetails      *PathDetails      `json:"pathDetails,omitempty"`
	OrderbookDetails *OrderbookDetails `json:"orderbookDetails,omitempty"`
}

func (d *BestDetails) PriceSource() string { return "best" }

// TokenPairPrice represents the price relationship between two tokens.
type TokenPairPrice struct {
	TokenA            string       `json:"tokenA"`
	TokenB            string       `json:"tokenB"`
	Price             string       `json:"price"`
	DestinationAmount string       `json:"destinationAmount"`
	Timestamp         time.Time    `json:"timestamp"`
	Details           PriceDetails `json:"details,omitempty"`
}
