// Package stellarexpert is a minimal client for stellar.expert's pre-aggregated
// asset stats. It exists to back I25 (daily EURMTL payment volume) and I26
// (cumulative EURMTL payment volume) with a single HTTP call instead of
// paginating Horizon's /payments endpoint per run — stellar.expert ingests
// every payment and exposes a daily breakdown via /stats-history.
package stellarexpert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/shopspring/decimal"
)

// EURMTLAssetID is the asset identifier stellar.expert uses for the EURMTL
// trustline (code-issuer-decimals) on the public network.
const EURMTLAssetID = "EURMTL-GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V-2"

// stroopsScale converts Stellar's 7-decimal stroop integers to asset-denominated
// amounts via Decimal.Shift.
const stroopsScale = -7

// ErrNoDailyEntry signals that stats-history has no row for the requested date.
// Callers should treat this as "data not yet available" and sticky-fallback to
// the previous day's persisted value.
var ErrNoDailyEntry = errors.New("stellar.expert stats-history has no entry for the requested date")

// Stats holds I25 (Daily) and I26 (Cumulative) for one snapshot date.
// Both values are in EURMTL — already shifted from stroops.
type Stats struct {
	Daily      decimal.Decimal
	Cumulative decimal.Decimal
}

// Client is an HTTP client for stellar.expert's public read-only API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a Client. baseURL should be the API root, e.g.
// "https://api.stellar.expert".
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type historyPoint struct {
	Ts             int64       `json:"ts"`
	PaymentsAmount json.Number `json:"payments_amount"`
}

// FetchEURMTLPaymentStats returns the daily and cumulative payment volume for
// EURMTL as of `date` (UTC midnight). stats-history is sorted ascending by
// `ts`, so we accumulate while ts ≤ targetDay and stop once we cross it.
//
// Returns ErrNoDailyEntry when no row matches `date` exactly — the caller
// should sticky-fallback rather than treat this as a real error.
func (c *Client) FetchEURMTLPaymentStats(ctx context.Context, date time.Time) (Stats, error) {
	url := c.baseURL + "/explorer/public/asset/" + EURMTLAssetID + "/stats-history"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Stats{}, fmt.Errorf("building request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Stats{}, fmt.Errorf("calling stats-history: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return Stats{}, fmt.Errorf("stats-history returned %s: %s", resp.Status, string(body))
	}

	var pts []historyPoint
	if err := json.NewDecoder(resp.Body).Decode(&pts); err != nil {
		return Stats{}, fmt.Errorf("decoding stats-history: %w", err)
	}

	targetTs := date.UTC().Truncate(24 * time.Hour).Unix()

	cumulative := decimal.Zero
	var daily decimal.Decimal
	found := false
	for _, p := range pts {
		if p.Ts > targetTs {
			break
		}
		amt, err := decimal.NewFromString(p.PaymentsAmount.String())
		if err != nil {
			return Stats{}, fmt.Errorf("parsing payments_amount %q at ts=%d: %w", p.PaymentsAmount.String(), p.Ts, err)
		}
		cumulative = cumulative.Add(amt)
		if p.Ts == targetTs {
			daily = amt
			found = true
		}
	}

	if !found {
		return Stats{}, ErrNoDailyEntry
	}

	return Stats{
		Daily:      daily.Shift(stroopsScale),
		Cumulative: cumulative.Shift(stroopsScale),
	}, nil
}
