package horizon

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mtlprog/stat/internal/domain"
)

// HorizonLiquidityPoolsResponse wraps the embedded records for liquidity pool queries.
type HorizonLiquidityPoolsResponse struct {
	Embedded struct {
		Records []HorizonLiquidityPool `json:"records"`
	} `json:"_embedded"`
}

// FetchLiquidityPools retrieves liquidity pools containing both reserve assets.
func (c *Client) FetchLiquidityPools(ctx context.Context, reserveA, reserveB domain.AssetInfo) ([]HorizonLiquidityPool, error) {
	params := url.Values{}

	reserves := make([]string, 0, 2)
	for _, asset := range []domain.AssetInfo{reserveA, reserveB} {
		if asset.IsNative() {
			reserves = append(reserves, "native")
		} else {
			reserves = append(reserves, fmt.Sprintf("%s:%s", asset.Code, asset.Issuer))
		}
	}
	params.Set("reserves", reserves[0]+","+reserves[1])
	params.Set("limit", "1")

	var resp HorizonLiquidityPoolsResponse
	if err := c.getJSON(ctx, "/liquidity_pools?"+params.Encode(), &resp); err != nil {
		return nil, fmt.Errorf("fetching liquidity pools: %w", err)
	}
	return resp.Embedded.Records, nil
}
