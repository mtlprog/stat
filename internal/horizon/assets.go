package horizon

import (
	"context"
	"fmt"
	"net/url"

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
