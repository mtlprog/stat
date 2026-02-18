package horizon

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// horizonAccountsResponse wraps the embedded records for account queries.
type horizonAccountsResponse struct {
	Links struct {
		Next struct {
			Href string `json:"href"`
		} `json:"next"`
	} `json:"_links"`
	Embedded struct {
		Records []struct {
			AccountID string `json:"account_id"`
		} `json:"records"`
	} `json:"_embedded"`
}

// HorizonAssetsResponse wraps the embedded records for asset queries.
type HorizonAssetsResponse struct {
	Embedded struct {
		Records []HorizonAsset `json:"records"`
	} `json:"_embedded"`
}

// HorizonAssetAccounts holds per-authorization-level account counts for an asset.
type HorizonAssetAccounts struct {
	Authorized                      int `json:"authorized"`
	AuthorizedToMaintainLiabilities int `json:"authorized_to_maintain_liabilities"`
	Unauthorized                    int `json:"unauthorized"`
}

// HorizonAssetBalances holds per-authorization-level balance totals for an asset.
type HorizonAssetBalances struct {
	Authorized                      string `json:"authorized"`
	AuthorizedToMaintainLiabilities string `json:"authorized_to_maintain_liabilities"`
	Unauthorized                    string `json:"unauthorized"`
}

// HorizonAsset represents an asset from the Horizon /assets endpoint.
type HorizonAsset struct {
	AssetType                string               `json:"asset_type"`
	AssetCode                string               `json:"asset_code"`
	AssetIssuer              string               `json:"asset_issuer"`
	Accounts                 HorizonAssetAccounts `json:"accounts"`
	Balances                 HorizonAssetBalances `json:"balances"`
	ClaimableBalancesAmount  string               `json:"claimable_balances_amount"`
	LiquidityPoolsAmount     string               `json:"liquidity_pools_amount"`
	ContractsAmount          string               `json:"contracts_amount"`
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

	return resp.Embedded.Records[0].Accounts.Authorized, nil
}

// FetchAllAssetHolderIDs returns the account IDs of all accounts holding the given asset.
// It paginates through all results using Horizon's cursor-based pagination.
func (c *Client) FetchAllAssetHolderIDs(ctx context.Context, asset domain.AssetInfo) ([]string, error) {
	if asset.IsNative() {
		return nil, fmt.Errorf("cannot query holders for native asset")
	}

	assetFilter := asset.Code + ":" + asset.Issuer
	path := "/accounts?" + url.Values{
		"asset": []string{assetFilter},
		"limit": []string{"200"},
	}.Encode()

	var ids []string
	for path != "" {
		var resp horizonAccountsResponse
		if err := c.getJSON(ctx, path, &resp); err != nil {
			return nil, fmt.Errorf("fetching holder IDs for %s: %w", asset.Code, err)
		}

		for _, record := range resp.Embedded.Records {
			ids = append(ids, record.AccountID)
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
	return ids, nil
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

	rec := resp.Embedded.Records[0]
	total := decimal.Zero
	for _, s := range []string{
		rec.Balances.Authorized,
		rec.Balances.AuthorizedToMaintainLiabilities,
		rec.Balances.Unauthorized,
		rec.ClaimableBalancesAmount,
		rec.LiquidityPoolsAmount,
		rec.ContractsAmount,
	} {
		if s == "" {
			continue
		}
		v, err := decimal.NewFromString(s)
		if err != nil {
			return decimal.Zero, fmt.Errorf("parsing amount for %s: %w", asset.Code, err)
		}
		total = total.Add(v)
	}
	return total, nil
}
