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

// When stellar.expert hasn't ingested the requested date yet, return a
// sentinel so the caller can sticky-fallback instead of treating "data
// pending" as a hard error.
func TestFetchEURMTLPaymentStatsNoDailyEntry(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(samplePayload))
	})

	// 2099-01-01 is well after the sample's last point.
	date := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := client.FetchEURMTLPaymentStats(context.Background(), date)
	if !errors.Is(err, ErrNoDailyEntry) {
		t.Fatalf("err = %v, want ErrNoDailyEntry", err)
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
