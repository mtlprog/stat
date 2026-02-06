package external

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
	if prices["BTC"] != 55000.00 {
		t.Errorf("BTC = %v, want 55000", prices["BTC"])
	}

	// ETH direct
	if prices["ETH"] != 2500.00 {
		t.Errorf("ETH = %v, want 2500", prices["ETH"])
	}

	// XLM direct
	if prices["XLM"] != 0.10 {
		t.Errorf("XLM = %v, want 0.10", prices["XLM"])
	}

	// Sats = BTC / 100_000_000
	expectedSats := 55000.00 / 100_000_000
	if math.Abs(prices["Sats"]-expectedSats) > 1e-12 {
		t.Errorf("Sats = %v, want %v", prices["Sats"], expectedSats)
	}

	// USD via tether
	if prices["USD"] != 0.92 {
		t.Errorf("USD = %v, want 0.92", prices["USD"])
	}

	// AU = gold per oz / 31.1035
	expectedAU := 1800.00 / 31.1035
	if math.Abs(prices["AU"]-expectedAU) > 0.01 {
		t.Errorf("AU = %v, want ~%v", prices["AU"], expectedAU)
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
	if prices["BTC"] != 55000 {
		t.Errorf("BTC = %v, want 55000", prices["BTC"])
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
