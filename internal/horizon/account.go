package horizon

import (
	"context"
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
