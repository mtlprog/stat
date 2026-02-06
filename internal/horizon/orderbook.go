package horizon

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mtlprog/stat/internal/domain"
)

// FetchOrderbook retrieves the orderbook for a trading pair.
func (c *Client) FetchOrderbook(ctx context.Context, selling, buying domain.AssetInfo, limit int) (HorizonOrderbook, error) {
	params := url.Values{}
	if selling.IsNative() {
		params.Set("selling_asset_type", "native")
	} else {
		params.Set("selling_asset_type", string(selling.Type))
		params.Set("selling_asset_code", selling.Code)
		params.Set("selling_asset_issuer", selling.Issuer)
	}
	if buying.IsNative() {
		params.Set("buying_asset_type", "native")
	} else {
		params.Set("buying_asset_type", string(buying.Type))
		params.Set("buying_asset_code", buying.Code)
		params.Set("buying_asset_issuer", buying.Issuer)
	}
	params.Set("limit", fmt.Sprintf("%d", limit))

	var ob HorizonOrderbook
	if err := c.getJSON(ctx, "/order_book?"+params.Encode(), &ob); err != nil {
		return HorizonOrderbook{}, fmt.Errorf("fetching orderbook: %w", err)
	}
	return ob, nil
}
