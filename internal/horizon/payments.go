package horizon

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

type horizonPayment struct {
	Type        string `json:"type"`
	To          string `json:"to"`
	From        string `json:"from"`
	AssetCode   string `json:"asset_code"`
	AssetIssuer string `json:"asset_issuer"`
	Amount      string `json:"amount"`
	CreatedAt   string `json:"created_at"`
}

type horizonPaymentsResponse struct {
	Links struct {
		Next struct {
			Href string `json:"href"`
		} `json:"next"`
	} `json:"_links"`
	Embedded struct {
		Records []horizonPayment `json:"records"`
	} `json:"_embedded"`
}

// FetchMonthlyEURMTLOutflow returns the total EURMTL paid from accountID to non-fund addresses
// in the last 30 days. It paginates through all results, stopping when payments fall outside
// the 30-day window (since results are ordered descending by time).
func (c *Client) FetchMonthlyEURMTLOutflow(ctx context.Context, accountID string, fundAddresses []string) (decimal.Decimal, error) {
	since := time.Now().AddDate(0, 0, -30)
	eurmtl := domain.EURMTLAsset()

	fundSet := make(map[string]bool, len(fundAddresses))
	for _, addr := range fundAddresses {
		fundSet[addr] = true
	}

	total := decimal.Zero
	path := fmt.Sprintf("/accounts/%s/payments?order=desc&limit=200", accountID)

	for path != "" {
		var resp horizonPaymentsResponse
		if err := c.getJSON(ctx, path, &resp); err != nil {
			return decimal.Zero, fmt.Errorf("fetching payments for %s: %w", accountID, err)
		}

		done := false
		for _, p := range resp.Embedded.Records {
			// Check timestamp first — records are ordered desc, so once we're past 30 days we can stop.
			t, err := time.Parse(time.RFC3339, p.CreatedAt)
			if err != nil {
				continue
			}
			if t.Before(since) {
				done = true
				break
			}

			if p.Type != "payment" {
				continue
			}
			if p.From != accountID {
				continue
			}
			if p.AssetCode != eurmtl.Code || p.AssetIssuer != eurmtl.Issuer {
				continue
			}
			if fundSet[p.To] {
				continue
			}

			amt, err := decimal.NewFromString(p.Amount)
			if err != nil {
				continue
			}
			total = total.Add(amt)
		}

		if done || len(resp.Embedded.Records) == 0 || resp.Links.Next.Href == "" {
			break
		}

		u, err := url.Parse(resp.Links.Next.Href)
		if err != nil {
			break
		}
		path = u.Path + "?" + u.RawQuery
	}

	return total, nil
}
