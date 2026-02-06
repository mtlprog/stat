package horizon

import (
	"context"
	"fmt"
)

// FetchAccount retrieves a Stellar account's details including balances and data entries.
func (c *Client) FetchAccount(ctx context.Context, accountID string) (HorizonAccount, error) {
	var account HorizonAccount
	if err := c.getJSON(ctx, fmt.Sprintf("/accounts/%s", accountID), &account); err != nil {
		return HorizonAccount{}, fmt.Errorf("fetching account %s: %w", accountID, err)
	}
	return account, nil
}
