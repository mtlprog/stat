package price

import (
	"testing"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/horizon"
)

func TestCalculateAMMSpotValid(t *testing.T) {
	pool := horizon.HorizonLiquidityPool{
		Reserves: []horizon.HorizonLiquidityPoolReserve{
			{Asset: "MTL:GISSUER", Amount: "1000"},
			{Asset: domain.EURMTLAsset().Canonical(), Amount: "500"},
		},
	}

	source := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER", Type: domain.AssetTypeCreditAlphanum4}
	spot := calculateAMMSpot(pool, source)
	if spot == nil {
		t.Fatal("expected non-nil spot price")
	}
	if *spot != "0.5" {
		t.Errorf("spot = %q, want 0.5", *spot)
	}
}

func TestCalculateAMMSpotInvalidReserveCount(t *testing.T) {
	pool := horizon.HorizonLiquidityPool{
		Reserves: []horizon.HorizonLiquidityPoolReserve{
			{Asset: "MTL:GISSUER", Amount: "1000"},
		},
	}

	source := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER", Type: domain.AssetTypeCreditAlphanum4}
	spot := calculateAMMSpot(pool, source)
	if spot != nil {
		t.Errorf("expected nil for invalid reserve count, got %q", *spot)
	}
}

func TestCalculateAMMSpotZeroReserve(t *testing.T) {
	pool := horizon.HorizonLiquidityPool{
		Reserves: []horizon.HorizonLiquidityPoolReserve{
			{Asset: "MTL:GISSUER", Amount: "0"},
			{Asset: domain.EURMTLAsset().Canonical(), Amount: "500"},
		},
	}

	source := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER", Type: domain.AssetTypeCreditAlphanum4}
	spot := calculateAMMSpot(pool, source)
	if spot != nil {
		t.Errorf("expected nil for zero reserve, got %v", spot)
	}
}

func TestCalculateAMMSpotUnparseableAmount(t *testing.T) {
	pool := horizon.HorizonLiquidityPool{
		Reserves: []horizon.HorizonLiquidityPoolReserve{
			{Asset: "MTL:GISSUER", Amount: "not-a-number"},
			{Asset: domain.EURMTLAsset().Canonical(), Amount: "500"},
		},
	}

	source := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER", Type: domain.AssetTypeCreditAlphanum4}
	spot := calculateAMMSpot(pool, source)
	if spot != nil {
		t.Errorf("expected nil for unparseable amount, got %v", spot)
	}
}

func TestFetchOrderbookDataBestSourceSelection(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockHorizon
		wantSource string
	}{
		{
			name: "orderbook wins over AMM",
			mock: &mockHorizon{
				orderbook: horizon.HorizonOrderbook{
					Asks: []horizon.HorizonOrderbookEntry{{Price: "0.4", Amount: "100"}},
				},
				pools: []horizon.HorizonLiquidityPool{{
					ID: "pool",
					Reserves: []horizon.HorizonLiquidityPoolReserve{
						{Asset: "MTL:GISSUER", Amount: "1000"},
						{Asset: domain.EURMTLAsset().Canonical(), Amount: "600"},
					},
				}},
			},
			wantSource: "orderbook",
		},
		{
			name: "AMM wins over orderbook",
			mock: &mockHorizon{
				orderbook: horizon.HorizonOrderbook{
					Asks: []horizon.HorizonOrderbookEntry{{Price: "0.8", Amount: "100"}},
				},
				pools: []horizon.HorizonLiquidityPool{{
					ID: "pool",
					Reserves: []horizon.HorizonLiquidityPoolReserve{
						{Asset: "MTL:GISSUER", Amount: "1000"},
						{Asset: domain.EURMTLAsset().Canonical(), Amount: "600"},
					},
				}},
			},
			wantSource: "amm",
		},
		{
			name: "orderbook only",
			mock: &mockHorizon{
				orderbook: horizon.HorizonOrderbook{
					Bids: []horizon.HorizonOrderbookEntry{{Price: "0.5", Amount: "100"}},
				},
				poolsErr: nil,
			},
			wantSource: "orderbook",
		},
		{
			name: "both empty",
			mock: &mockHorizon{
				orderbook: horizon.HorizonOrderbook{},
			},
			wantSource: "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(tt.mock)
			source := domain.AssetInfo{Code: "MTL", Issuer: "GISSUER", Type: domain.AssetTypeCreditAlphanum4}
			data, err := svc.fetchOrderbookData(t.Context(), source, domain.EURMTLAsset())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if data.BestSource != tt.wantSource {
				t.Errorf("BestSource = %q, want %q", data.BestSource, tt.wantSource)
			}
		})
	}
}
