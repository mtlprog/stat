package horizon

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// FetchAccount retrieves a Stellar account's details including balances and data entries.
func (c *Client) FetchAccount(ctx context.Context, accountID string) (HorizonAccount, error) {
	var account HorizonAccount
	if err := c.getJSON(ctx, fmt.Sprintf("/accounts/%s", accountID), &account); err != nil {
		return HorizonAccount{}, fmt.Errorf("fetching account %s: %w", accountID, err)
	}
	return account, nil
}

// FetchAccountBalance returns the balance of the given asset for the specified account.
// Returns zero if the account does not hold the asset.
func (c *Client) FetchAccountBalance(ctx context.Context, accountID string, asset domain.AssetInfo) (decimal.Decimal, error) {
	account, err := c.FetchAccount(ctx, accountID)
	if err != nil {
		return decimal.Zero, err
	}

	for _, balance := range account.Balances {
		if balance.AssetCode == asset.Code && balance.AssetIssuer == asset.Issuer {
			amt, err := decimal.NewFromString(balance.Balance)
			if err != nil {
				return decimal.Zero, fmt.Errorf("parsing balance for %s: %w", asset.Code, err)
			}
			return amt, nil
		}
	}
	return decimal.Zero, nil
}

// FetchAccountDataEntry reads `account.data[key]` from /accounts/{id} and
// returns the base64-decoded UTF-8 value. The middle return is `present`:
// false when the key is absent (HTTP succeeded, key missing) — caller decides
// whether that's an error. The error return is non-nil only when the HTTP
// fetch or base64 decode fails.
func (c *Client) FetchAccountDataEntry(ctx context.Context, accountID, key string) (string, bool, error) {
	account, err := c.FetchAccount(ctx, accountID)
	if err != nil {
		return "", false, err
	}
	encoded, ok := account.Data[key]
	if !ok || encoded == "" {
		return "", false, nil
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", false, fmt.Errorf("decoding data entry %s on %s: %w", key, accountID, err)
	}
	return string(raw), true, nil
}
