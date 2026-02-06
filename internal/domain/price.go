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

// PathHop represents a single hop in a path finding route.
type PathHop struct {
	From      string         `json:"from"`
	To        string         `json:"to"`
	Orderbook *OrderbookData `json:"orderbook,omitempty"`
}

// PriceDetails is a concrete struct representing price source metadata.
// The Source field discriminates between "path", "orderbook", and "best".
type PriceDetails struct {
	Source            string         `json:"source"`                      // "path", "orderbook", or "best"
	PriceType         string         `json:"priceType,omitempty"`         // "bid" or "ask"
	SourceAmount      *string        `json:"sourceAmount,omitempty"`      // path
	DestinationAmount *string        `json:"destinationAmount,omitempty"` // path
	Path              []PathHop      `json:"path,omitempty"`              // path
	OrderbookData     *OrderbookData `json:"orderbookData,omitempty"`     // orderbook
	PathPrice         *string        `json:"pathPrice,omitempty"`         // best
	OrderbookPrice    *string        `json:"orderbookPrice,omitempty"`    // best
	ChosenSource      string         `json:"chosenSource,omitempty"`      // best: "path" or "orderbook"
	PathSubDetails    *PriceDetails  `json:"pathDetails,omitempty"`       // best
	OBSubDetails      *PriceDetails  `json:"orderbookDetails,omitempty"`  // best
}

// TokenPairPrice represents the price relationship between two tokens.
type TokenPairPrice struct {
	TokenA            string        `json:"tokenA"`
	TokenB            string        `json:"tokenB"`
	Price             string        `json:"price"`
	DestinationAmount string        `json:"destinationAmount"`
	Timestamp         time.Time     `json:"timestamp"`
	Details           *PriceDetails `json:"details,omitempty"`
}
