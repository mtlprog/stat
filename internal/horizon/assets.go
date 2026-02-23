package horizon

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// horizonAccountBalance represents a single balance line on a Stellar account.
type horizonAccountBalance struct {
	Balance     string `json:"balance"`
	AssetType   string `json:"asset_type"`
	AssetCode   string `json:"asset_code"`
	AssetIssuer string `json:"asset_issuer"`
}

// horizonAccountRecord represents a single account from the Horizon /accounts endpoint.
type horizonAccountRecord struct {
	AccountID string                  `json:"account_id"`
	Balances  []horizonAccountBalance `json:"balances"`
}

// horizonAccountsResponse wraps the embedded records for account queries.
type horizonAccountsResponse struct {
	Links struct {
		Next struct {
			Href string `json:"href"`
		} `json:"next"`
	} `json:"_links"`
	Embedded struct {
		Records []horizonAccountRecord `json:"records"`
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

// accountBalanceForAsset returns the balance of the specified asset on an
// account. The boolean indicates whether the asset was found; if false, the
// returned decimal is zero.
func accountBalanceForAsset(rec horizonAccountRecord, asset domain.AssetInfo) (decimal.Decimal, bool) {
	for _, b := range rec.Balances {
		if b.AssetCode == asset.Code && b.AssetIssuer == asset.Issuer {
			v, err := decimal.NewFromString(b.Balance)
			if err != nil {
				slog.Warn("failed to parse account balance, skipping",
					"account", rec.AccountID, "asset", asset.Code,
					"balance", b.Balance, "error", err)
				return decimal.Zero, false
			}
			return v, true
		}
	}
	return decimal.Zero, false
}

// paginateAccounts iterates through all accounts holding the given asset,
// calling fn for each account record. Pagination stops when fn returns false,
// when there are no more pages, or on error.
func (c *Client) paginateAccounts(ctx context.Context, asset domain.AssetInfo, fn func(horizonAccountRecord) bool) error {
	assetFilter := asset.Code + ":" + asset.Issuer
	path := "/accounts?" + url.Values{
		"asset": []string{assetFilter},
		"limit": []string{"200"},
	}.Encode()

	for path != "" {
		var resp horizonAccountsResponse
		if err := c.getJSON(ctx, path, &resp); err != nil {
			return fmt.Errorf("fetching accounts for %s: %w", asset.Code, err)
		}

		for _, record := range resp.Embedded.Records {
			if !fn(record) {
				return nil
			}
		}

		if len(resp.Embedded.Records) == 0 || resp.Links.Next.Href == "" {
			break
		}

		u, err := url.Parse(resp.Links.Next.Href)
		if err != nil {
			return fmt.Errorf("parsing Horizon pagination link %q: %w", resp.Links.Next.Href, err)
		}
		path = u.Path + "?" + u.RawQuery
	}
	return nil
}

// FetchAssetHolderCountByBalance returns the number of accounts whose balance
// of the given asset is >= minBalance. It paginates through the Horizon
// /accounts endpoint and inspects each account's balance lines.
func (c *Client) FetchAssetHolderCountByBalance(ctx context.Context, asset domain.AssetInfo, minBalance decimal.Decimal) (int, error) {
	if asset.IsNative() {
		return 0, fmt.Errorf("cannot query holders for native asset")
	}

	var count int
	err := c.paginateAccounts(ctx, asset, func(rec horizonAccountRecord) bool {
		if bal, ok := accountBalanceForAsset(rec, asset); ok && bal.GreaterThanOrEqual(minBalance) {
			count++
		}
		return true
	})
	return count, err
}

// FetchAssetHolderIDsByBalance returns the account IDs of all accounts whose
// balance of the given asset is >= minBalance.
func (c *Client) FetchAssetHolderIDsByBalance(ctx context.Context, asset domain.AssetInfo, minBalance decimal.Decimal) ([]string, error) {
	if asset.IsNative() {
		return nil, fmt.Errorf("cannot query holders for native asset")
	}

	var ids []string
	err := c.paginateAccounts(ctx, asset, func(rec horizonAccountRecord) bool {
		if bal, ok := accountBalanceForAsset(rec, asset); ok && bal.GreaterThanOrEqual(minBalance) {
			ids = append(ids, rec.AccountID)
		}
		return true
	})
	return ids, err
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
