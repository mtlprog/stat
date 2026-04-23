package horizon

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestFetchEURMTLPaymentVolumeSinglePage(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-1 * time.Hour).Format(time.RFC3339)
	old := now.Add(-48 * time.Hour).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"_links": {"next": {"href": ""}},
			"_embedded": {
				"records": [
					{"type": "payment", "amount": "100.00", "asset_code": "EURMTL", "asset_issuer": "GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V", "from": "A", "to": "B", "created_at": %q},
					{"type": "payment", "amount": "50.00", "asset_code": "EURMTL", "asset_issuer": "GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V", "from": "C", "to": "D", "created_at": %q},
					{"type": "payment", "amount": "999.00", "asset_code": "EURMTL", "asset_issuer": "GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V", "from": "E", "to": "F", "created_at": %q}
				]
			}
		}`, recent, recent, old)
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	since := now.Add(-24 * time.Hour)

	vol, err := client.FetchEURMTLPaymentVolume(context.Background(), since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the two recent payments (100 + 50), old one is before 'since'
	expected := decimal.NewFromInt(150)
	if !vol.Equal(expected) {
		t.Errorf("volume = %s, want %s", vol, expected)
	}
}

func TestFetchEURMTLPaymentVolumeFiltersNonPayments(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-1 * time.Hour).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"_links": {"next": {"href": ""}},
			"_embedded": {
				"records": [
					{"type": "payment", "amount": "100.00", "asset_code": "EURMTL", "asset_issuer": "GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V", "from": "A", "to": "B", "created_at": %q},
					{"type": "create_account", "amount": "50.00", "asset_code": "EURMTL", "asset_issuer": "GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V", "from": "C", "to": "D", "created_at": %q},
					{"type": "path_payment_strict_send", "amount": "25.00", "asset_code": "EURMTL", "asset_issuer": "GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V", "from": "E", "to": "F", "created_at": %q}
				]
			}
		}`, recent, recent, recent)
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	since := now.Add(-24 * time.Hour)

	vol, err := client.FetchEURMTLPaymentVolume(context.Background(), since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// payment (100) + path_payment_strict_send (25) = 125; create_account is filtered out
	expected := decimal.NewFromInt(125)
	if !vol.Equal(expected) {
		t.Errorf("volume = %s, want %s", vol, expected)
	}
}

func TestFetchEURMTLPaymentVolumeEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"_links": {"next": {"href": ""}},
			"_embedded": {"records": []}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	vol, err := client.FetchEURMTLPaymentVolume(context.Background(), time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !vol.IsZero() {
		t.Errorf("volume = %s, want 0", vol)
	}
}

func TestFetchEURMTLPaymentVolumeHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, 1, 10*time.Millisecond)
	_, err := client.FetchEURMTLPaymentVolume(context.Background(), time.Now().Add(-24*time.Hour))
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}
