package horizon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchAccountParsesBalances(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/accounts/GABC123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"id": "GABC123",
			"balances": [
				{
					"asset_type": "credit_alphanum4",
					"asset_code": "MTL",
					"asset_issuer": "GISSUER1",
					"balance": "100.0000000"
				},
				{
					"asset_type": "credit_alphanum12",
					"asset_code": "EURMTL",
					"asset_issuer": "GISSUER2",
					"balance": "500.5000000"
				},
				{
					"asset_type": "native",
					"balance": "1000.0000000"
				},
				{
					"asset_type": "liquidity_pool_shares",
					"liquidity_pool_id": "pool123",
					"balance": "50.0000000"
				}
			],
			"data": {
				"MTLART_COST": "MTAwMA=="
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	account, err := client.FetchAccount(context.Background(), "GABC123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if account.ID != "GABC123" {
		t.Errorf("ID = %q, want GABC123", account.ID)
	}
	if len(account.Balances) != 4 {
		t.Fatalf("balances count = %d, want 4", len(account.Balances))
	}

	// Check credit_alphanum4
	if account.Balances[0].AssetCode != "MTL" {
		t.Errorf("balance[0].AssetCode = %q, want MTL", account.Balances[0].AssetCode)
	}
	if account.Balances[0].Balance != "100.0000000" {
		t.Errorf("balance[0].Balance = %q, want 100.0000000", account.Balances[0].Balance)
	}

	// Check native
	if account.Balances[2].AssetType != "native" {
		t.Errorf("balance[2].AssetType = %q, want native", account.Balances[2].AssetType)
	}

	// Check LP shares
	if account.Balances[3].LiquidityPoolID != "pool123" {
		t.Errorf("balance[3].LiquidityPoolID = %q, want pool123", account.Balances[3].LiquidityPoolID)
	}

	// Check data entries
	if account.Data["MTLART_COST"] != "MTAwMA==" {
		t.Errorf("data[MTLART_COST] = %q, want MTAwMA==", account.Data["MTLART_COST"])
	}
}

func TestFetchAccountError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"status": 404, "title": "Resource Missing"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	_, err := client.FetchAccount(context.Background(), "GNOTEXIST")
	if err == nil {
		t.Fatal("expected error for missing account, got nil")
	}
}

// FetchAccountDataEntry has three meaningful states the caller discriminates:
// present-and-decoded, absent (HTTP 200, key missing), and bad base64.
func TestFetchAccountDataEntryPresent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// "MjAwOC42ODI5MjI4" = base64("2008.6829228")
		w.Write([]byte(`{"id":"GABC","balances":[],"data":{"LAST_DIVS":"MjAwOC42ODI5MjI4"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	val, present, err := client.FetchAccountDataEntry(context.Background(), "GABC", "LAST_DIVS")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !present {
		t.Fatal("present=false, want true")
	}
	if val != "2008.6829228" {
		t.Errorf("val = %q, want %q", val, "2008.6829228")
	}
}

func TestFetchAccountDataEntryAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"GABC","balances":[],"data":{"OtherKey":"YWJj"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	val, present, err := client.FetchAccountDataEntry(context.Background(), "GABC", "LAST_DIVS")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if present {
		t.Errorf("present=true, want false (key missing)")
	}
	if val != "" {
		t.Errorf("val = %q, want empty", val)
	}
}

func TestFetchAccountDataEntryBadBase64(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"GABC","balances":[],"data":{"LAST_DIVS":"not_base64!!!"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	_, _, err := client.FetchAccountDataEntry(context.Background(), "GABC", "LAST_DIVS")
	if err == nil {
		t.Fatal("expected base64 decode error, got nil")
	}
}
