package horizon

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
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
	Type            string              `json:"type"`
	To              string              `json:"to"`
	From            string              `json:"from"`
	AssetCode       string              `json:"asset_code"`
	AssetIssuer     string              `json:"asset_issuer"`
	Amount          string              `json:"amount"`
	CreatedAt       string              `json:"created_at"`
	TransactionHash string              `json:"transaction_hash"`
	Transaction     *horizonTransaction `json:"transaction"`
	// manage_data fields (populated only when Type == "manage_data")
	Name  string `json:"name"`
	Value string `json:"value"` // base64-encoded for manage_data ops with non-nil value
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

// LastDivsUpdate is one historical write of the dividend distributor's
// `LAST_DIVS` data entry — the canonical "last monthly dividend amount"
// published by the fund's bot. I11 at any point in time is the LAST_DIVS
// value as of the most recent update at-or-before that date.
type LastDivsUpdate struct {
	TS    time.Time
	Value decimal.Decimal
}

// RecipientGroup is one logical dividend distribution: all "mtl div <date>"
// payments under the same memo, possibly split across multiple Stellar
// transactions (Stellar's per-tx op cap of 100 forces big batches into
// multiple txs). I18 = |Recipients| of the latest group at-or-before the
// snapshot date.
type RecipientGroup struct {
	TS         time.Time // earliest tx timestamp in the group
	Memo       string    // raw (cased) memo, e.g. "mtl div 07/05/2026"
	Recipients []string  // distinct, non-fund destinations
}

// DividendActivity bundles both series produced by one descending walk on the
// distributor's /operations endpoint. Both are sorted by TS ascending so the
// caller can find "latest event ≤ date" with a single linear scan.
type DividendActivity struct {
	LastDivsUpdates []LastDivsUpdate
	RecipientGroups []RecipientGroup
}

// dividendMemoPrefix is the canonical opening of a dividend transaction memo.
// Production memos look like "mtl div 07/05/2026"; we anchor on the prefix
// rather than substring "div" so unrelated memos containing "div" (e.g.
// subfond-internal "Q672 div MCITY" payouts on different accounts) don't
// pollute the indicator series.
const dividendMemoPrefix = "mtl div "

// lastDivsDataKey is the manage_data key the distributor bot uses to publish
// the canonical last-distribution-amount. The on-account current value is the
// authoritative I11 for "now"; the historical series comes from manage_data
// op history — same fund, single source of truth.
const lastDivsDataKey = "LAST_DIVS"

// FetchDividendActivity walks /accounts/{distributor}/operations once,
// descending, collecting every LAST_DIVS manage_data update and every
// "mtl div " payment op (grouped by memo, fund-recipients excluded). Returns
// both series sorted ascending by TS.
//
// The walk terminates at op.CreatedAt < since, so callers control depth:
// live (~90d) is a few pages; backfill anchored at oldest snapshot - 2
// months walks the full distribution history.
func (c *Client) FetchDividendActivity(ctx context.Context, distributor string, fundAddresses []string, since time.Time) (DividendActivity, error) {
	eurmtl := domain.EURMTLAsset()

	fundSet := make(map[string]bool, len(fundAddresses))
	for _, addr := range fundAddresses {
		fundSet[addr] = true
	}

	type partial struct {
		ts         time.Time
		memo       string
		recipients map[string]struct{}
	}
	byMemo := make(map[string]*partial)
	var lastDivsUpdates []LastDivsUpdate

	path := fmt.Sprintf("/accounts/%s/operations?join=transactions&order=desc&limit=200", distributor)

	for path != "" {
		var resp horizonOperationsResponse
		if err := c.getJSON(ctx, path, &resp); err != nil {
			return DividendActivity{}, fmt.Errorf("fetching operations for %s: %w", distributor, err)
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

			switch op.Type {
			case "manage_data":
				if op.Name != lastDivsDataKey || op.Value == "" {
					continue
				}
				raw, err := base64.StdEncoding.DecodeString(op.Value)
				if err != nil {
					slog.Error("dividend walker: LAST_DIVS value not valid base64", "ts", op.CreatedAt, "error", err)
					continue
				}
				val, err := decimal.NewFromString(strings.TrimSpace(string(raw)))
				if err != nil {
					slog.Error("dividend walker: LAST_DIVS value not numeric", "ts", op.CreatedAt, "raw", string(raw), "error", err)
					continue
				}
				lastDivsUpdates = append(lastDivsUpdates, LastDivsUpdate{TS: t, Value: val})

			case "payment":
				if op.From != distributor {
					continue
				}
				if op.AssetCode != eurmtl.Code || op.AssetIssuer != eurmtl.Issuer {
					continue
				}
				if op.Transaction == nil {
					continue
				}
				memoLower := strings.TrimSpace(strings.ToLower(op.Transaction.Memo))
				if !strings.HasPrefix(memoLower, dividendMemoPrefix) {
					continue
				}
				if fundSet[op.To] {
					continue
				}

				key := memoLower
				ev, ok := byMemo[key]
				if !ok {
					ev = &partial{ts: t, memo: op.Transaction.Memo, recipients: make(map[string]struct{})}
					byMemo[key] = ev
				}
				if t.Before(ev.ts) {
					ev.ts = t
				}
				ev.recipients[op.To] = struct{}{}
			}
		}

		if done || len(resp.Embedded.Records) == 0 || resp.Links.Next.Href == "" {
			break
		}

		u, err := url.Parse(resp.Links.Next.Href)
		if err != nil {
			slog.Error("failed to parse Horizon pagination link, results may be incomplete",
				"href", resp.Links.Next.Href, "error", err)
			break
		}
		path = u.Path + "?" + u.RawQuery
	}

	groups := make([]RecipientGroup, 0, len(byMemo))
	for _, ev := range byMemo {
		recipients := make([]string, 0, len(ev.recipients))
		for r := range ev.recipients {
			recipients = append(recipients, r)
		}
		groups = append(groups, RecipientGroup{
			TS:         ev.ts,
			Memo:       ev.memo,
			Recipients: recipients,
		})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].TS.Before(groups[j].TS) })
	sort.Slice(lastDivsUpdates, func(i, j int) bool { return lastDivsUpdates[i].TS.Before(lastDivsUpdates[j].TS) })

	return DividendActivity{
		LastDivsUpdates: lastDivsUpdates,
		RecipientGroups: groups,
	}, nil
}
