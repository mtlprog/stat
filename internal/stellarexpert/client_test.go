package stellarexpert

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const samplePayload = `[
  {"ts":1622505600,"payments_amount":1000000000},
  {"ts":1622592000,"payments_amount":2500000000},
  {"ts":1622678400,"payments_amount":0},
  {"ts":1622764800,"payments_amount":7500000000}
]`

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL)
}

// 1622592000 == 2021-06-02 00:00 UTC. Cumulative through that day is
// 1_000_000_000 + 2_500_000_000 = 3_500_000_000 stroops = 350.00 EURMTL.
// Daily for that date is 2_500_000_000 stroops = 250.00 EURMTL.
func TestFetchEURMTLPaymentStatsHappyPath(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(samplePayload))
	})

	date := time.Date(2021, 6, 2, 0, 0, 0, 0, time.UTC)
	stats, err := client.FetchEURMTLPaymentStats(context.Background(), date)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.Daily.String() != "250" {
		t.Errorf("Daily = %s, want 250", stats.Daily)
	}
	if stats.Cumulative.String() != "350" {
		t.Errorf("Cumulative = %s, want 350", stats.Cumulative)
	}
}

// Cumulative MUST stop at the target date — points after it must not count.
// 1622678400 == 2021-06-03; cumulative through that day = 3.5e9 + 0 = 350,
// daily = 0 (legitimate zero, still found).
func TestFetchEURMTLPaymentStatsCumulativeStopsAtTarget(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(samplePayload))
	})

	date := time.Date(2021, 6, 3, 0, 0, 0, 0, time.UTC)
	stats, err := client.FetchEURMTLPaymentStats(context.Background(), date)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.Daily.String() != "0" {
		t.Errorf("Daily = %s, want 0 (legitimate zero day)", stats.Daily)
	}
	if stats.Cumulative.String() != "350" {
		t.Errorf("Cumulative = %s, want 350 (sum through 2021-06-03)", stats.Cumulative)
	}
}

// stats-history binned to UTC midnight: an arbitrary intra-day timestamp
// must still align to the right `ts` row.
func TestFetchEURMTLPaymentStatsTruncatesDateToUTCMidnight(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(samplePayload))
	})

	date := time.Date(2021, 6, 2, 14, 37, 12, 0, time.UTC)
	stats, err := client.FetchEURMTLPaymentStats(context.Background(), date)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.Daily.String() != "250" {
		t.Errorf("Daily = %s, want 250 (intra-day timestamp must truncate)", stats.Daily)
	}
}

func TestFetchEURMTLPaymentStatsHTTPError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server boom"))
	})

	_, err := client.FetchEURMTLPaymentStats(context.Background(), time.Now().UTC())
	if err == nil {
		t.Fatal("err = nil, want HTTP error")
	}
	if errors.Is(err, ErrNoDailyEntry) {
		t.Fatal("HTTP failure must NOT collapse to ErrNoDailyEntry — that's a sticky-fallback signal reserved for missing rows")
	}
}

func TestFetchEURMTLPaymentStatsMalformedJSON(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"not": "an array"}`))
	})

	_, err := client.FetchEURMTLPaymentStats(context.Background(), time.Now().UTC())
	if err == nil {
		t.Fatal("err = nil, want decode error")
	}
}

// Empty payload must NOT collapse to ErrNoDailyEntry — that sentinel is
// reserved for "data not yet ingested for this specific date" against an
// otherwise-healthy dataset. An empty array is an upstream symptom and must
// surface as a real error.
func TestFetchEURMTLPaymentStatsEmptyArrayIsRealError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})

	_, err := client.FetchEURMTLPaymentStats(context.Background(), time.Now().UTC())
	if err == nil {
		t.Fatal("err = nil, want non-sentinel error on empty payload")
	}
	if errors.Is(err, ErrNoDailyEntry) {
		t.Fatalf("err = %v, must NOT collapse to ErrNoDailyEntry", err)
	}
}

// Target outside the freshness window (latest point >48h older than target)
// must surface as a real error, not the sentinel — it likely indicates an
// upstream outage we want logged loud.
func TestFetchEURMTLPaymentStatsStaleDatasetIsRealError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(samplePayload))
	})

	// 2099 is years past the sample's last point (2021-06-04).
	_, err := client.FetchEURMTLPaymentStats(context.Background(), time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("err = nil, want real error when latest ts is stale beyond freshnessWindow")
	}
	if errors.Is(err, ErrNoDailyEntry) {
		t.Fatalf("err = %v, must NOT collapse to ErrNoDailyEntry", err)
	}
}

// Target predates the entire dataset → real error, not the sentinel.
func TestFetchEURMTLPaymentStatsTargetBeforeDataset(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(samplePayload))
	})

	_, err := client.FetchEURMTLPaymentStats(context.Background(), time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("err = nil, want error when target is before dataset")
	}
	if errors.Is(err, ErrNoDailyEntry) {
		t.Fatalf("err = %v, must NOT collapse to ErrNoDailyEntry", err)
	}
}

// Endpoint may flip ordering at any time; the client must defensively sort
// before accumulating. With a target inside the dataset, descending input
// must produce the same Stats as ascending input.
func TestFetchEURMTLPaymentStatsHandlesDescendingOrder(t *testing.T) {
	descending := `[
  {"ts":1622764800,"payments_amount":7500000000},
  {"ts":1622678400,"payments_amount":0},
  {"ts":1622592000,"payments_amount":2500000000},
  {"ts":1622505600,"payments_amount":1000000000}
]`
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(descending))
	})

	date := time.Date(2021, 6, 2, 0, 0, 0, 0, time.UTC)
	stats, err := client.FetchEURMTLPaymentStats(context.Background(), date)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.Daily.String() != "250" {
		t.Errorf("Daily = %s, want 250 (descending input must sort)", stats.Daily)
	}
	if stats.Cumulative.String() != "350" {
		t.Errorf("Cumulative = %s, want 350 (descending input must sort)", stats.Cumulative)
	}
}

// "Gap day" — ingester is keeping up overall (recent points exist) but the
// exact requested date is missing. This is the legitimate ErrNoDailyEntry
// signal: stellar.expert has fresh data, just not for this specific day.
func TestFetchEURMTLPaymentStatsGapDayReturnsSentinel(t *testing.T) {
	// samplePayload covers 2021-06-01..2021-06-04 contiguously. Ask for
	// 2021-06-05 — well within freshnessWindow of 06-04 (24h), so the
	// outage-detector doesn't fire, but no exact match exists either.
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(samplePayload))
	})

	target := time.Date(2021, 6, 5, 0, 0, 0, 0, time.UTC)
	_, err := client.FetchEURMTLPaymentStats(context.Background(), target)
	if !errors.Is(err, ErrNoDailyEntry) {
		t.Fatalf("err = %v, want ErrNoDailyEntry for in-window gap day", err)
	}
}
