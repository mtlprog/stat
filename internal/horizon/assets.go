package horizon

import (
	"context"
	"fmt"
	"net/url"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// HorizonAssetsResponse wraps the embedded records for asset queries.
type HorizonAssetsResponse struct {
	Embedded struct {
		Records []HorizonAsset `json:"records"`
	} `json:"_embedded"`
}

// HorizonAsset represents an asset from the Horizon /assets endpoint.
type HorizonAsset struct {
	AssetType   string `json:"asset_type"`
	AssetCode   string `json:"asset_code"`
	AssetIssuer string `json:"asset_issuer"`
	NumAccounts int    `json:"num_accounts"`
	Amount      string `json:"amount"`
}

// FetchAssetHolders returns the number of accounts holding the given asset.
func (c *Client) FetchAssetHolders(ctx context.Context, asset domain.AssetInfo) (int, error) {
	if asset.IsNative() {
		return 0, fmt.Errorf("cannot query holders for native asset")
	}

	params := url.Values{}
	params.Set("asset_code", asset.Code)
	params.Set("asset_issuer", asset.Issuer)
	params.Set("limit", "1")

	var resp HorizonAssetsResponse
	if err := c.getJSON(ctx, "/assets?"+params.Encode(), &resp); err != nil {
		return 0, fmt.Errorf("fetching asset holders for %s: %w", asset.Code, err)
	}

	if len(resp.Embedded.Records) == 0 {
		return 0, nil
	}

	return resp.Embedded.Records[0].NumAccounts, nil
}

// FetchAssetAmount returns the total issued amount of the given asset.
func (c *Client) FetchAssetAmount(ctx context.Context, asset domain.AssetInfo) (decimal.Decimal, error) {
	if asset.IsNative() {
		return decimal.Zero, fmt.Errorf("cannot query amount for native asset")
	}

	params := url.Values{}
	params.Set("asset_code", asset.Code)
	params.Set("asset_issuer", asset.Issuer)
	params.Set("limit", "1")

	var resp HorizonAssetsResponse
	if err := c.getJSON(ctx, "/assets?"+params.Encode(), &resp); err != nil {
		return decimal.Zero, fmt.Errorf("fetching asset amount for %s: %w", asset.Code, err)
	}

	if len(resp.Embedded.Records) == 0 {
		return decimal.Zero, nil
	}

	amt, err := decimal.NewFromString(resp.Embedded.Records[0].Amount)
	if err != nil {
		return decimal.Zero, fmt.Errorf("parsing amount for %s: %w", asset.Code, err)
	}
	return amt, nil
}
