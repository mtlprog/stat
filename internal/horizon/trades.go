package horizon

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mtlprog/stat/internal/domain"
)

// HorizonTradePrice is the rational n/d representation of a trade's executed
// price. Horizon serialises `n` and `d` as JSON strings — the protocol values
// are int64 stroop ratios that can exceed the JSON-number safe range, so the
// API uses strings to preserve precision. Decoded as strings here and parsed
// via decimal.NewFromString at the consumer.
type HorizonTradePrice struct {
	N string `json:"n"`
	D string `json:"d"`
}

// HorizonTrade represents a single record from /trades. Only the fields the
// average-price calculation needs are unmarshalled.
type HorizonTrade struct {
	BaseAssetType      string            `json:"base_asset_type"`
	BaseAssetCode      string            `json:"base_asset_code"`
	BaseAssetIssuer    string            `json:"base_asset_issuer"`
	CounterAssetType   string            `json:"counter_asset_type"`
	CounterAssetCode   string            `json:"counter_asset_code"`
	CounterAssetIssuer string            `json:"counter_asset_issuer"`
	Price              HorizonTradePrice `json:"price"`
}

type horizonTradesResponse struct {
	Embedded struct {
		Records []HorizonTrade `json:"records"`
	} `json:"_embedded"`
}

// FetchTrades returns up to `limit` trades for the given pair, newest first.
// All trade types (orderbook + liquidity pool) are returned — Horizon doesn't
// filter unless `trade_type` is set explicitly.
func (c *Client) FetchTrades(ctx context.Context, base, counter domain.AssetInfo, limit int) ([]HorizonTrade, error) {
	params := url.Values{}
	if base.IsNative() {
		params.Set("base_asset_type", "native")
	} else {
		params.Set("base_asset_type", string(base.Type))
		params.Set("base_asset_code", base.Code)
		params.Set("base_asset_issuer", base.Issuer)
	}
	if counter.IsNative() {
		params.Set("counter_asset_type", "native")
	} else {
		params.Set("counter_asset_type", string(counter.Type))
		params.Set("counter_asset_code", counter.Code)
		params.Set("counter_asset_issuer", counter.Issuer)
	}
	params.Set("order", "desc")
	params.Set("limit", fmt.Sprintf("%d", limit))

	var resp horizonTradesResponse
	if err := c.getJSON(ctx, "/trades?"+params.Encode(), &resp); err != nil {
		return nil, fmt.Errorf("fetching trades: %w", err)
	}
	return resp.Embedded.Records, nil
}
