package horizon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mtlprog/stat/internal/domain"
)

func TestFetchTradesParsesPair(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("base_asset_code"); got != "MTL" {
			t.Errorf("base_asset_code = %q, want MTL", got)
		}
		if got := r.URL.Query().Get("counter_asset_code"); got != "EURMTL" {
			t.Errorf("counter_asset_code = %q, want EURMTL", got)
		}
		if got := r.URL.Query().Get("order"); got != "desc" {
			t.Errorf("order = %q, want desc", got)
		}
		if got := r.URL.Query().Get("limit"); got != "100" {
			t.Errorf("limit = %q, want 100", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"_embedded": {"records": [
				{"base_asset_code": "MTL", "counter_asset_code": "EURMTL", "price": {"n": "17", "d": "2"}},
				{"base_asset_code": "EURMTL", "counter_asset_code": "MTL", "price": {"n": "1", "d": "8"}}
			]}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	mtl := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER", Type: domain.AssetTypeCreditAlphanum4}
	eurmtl := domain.AssetInfo{Code: "EURMTL", Issuer: "GISSUER2", Type: domain.AssetTypeCreditAlphanum12}

	trades, err := client.FetchTrades(context.Background(), mtl, eurmtl, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(trades) != 2 {
		t.Fatalf("got %d trades, want 2", len(trades))
	}
	if trades[0].Price.N != "17" || trades[0].Price.D != "2" {
		t.Errorf("trade[0] price = %s/%s, want 17/2", trades[0].Price.N, trades[0].Price.D)
	}
	if trades[1].BaseAssetCode != "EURMTL" {
		t.Errorf("trade[1] base = %q, want EURMTL (inverted ordering)", trades[1].BaseAssetCode)
	}
}

func TestFetchTradesEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"_embedded": {"records": []}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	mtl := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER", Type: domain.AssetTypeCreditAlphanum4}
	eurmtl := domain.AssetInfo{Code: "EURMTL", Issuer: "GISSUER2", Type: domain.AssetTypeCreditAlphanum12}

	trades, err := client.FetchTrades(context.Background(), mtl, eurmtl, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(trades) != 0 {
		t.Errorf("got %d trades, want 0", len(trades))
	}
}

func TestFetchTradesHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`internal error`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	mtl := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER", Type: domain.AssetTypeCreditAlphanum4}
	eurmtl := domain.AssetInfo{Code: "EURMTL", Issuer: "GISSUER2", Type: domain.AssetTypeCreditAlphanum12}

	if _, err := client.FetchTrades(context.Background(), mtl, eurmtl, 100); err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}
