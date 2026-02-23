package horizon

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

// --- accountBalanceForAsset tests ---

func TestAccountBalanceForAssetFound(t *testing.T) {
	rec := horizonAccountRecord{
		AccountID: "GABC",
		Balances: []horizonAccountBalance{
			{Balance: "100.5000000", AssetCode: "MTL", AssetIssuer: "GISSUER"},
		},
	}
	asset := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER"}

	bal, ok := accountBalanceForAsset(rec, asset)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !bal.Equal(decimal.RequireFromString("100.5")) {
		t.Errorf("balance = %s, want 100.5", bal)
	}
}

func TestAccountBalanceForAssetNotFound(t *testing.T) {
	rec := horizonAccountRecord{
		AccountID: "GABC",
		Balances: []horizonAccountBalance{
			{Balance: "50.0000000", AssetCode: "EURMTL", AssetIssuer: "GISSUER"},
		},
	}
	asset := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER"}

	_, ok := accountBalanceForAsset(rec, asset)
	if ok {
		t.Error("expected ok=false for missing asset")
	}
}

func TestAccountBalanceForAssetEmptyBalances(t *testing.T) {
	rec := horizonAccountRecord{AccountID: "GABC"}
	asset := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER"}

	_, ok := accountBalanceForAsset(rec, asset)
	if ok {
		t.Error("expected ok=false for empty balances")
	}
}

func TestAccountBalanceForAssetParseError(t *testing.T) {
	rec := horizonAccountRecord{
		AccountID: "GABC",
		Balances: []horizonAccountBalance{
			{Balance: "not_a_number", AssetCode: "MTL", AssetIssuer: "GISSUER"},
		},
	}
	asset := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER"}

	bal, ok := accountBalanceForAsset(rec, asset)
	if ok {
		t.Error("expected ok=false on parse error")
	}
	if !bal.IsZero() {
		t.Errorf("balance = %s, want zero on parse error", bal)
	}
}

func TestAccountBalanceForAssetIssuerMismatch(t *testing.T) {
	rec := horizonAccountRecord{
		AccountID: "GABC",
		Balances: []horizonAccountBalance{
			{Balance: "100.0000000", AssetCode: "MTL", AssetIssuer: "GWRONG"},
		},
	}
	asset := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER"}

	_, ok := accountBalanceForAsset(rec, asset)
	if ok {
		t.Error("expected ok=false when issuer does not match")
	}
}

// --- paginateAccounts tests ---

func TestPaginateAccountsSinglePage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"_links": {"next": {"href": ""}},
			"_embedded": {
				"records": [
					{"account_id": "A", "balances": []},
					{"account_id": "B", "balances": []}
				]
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	asset := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER"}

	var ids []string
	err := client.paginateAccounts(context.Background(), asset, func(rec horizonAccountRecord) bool {
		ids = append(ids, rec.AccountID)
		return true
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("got %d records, want 2", len(ids))
	}
}

func TestPaginateAccountsMultiPage(t *testing.T) {
	page := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		page++
		if page == 1 {
			nextURL := fmt.Sprintf("http://%s/accounts?asset=MTL:GISSUER&cursor=A&limit=200", r.Host)
			fmt.Fprintf(w, `{
				"_links": {"next": {"href": %q}},
				"_embedded": {
					"records": [{"account_id": "A", "balances": []}]
				}
			}`, nextURL)
		} else {
			w.Write([]byte(`{
				"_links": {"next": {"href": ""}},
				"_embedded": {
					"records": [{"account_id": "B", "balances": []}]
				}
			}`))
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	asset := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER"}

	var ids []string
	err := client.paginateAccounts(context.Background(), asset, func(rec horizonAccountRecord) bool {
		ids = append(ids, rec.AccountID)
		return true
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("got %d records, want 2", len(ids))
	}
	if ids[0] != "A" || ids[1] != "B" {
		t.Errorf("ids = %v, want [A B]", ids)
	}
}

func TestPaginateAccountsEarlyTermination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"_links": {"next": {"href": ""}},
			"_embedded": {
				"records": [
					{"account_id": "A", "balances": []},
					{"account_id": "B", "balances": []},
					{"account_id": "C", "balances": []}
				]
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	asset := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER"}

	var ids []string
	err := client.paginateAccounts(context.Background(), asset, func(rec horizonAccountRecord) bool {
		ids = append(ids, rec.AccountID)
		return len(ids) < 2 // stop after 2
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("got %d records, want 2 (early termination)", len(ids))
	}
}

func TestPaginateAccountsEmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"_links": {"next": {"href": ""}},
			"_embedded": {"records": []}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	asset := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER"}

	called := false
	err := client.paginateAccounts(context.Background(), asset, func(rec horizonAccountRecord) bool {
		called = true
		return true
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("callback should not be called on empty response")
	}
}

func TestPaginateAccountsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`internal error`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	asset := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER"}

	err := client.paginateAccounts(context.Background(), asset, func(rec horizonAccountRecord) bool {
		return true
	})
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}

// --- FetchAssetHolderCountByBalance tests ---

func TestFetchAssetHolderCountByBalanceNativeRejected(t *testing.T) {
	client := NewClient("http://unused", 1, 10*time.Millisecond)
	_, err := client.FetchAssetHolderCountByBalance(context.Background(), domain.XLMAsset(), decimal.NewFromInt(1))
	if err == nil {
		t.Fatal("expected error for native asset")
	}
}

func TestFetchAssetHolderCountByBalanceFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"_links": {"next": {"href": ""}},
			"_embedded": {
				"records": [
					{
						"account_id": "A",
						"balances": [{"asset_code": "MTL", "asset_issuer": "GISSUER", "balance": "0.5000000"}]
					},
					{
						"account_id": "B",
						"balances": [{"asset_code": "MTL", "asset_issuer": "GISSUER", "balance": "1.0000000"}]
					},
					{
						"account_id": "C",
						"balances": [{"asset_code": "MTL", "asset_issuer": "GISSUER", "balance": "5.0000000"}]
					},
					{
						"account_id": "D",
						"balances": [{"asset_code": "MTL", "asset_issuer": "GISSUER", "balance": "0.0000000"}]
					}
				]
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	asset := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER"}

	count, err := client.FetchAssetHolderCountByBalance(context.Background(), asset, decimal.NewFromInt(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2 (accounts B and C with balance >= 1)", count)
	}
}

func TestFetchAssetHolderCountByBalanceZeroBalanceExcluded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"_links": {"next": {"href": ""}},
			"_embedded": {
				"records": [
					{
						"account_id": "A",
						"balances": [{"asset_code": "EURMTL", "asset_issuer": "GISSUER", "balance": "0.0000000"}]
					},
					{
						"account_id": "B",
						"balances": [{"asset_code": "EURMTL", "asset_issuer": "GISSUER", "balance": "0.0000001"}]
					}
				]
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	asset := domain.AssetInfo{Code: "EURMTL", Issuer: "GISSUER"}

	// minNonZero = 1 stroop (0.0000001)
	count, err := client.FetchAssetHolderCountByBalance(context.Background(), asset, decimal.New(1, -7))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 (only account B has non-zero balance)", count)
	}
}

// --- FetchAssetHolderIDsByBalance tests ---

func TestFetchAssetHolderIDsByBalanceNativeRejected(t *testing.T) {
	client := NewClient("http://unused", 1, 10*time.Millisecond)
	_, err := client.FetchAssetHolderIDsByBalance(context.Background(), domain.XLMAsset(), decimal.NewFromInt(1))
	if err == nil {
		t.Fatal("expected error for native asset")
	}
}

func TestFetchAssetHolderIDsByBalanceFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"_links": {"next": {"href": ""}},
			"_embedded": {
				"records": [
					{
						"account_id": "A",
						"balances": [{"asset_code": "MTL", "asset_issuer": "GISSUER", "balance": "0.5000000"}]
					},
					{
						"account_id": "B",
						"balances": [{"asset_code": "MTL", "asset_issuer": "GISSUER", "balance": "2.0000000"}]
					},
					{
						"account_id": "C",
						"balances": [{"asset_code": "MTL", "asset_issuer": "GISSUER", "balance": "10.0000000"}]
					}
				]
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	asset := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER"}

	ids, err := client.FetchAssetHolderIDsByBalance(context.Background(), asset, decimal.NewFromInt(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("got %d IDs, want 2", len(ids))
	}
	if ids[0] != "B" || ids[1] != "C" {
		t.Errorf("ids = %v, want [B C]", ids)
	}
}
