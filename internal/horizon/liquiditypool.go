package horizon

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// HorizonLiquidityPoolsResponse wraps the embedded records for liquidity pool queries.
type HorizonLiquidityPoolsResponse struct {
	Links struct {
		Next struct {
			Href string `json:"href"`
		} `json:"next"`
	} `json:"_links"`
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

// FetchAllPoolReservesForAsset returns the total amount of the given asset locked across all AMM pools.
// It paginates through all results using Horizon's cursor-based pagination.
func (c *Client) FetchAllPoolReservesForAsset(ctx context.Context, asset domain.AssetInfo) (decimal.Decimal, error) {
	assetFilter := asset.Code + ":" + asset.Issuer
	path := "/liquidity_pools?" + url.Values{
		"reserves": []string{assetFilter},
		"limit":    []string{"200"},
	}.Encode()

	total := decimal.Zero
	for path != "" {
		var resp HorizonLiquidityPoolsResponse
		if err := c.getJSON(ctx, path, &resp); err != nil {
			return decimal.Zero, fmt.Errorf("fetching liquidity pools for %s: %w", asset.Code, err)
		}

		for _, pool := range resp.Embedded.Records {
			for _, reserve := range pool.Reserves {
				if reserve.Asset == assetFilter {
					amt, err := decimal.NewFromString(reserve.Amount)
					if err != nil {
						slog.Warn("failed to parse pool reserve amount", "pool", pool.ID, "asset", reserve.Asset, "error", err)
						continue
					}
					total = total.Add(amt)
				}
			}
		}

		if len(resp.Embedded.Records) == 0 || resp.Links.Next.Href == "" {
			break
		}

		u, err := url.Parse(resp.Links.Next.Href)
		if err != nil {
			slog.Warn("failed to parse Horizon pagination link, results may be incomplete",
				"href", resp.Links.Next.Href, "error", err)
			break
		}
		path = u.Path + "?" + u.RawQuery
	}
	return total, nil
}
