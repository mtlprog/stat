package horizon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// b64 encodes a string for use as a manage_data wire value.
func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// distributorAddr is the canonical dividend distributor used across these
// tests; matches what production-side callers pass.
const distributorAddr = "GDNHQWZRZDZZBARNOH6VFFXMN6LBUNZTZHOKBUT7GREOWBTZI4FGS7IQ"

// --- FetchDividendActivity ---

// Walker correctness on a single page covering the full filter matrix:
//   - LAST_DIVS manage_data update
//   - "mtl div ..." payment to a real recipient
//   - "MTL DIV ..." payment (uppercase memo) — must still group via case-insensitive prefix
//   - subfond "Q672 div MCITY" payment — substring "div" present, must NOT match
//   - payment to a fund-set address — must be excluded
//   - manage_data with a different name — must NOT show up
func TestFetchDividendActivityFiltersAndGroups(t *testing.T) {
	eurmtlIssuer := domain.IssuerAddress
	fundAddr := "GFUNDABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABCDEFGHIJKLMNOPQRST"

	resp := map[string]any{
		"_links": map[string]any{"next": map[string]any{"href": ""}},
		"_embedded": map[string]any{
			"records": []map[string]any{
				// Order is descending by created_at — that's how Horizon delivers it.
				{
					"type":        "manage_data",
					"name":        "LAST_DIVS",
					"value":       b64("123.45"),
					"created_at":  "2026-05-07T06:55:00Z",
					"transaction": map[string]any{"memo": "", "memo_type": "none"},
				},
				{
					"type":        "manage_data",
					"name":        "Some_Other_Key",
					"value":       b64("ignored"),
					"created_at":  "2026-05-07T06:54:30Z",
					"transaction": map[string]any{"memo": "", "memo_type": "none"},
				},
				{
					"type":         "payment",
					"from":         distributorAddr,
					"to":           "GREC1ABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABCDEFGHIJKLMNOPQRST",
					"asset_code":   "EURMTL",
					"asset_issuer": eurmtlIssuer,
					"amount":       "0.0500000",
					"created_at":   "2026-05-07T06:55:30Z",
					"transaction":  map[string]any{"memo": "mtl div 07/05/2026", "memo_type": "text"},
				},
				{
					// Same memo, different recipient — must merge into one group.
					"type":         "payment",
					"from":         distributorAddr,
					"to":           "GREC2ABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABCDEFGHIJKLMNOPQRST",
					"asset_code":   "EURMTL",
					"asset_issuer": eurmtlIssuer,
					"amount":       "0.0500000",
					"created_at":   "2026-05-07T06:55:35Z",
					"transaction":  map[string]any{"memo": "mtl div 07/05/2026", "memo_type": "text"},
				},
				{
					// Uppercase memo — must group with the lowercase ones.
					"type":         "payment",
					"from":         distributorAddr,
					"to":           "GREC3ABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABCDEFGHIJKLMNOPQRST",
					"asset_code":   "EURMTL",
					"asset_issuer": eurmtlIssuer,
					"amount":       "0.0500000",
					"created_at":   "2026-05-07T06:55:40Z",
					"transaction":  map[string]any{"memo": "MTL DIV 07/05/2026", "memo_type": "text"},
				},
				{
					// Recipient is a fund address — must be excluded.
					"type":         "payment",
					"from":         distributorAddr,
					"to":           fundAddr,
					"asset_code":   "EURMTL",
					"asset_issuer": eurmtlIssuer,
					"amount":       "100.0000000",
					"created_at":   "2026-05-07T06:55:45Z",
					"transaction":  map[string]any{"memo": "mtl div 07/05/2026", "memo_type": "text"},
				},
				{
					// Memo contains "div" but doesn't match the "mtl div " prefix — must NOT group.
					"type":         "payment",
					"from":         distributorAddr,
					"to":           "GREC4ABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABCDEFGHIJKLMNOPQRST",
					"asset_code":   "EURMTL",
					"asset_issuer": eurmtlIssuer,
					"amount":       "5.0000000",
					"created_at":   "2026-05-07T06:55:50Z",
					"transaction":  map[string]any{"memo": "Q672 div MCITY 04/26", "memo_type": "text"},
				},
				{
					// Wrong asset — must NOT match.
					"type":         "payment",
					"from":         distributorAddr,
					"to":           "GREC5ABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABCDEFGHIJKLMNOPQRST",
					"asset_code":   "USDM",
					"asset_issuer": eurmtlIssuer,
					"amount":       "1.0000000",
					"created_at":   "2026-05-07T06:55:55Z",
					"transaction":  map[string]any{"memo": "mtl div 07/05/2026", "memo_type": "text"},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	since := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	activity, err := client.FetchDividendActivity(context.Background(), distributorAddr, []string{fundAddr}, since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(activity.LastDivsUpdates) != 1 {
		t.Fatalf("LastDivsUpdates len = %d, want 1 (only LAST_DIVS, not Some_Other_Key)", len(activity.LastDivsUpdates))
	}
	if !activity.LastDivsUpdates[0].Value.Equal(decimal.RequireFromString("123.45")) {
		t.Errorf("LAST_DIVS value = %s, want 123.45", activity.LastDivsUpdates[0].Value)
	}

	if len(activity.RecipientGroups) != 1 {
		t.Fatalf("RecipientGroups len = %d, want 1 (only memos with prefix 'mtl div ', case-insensitive, merged)", len(activity.RecipientGroups))
	}
	g := activity.RecipientGroups[0]
	if len(g.Recipients) != 3 {
		t.Errorf("Recipients len = %d, want 3 (REC1, REC2, REC3 — fund addr excluded, REC4/REC5 don't match)", len(g.Recipients))
	}
	for _, r := range g.Recipients {
		if r == fundAddr {
			t.Errorf("Recipients contains fund address %s — should be excluded", fundAddr)
		}
	}
}

// LAST_DIVS values that fail to base64-decode or parse as decimal must be
// skipped without aborting the walk. Operator visibility comes from
// slog.Error inside the walker (not asserted here — slog is global).
func TestFetchDividendActivitySkipsBadLastDivsValues(t *testing.T) {
	resp := map[string]any{
		"_links": map[string]any{"next": map[string]any{"href": ""}},
		"_embedded": map[string]any{
			"records": []map[string]any{
				{
					"type":        "manage_data",
					"name":        "LAST_DIVS",
					"value":       b64("100"),
					"created_at":  "2026-05-07T06:55:00Z",
					"transaction": map[string]any{"memo": "", "memo_type": "none"},
				},
				{
					"type":        "manage_data",
					"name":        "LAST_DIVS",
					"value":       "not-base64-!!!",
					"created_at":  "2026-05-07T06:54:00Z",
					"transaction": map[string]any{"memo": "", "memo_type": "none"},
				},
				{
					"type":        "manage_data",
					"name":        "LAST_DIVS",
					"value":       b64("not-a-number"),
					"created_at":  "2026-05-07T06:53:00Z",
					"transaction": map[string]any{"memo": "", "memo_type": "none"},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	since := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	activity, err := client.FetchDividendActivity(context.Background(), distributorAddr, nil, since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(activity.LastDivsUpdates) != 1 {
		t.Errorf("LastDivsUpdates len = %d, want 1 (only the valid value survives)", len(activity.LastDivsUpdates))
	}
}

// Pagination: the walker must follow _links.next.href across pages and
// terminate cleanly at op.CreatedAt < since regardless of which page contains
// the boundary op.
func TestFetchDividendActivityPaginatesAndTerminates(t *testing.T) {
	eurmtlIssuer := domain.IssuerAddress

	mkOp := func(to, ts string) map[string]any {
		return map[string]any{
			"type":         "payment",
			"from":         distributorAddr,
			"to":           to,
			"asset_code":   "EURMTL",
			"asset_issuer": eurmtlIssuer,
			"amount":       "0.0100000",
			"created_at":   ts,
			"transaction":  map[string]any{"memo": "mtl div 07/05/2026", "memo_type": "text"},
		}
	}

	var page int
	var nextURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		page++
		var body map[string]any
		switch page {
		case 1:
			body = map[string]any{
				"_links":    map[string]any{"next": map[string]any{"href": nextURL + "?cursor=p1"}},
				"_embedded": map[string]any{"records": []map[string]any{mkOp("GA1", "2026-05-07T10:00:00Z"), mkOp("GA2", "2026-05-07T09:00:00Z")}},
			}
		case 2:
			body = map[string]any{
				"_links":    map[string]any{"next": map[string]any{"href": nextURL + "?cursor=p2"}},
				"_embedded": map[string]any{"records": []map[string]any{mkOp("GA3", "2026-05-07T08:00:00Z"), mkOp("GA4", "2026-05-07T07:00:00Z")}},
			}
		case 3:
			body = map[string]any{
				"_links":    map[string]any{"next": map[string]any{"href": nextURL + "?cursor=p3"}},
				"_embedded": map[string]any{"records": []map[string]any{mkOp("GA5", "2026-05-06T12:00:00Z")}}, // before since → terminates
			}
		default:
			t.Errorf("walker fetched page %d — should have terminated by now", page)
			body = map[string]any{"_links": map[string]any{"next": map[string]any{"href": ""}}, "_embedded": map[string]any{"records": []map[string]any{}}}
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer server.Close()
	nextURL = server.URL + "/accounts/" + distributorAddr + "/operations"

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	since := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	activity, err := client.FetchDividendActivity(context.Background(), distributorAddr, nil, since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page != 3 {
		t.Errorf("walker fetched %d page(s), want exactly 3 (terminate on page 3 boundary op)", page)
	}
	if len(activity.RecipientGroups) != 1 {
		t.Fatalf("RecipientGroups len = %d, want 1 (all GA1..GA4 share one memo)", len(activity.RecipientGroups))
	}
	if got := len(activity.RecipientGroups[0].Recipients); got != 4 {
		t.Errorf("Recipients len = %d, want 4 (GA1..GA4 from pages 1+2; GA5 excluded by since)", got)
	}
}

// Pagination link malformed → return error to caller (no silent truncation).
func TestFetchDividendActivityErrorsOnBadPaginationLink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"_links":{"next":{"href":"http://[::1:bad"}},"_embedded":{"records":[]}}`)
	}))
	defer server.Close()

	// One record on page 1 keeps the loop alive; pagination link is malformed
	// so the walker must error out.
	page := 0
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		page++
		w.Header().Set("Content-Type", "application/json")
		if page == 1 {
			fmt.Fprintf(w, `{"_links":{"next":{"href":"http://[::1:bad"}},"_embedded":{"records":[{"type":"payment","from":%q,"to":"GAA","asset_code":"EURMTL","asset_issuer":%q,"amount":"1.0000000","created_at":"2026-05-07T10:00:00Z","transaction":{"memo":"mtl div 07/05/2026"}}]}}`, distributorAddr, domain.IssuerAddress)
			return
		}
		fmt.Fprint(w, `{"_links":{"next":{"href":""}},"_embedded":{"records":[]}}`)
	})

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	since := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	_, err := client.FetchDividendActivity(context.Background(), distributorAddr, nil, since)
	if err == nil {
		t.Fatal("expected error on malformed pagination link, got nil (silent truncation)")
	}
}
