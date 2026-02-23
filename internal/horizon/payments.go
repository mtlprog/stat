package horizon

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

type horizonTransaction struct {
	Memo     string `json:"memo"`
	MemoType string `json:"memo_type"`
}

type horizonOperation struct {
	Type        string              `json:"type"`
	To          string              `json:"to"`
	From        string              `json:"from"`
	AssetCode   string              `json:"asset_code"`
	AssetIssuer string              `json:"asset_issuer"`
	Amount      string              `json:"amount"`
	CreatedAt   string              `json:"created_at"`
	Transaction *horizonTransaction `json:"transaction"`
}

type horizonOperationsResponse struct {
	Links struct {
		Next struct {
			Href string `json:"href"`
		} `json:"next"`
	} `json:"_links"`
	Embedded struct {
		Records []horizonOperation `json:"records"`
	} `json:"_embedded"`
}

// FetchMonthlyEURMTLOutflow returns the total EURMTL paid from accountID to non-fund
// addresses in the last 30 days, counting only operations whose transaction memo starts
// with "div" (case-insensitive). Accepts payment and path_payment operation types.
func (c *Client) FetchMonthlyEURMTLOutflow(ctx context.Context, accountID string, fundAddresses []string) (decimal.Decimal, error) {
	since := time.Now().AddDate(0, 0, -30)
	eurmtl := domain.EURMTLAsset()

	fundSet := make(map[string]bool, len(fundAddresses))
	for _, addr := range fundAddresses {
		fundSet[addr] = true
	}

	total := decimal.Zero
	path := fmt.Sprintf("/accounts/%s/operations?join=transactions&order=desc&limit=200", accountID)

	for path != "" {
		var resp horizonOperationsResponse
		if err := c.getJSON(ctx, path, &resp); err != nil {
			return decimal.Zero, fmt.Errorf("fetching operations for %s: %w", accountID, err)
		}

		done := false
		for _, op := range resp.Embedded.Records {
			t, err := time.Parse(time.RFC3339, op.CreatedAt)
			if err != nil {
				continue
			}
			if t.Before(since) {
				done = true
				break
			}

			if op.Type != "payment" && op.Type != "path_payment_strict_send" && op.Type != "path_payment_strict_receive" {
				continue
			}
			if op.From != accountID {
				continue
			}
			if op.AssetCode != eurmtl.Code || op.AssetIssuer != eurmtl.Issuer {
				continue
			}
			if fundSet[op.To] {
				continue
			}

			// Only count operations whose transaction memo starts with "div"
			if op.Transaction == nil {
				continue
			}
			memo := strings.TrimSpace(strings.ToLower(op.Transaction.Memo))
			if !strings.Contains(memo, "div") {
				continue
			}

			amt, err := decimal.NewFromString(op.Amount)
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
			slog.Warn("failed to parse Horizon pagination link, results may be incomplete",
				"href", resp.Links.Next.Href, "error", err)
			break
		}
		path = u.Path + "?" + u.RawQuery
	}

	return total, nil
}
