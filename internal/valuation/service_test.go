package valuation

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/horizon"
)

func TestLookupValuationNFTPrefersCost(t *testing.T) {
	valuations := []domain.AssetValuation{
		{TokenCode: "MTLART", ValuationType: domain.ValuationTypeNFT, SourceAccount: "GOWNER", RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "100"}},
		{TokenCode: "MTLART", ValuationType: domain.ValuationTypeUnit, SourceAccount: "GOWNER", RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "50"}},
	}

	result := LookupValuation("MTLART", "0.0000001", "GOWNER", valuations)
	if result == nil {
		t.Fatal("expected valuation, got nil")
	}
	if result.ValuationType != domain.ValuationTypeNFT {
		t.Errorf("got type %q, want nft (NFTs prefer _COST)", result.ValuationType)
	}
}

func TestLookupValuationRegularPrefers1COST(t *testing.T) {
	valuations := []domain.AssetValuation{
		{TokenCode: "TOKEN", ValuationType: domain.ValuationTypeNFT, SourceAccount: "GOWNER", RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "100"}},
		{TokenCode: "TOKEN", ValuationType: domain.ValuationTypeUnit, SourceAccount: "GOWNER", RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "50"}},
	}

	result := LookupValuation("TOKEN", "1000.0000000", "GOWNER", valuations)
	if result == nil {
		t.Fatal("expected valuation, got nil")
	}
	if result.ValuationType != domain.ValuationTypeUnit {
		t.Errorf("got type %q, want unit (regular tokens prefer _1COST)", result.ValuationType)
	}
}

func TestLookupValuationOwnerPriority(t *testing.T) {
	valuations := []domain.AssetValuation{
		{TokenCode: "TOKEN", ValuationType: domain.ValuationTypeUnit, SourceAccount: "GOTHER", RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "200"}},
		{TokenCode: "TOKEN", ValuationType: domain.ValuationTypeUnit, SourceAccount: "GOWNER", RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "100"}},
	}

	result := LookupValuation("TOKEN", "10", "GOWNER", valuations)
	if result == nil {
		t.Fatal("expected valuation, got nil")
	}
	if result.RawValue.Value != "100" {
		t.Errorf("Value = %q, want 100 (owner priority)", result.RawValue.Value)
	}
}

func TestLookupValuationFallback(t *testing.T) {
	// Only _COST available for a regular token, should fall back
	valuations := []domain.AssetValuation{
		{TokenCode: "TOKEN", ValuationType: domain.ValuationTypeNFT, SourceAccount: "GOTHER", RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "500"}},
	}

	result := LookupValuation("TOKEN", "100", "GOWNER", valuations)
	if result == nil {
		t.Fatal("expected fallback valuation, got nil")
	}
	if result.ValuationType != domain.ValuationTypeNFT {
		t.Errorf("got type %q, want nft (fallback)", result.ValuationType)
	}
}

func TestDeduplicateValuationsDeterministic(t *testing.T) {
	// Same valuations in different input orders should produce same result
	v1 := domain.AssetValuation{TokenCode: "TOKEN", ValuationType: domain.ValuationTypeUnit, SourceAccount: "GAAA", RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "100"}}
	v2 := domain.AssetValuation{TokenCode: "TOKEN", ValuationType: domain.ValuationTypeUnit, SourceAccount: "GZZZ", RawValue: domain.ValuationValue{Type: domain.ValuationValueEURMTL, Value: "200"}}

	// Order 1: GZZZ first
	result1 := deduplicateValuations([]domain.AssetValuation{v2, v1})
	// Order 2: GAAA first
	result2 := deduplicateValuations([]domain.AssetValuation{v1, v2})

	if len(result1) != 1 || len(result2) != 1 {
		t.Fatalf("expected 1 deduped valuation each, got %d and %d", len(result1), len(result2))
	}

	// Both should select the same account (lexicographically first: GAAA)
	if result1[0].SourceAccount != result2[0].SourceAccount {
		t.Errorf("non-deterministic dedup: order1=%q, order2=%q", result1[0].SourceAccount, result2[0].SourceAccount)
	}
	if result1[0].SourceAccount != "GAAA" {
		t.Errorf("dedup should pick lexicographically first account, got %q", result1[0].SourceAccount)
	}
}

func TestLookupValuationNotFound(t *testing.T) {
	valuations := []domain.AssetValuation{
		{TokenCode: "OTHER", ValuationType: domain.ValuationTypeUnit, SourceAccount: "GOTHER"},
	}

	result := LookupValuation("TOKEN", "100", "GOWNER", valuations)
	if result != nil {
		t.Error("expected nil for non-matching token")
	}
}

type failingAccountFetcher struct{}

func (f *failingAccountFetcher) FetchAccount(_ context.Context, _ string) (horizon.HorizonAccount, error) {
	return horizon.HorizonAccount{}, errors.New("horizon unavailable")
}

func TestFetchAllValuationsAllFail(t *testing.T) {
	svc := NewService(&failingAccountFetcher{})
	_, err := svc.FetchAllValuations(context.Background())
	if err == nil {
		t.Error("expected error when all account scans fail, got nil")
	}
}

type partialFailFetcher struct{ successID string }

func (f *partialFailFetcher) FetchAccount(_ context.Context, accountID string) (horizon.HorizonAccount, error) {
	if accountID == f.successID {
		return horizon.HorizonAccount{
			ID: accountID,
			Data: map[string]string{
				"TOKEN_1COST": base64.StdEncoding.EncodeToString([]byte("100")),
			},
		}, nil
	}
	return horizon.HorizonAccount{}, errors.New("horizon unavailable")
}

func TestFetchAllValuationsPartialFailure(t *testing.T) {
	firstAccount := domain.AccountRegistry()[0].Address
	svc := NewService(&partialFailFetcher{successID: firstAccount})
	valuations, err := svc.FetchAllValuations(context.Background())
	if err != nil {
		t.Fatalf("expected no error on partial failure, got: %v", err)
	}
	if len(valuations) == 0 {
		t.Error("expected at least one valuation from the successful account")
	}
}
