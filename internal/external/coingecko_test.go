package external

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestFetchPricesAllSymbols(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"bitcoin": {"eur": 55000.00},
			"ethereum": {"eur": 2500.00},
			"stellar": {"eur": 0.10},
			"tether": {"eur": 0.92},
			"gold": {"eur": 1800.00}
		}`))
	}))
	defer server.Close()

	client := NewCoinGeckoClient(server.URL, 0, 1)
	prices, err := client.FetchPrices(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// BTC direct
	if !prices["BTC"].Equal(decimal.NewFromInt(55000)) {
		t.Errorf("BTC = %s, want 55000", prices["BTC"])
	}

	// ETH direct
	if !prices["ETH"].Equal(decimal.NewFromInt(2500)) {
		t.Errorf("ETH = %s, want 2500", prices["ETH"])
	}

	// XLM direct
	if !prices["XLM"].Equal(decimal.RequireFromString("0.1")) {
		t.Errorf("XLM = %s, want 0.1", prices["XLM"])
	}

	// Sats = BTC / 100_000_000
	expectedSats := decimal.NewFromInt(55000).Div(decimal.NewFromInt(100_000_000))
	if !prices["Sats"].Equal(expectedSats) {
		t.Errorf("Sats = %s, want %s", prices["Sats"], expectedSats)
	}

	// USD via tether
	if !prices["USD"].Equal(decimal.RequireFromString("0.92")) {
		t.Errorf("USD = %s, want 0.92", prices["USD"])
	}

	// AU = gold per oz / 31.1035
	expectedAU := decimal.NewFromInt(1800).Div(decimal.RequireFromString("31.1035"))
	if !prices["AU"].Equal(expectedAU) {
		t.Errorf("AU = %s, want %s", prices["AU"], expectedAU)
	}
}

func TestFetchPricesMissingEURKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// "bitcoin" is missing the "eur" key, so json.Number will be zero value (empty string)
		w.Write([]byte(`{
			"bitcoin": {},
			"ethereum": {"eur": 2500}
		}`))
	}))
	defer server.Close()

	client := NewCoinGeckoClient(server.URL, 0, 1)
	prices, err := client.FetchPrices(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// BTC and Sats should be missing (empty eur key → unparseable)
	if _, ok := prices["BTC"]; ok {
		t.Error("BTC should be skipped due to missing EUR key")
	}
	if _, ok := prices["Sats"]; ok {
		t.Error("Sats should be skipped due to missing BTC EUR key")
	}

	// ETH should still be present
	if !prices["ETH"].Equal(decimal.NewFromInt(2500)) {
		t.Errorf("ETH = %s, want 2500", prices["ETH"])
	}
}

func TestFetchPricesMissingSymbol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Only bitcoin present, everything else missing
		w.Write([]byte(`{"bitcoin": {"eur": 55000}}`))
	}))
	defer server.Close()

	client := NewCoinGeckoClient(server.URL, 0, 1)
	prices, err := client.FetchPrices(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// BTC and Sats should be present (both from bitcoin)
	if !prices["BTC"].Equal(decimal.NewFromInt(55000)) {
		t.Errorf("BTC = %s, want 55000", prices["BTC"])
	}

	// Others should be missing without error
	if _, ok := prices["ETH"]; ok {
		t.Error("ETH should be missing")
	}
	if _, ok := prices["AU"]; ok {
		t.Error("AU should be missing")
	}
}

func TestFetchPricesRetryOn429(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"bitcoin": {"eur": 55000}}`))
	}))
	defer server.Close()

	client := NewCoinGeckoClient(server.URL, 10*time.Millisecond, 2)
	prices, err := client.FetchPrices(context.Background())
	if err != nil {
		t.Fatalf("unexpected error after retry: %v", err)
	}
	if !prices["BTC"].Equal(decimal.NewFromInt(55000)) {
		t.Errorf("BTC = %s, want 55000", prices["BTC"])
	}
}

func TestFetchPricesContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1 * time.Second)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	client := NewCoinGeckoClient(server.URL, 0, 1)
	_, err := client.FetchPrices(ctx)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}
