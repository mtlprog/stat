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
	"sort"
	"time"

	"github.com/shopspring/decimal"
)

// EURMTLAssetID is the asset identifier stellar.expert uses for the EURMTL
// trustline (code-issuer-decimals) on the public network.
const EURMTLAssetID = "EURMTL-GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V-2"

// stroopsScale shifts Stellar's 7-decimal stroop integers right by 7 places
// (via Decimal.Shift) to recover the asset-denominated amount.
const stroopsScale = -7

// maxResponseBytes caps the stats-history response. The full EURMTL history
// today is ~1.7K 200-byte points (≈340 KB); 10 MB leaves vast headroom while
// preventing OOM on a hostile or runaway response from the configurable URL.
const maxResponseBytes = 10 << 20

// ErrNoDailyEntry signals that stats-history is well-formed and contains
// recent points, but no row matches the requested date — i.e. stellar.expert
// hasn't ingested that day yet. Callers should treat this as "data not yet
// available" and sticky-fallback to the previous day's persisted value.
//
// Empty payloads, all-points-in-the-future, all-points-in-the-distant-past,
// or any decode/transport failure are NOT this sentinel — they propagate as
// real errors so the caller can log loud and the ops team can act.
var ErrNoDailyEntry = errors.New("stellar.expert stats-history has no entry for the requested date")

// freshnessWindow caps how far behind the most-recent stats-history point
// can be from the target date before we treat the gap as a real outage
// rather than "ingester just behind". 48h gives stellar.expert a full day of
// late-ingest grace plus DST/timezone slack.
const freshnessWindow = 48 * time.Hour

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
// EURMTL as of `date` (UTC midnight). The response is defensively sorted
// ascending by `ts` before we accumulate; we then sum while ts ≤ targetTs
// and stop once we cross it.
//
// Returns ErrNoDailyEntry only when the response is well-formed, contains
// recent points (most-recent ts within freshnessWindow of targetTs), but no
// row matches `date` exactly — i.e. stellar.expert hasn't ingested that day
// yet. Empty payloads, stale-only payloads, or out-of-range targets surface
// as real errors so the caller logs loud rather than silently sticky-falls
// back.
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return Stats{}, fmt.Errorf("reading stats-history body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		preview := body
		if len(preview) > 1024 {
			preview = preview[:1024]
		}
		return Stats{}, fmt.Errorf("stats-history returned %s: %s", resp.Status, string(preview))
	}

	var pts []historyPoint
	if err := json.Unmarshal(body, &pts); err != nil {
		return Stats{}, fmt.Errorf("decoding stats-history: %w", err)
	}
	if len(pts) == 0 {
		return Stats{}, fmt.Errorf("stats-history returned an empty array")
	}

	// Defensive: don't trust the API's order. The cumulative loop relies on
	// ascending ts — sort once locally rather than wedge a silent miscount
	// if the endpoint ever flips ordering.
	sort.Slice(pts, func(i, j int) bool { return pts[i].Ts < pts[j].Ts })

	targetTs := date.UTC().Truncate(24 * time.Hour).Unix()

	// Reject targets that are obviously outside the dataset — both directions
	// would otherwise collapse to ErrNoDailyEntry, masking the real failure.
	if pts[len(pts)-1].Ts < targetTs-int64(freshnessWindow.Seconds()) {
		latest := time.Unix(pts[len(pts)-1].Ts, 0).UTC().Format("2006-01-02")
		return Stats{}, fmt.Errorf("stats-history latest point (%s) is more than %s before target (%s) — likely outage upstream",
			latest, freshnessWindow, date.UTC().Format("2006-01-02"))
	}
	if pts[0].Ts > targetTs {
		earliest := time.Unix(pts[0].Ts, 0).UTC().Format("2006-01-02")
		return Stats{}, fmt.Errorf("stats-history earliest point (%s) is after target (%s) — target predates the dataset",
			earliest, date.UTC().Format("2006-01-02"))
	}

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
