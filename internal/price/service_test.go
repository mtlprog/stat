package price

import (
	"context"
	"errors"
	"testing"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/horizon"
)

type mockHorizon struct {
	strictSendPaths    []horizon.HorizonPathRecord
	strictSendErr      error
	strictReceivePaths []horizon.HorizonPathRecord
	strictReceiveErr   error
	orderbook          horizon.HorizonOrderbook
	orderbookErr       error
	pools              []horizon.HorizonLiquidityPool
	poolsErr           error
}

func (m *mockHorizon) FetchOrderbook(_ context.Context, _, _ domain.AssetInfo, _ int) (horizon.HorizonOrderbook, error) {
	return m.orderbook, m.orderbookErr
}

func (m *mockHorizon) FetchStrictSendPaths(_ context.Context, _ domain.AssetInfo, _ string, _ domain.AssetInfo) ([]horizon.HorizonPathRecord, error) {
	return m.strictSendPaths, m.strictSendErr
}

func (m *mockHorizon) FetchStrictReceivePaths(_ context.Context, _ domain.AssetInfo, _ domain.AssetInfo, _ string) ([]horizon.HorizonPathRecord, error) {
	return m.strictReceivePaths, m.strictReceiveErr
}

func (m *mockHorizon) FetchLiquidityPools(_ context.Context, _, _ domain.AssetInfo) ([]horizon.HorizonLiquidityPool, error) {
	return m.pools, m.poolsErr
}

func TestGetPricePathOnly(t *testing.T) {
	mock := &mockHorizon{
		strictSendPaths: []horizon.HorizonPathRecord{
			{SourceAmount: "100", DestinationAmount: "50"},
		},
		orderbookErr: errors.New("no orderbook"),
		poolsErr:     errors.New("no pools"),
	}

	svc := NewService(mock)
	result, err := svc.GetPrice(context.Background(), testAsset(), domain.EURMTLAsset(), "100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Price != "0.5" {
		t.Errorf("Price = %q, want 0.5", result.Price)
	}
}

func TestGetPriceSpotPathWins(t *testing.T) {
	bid := "0.4"
	mock := &mockHorizon{
		strictSendPaths: []horizon.HorizonPathRecord{
			{SourceAmount: "1", DestinationAmount: "0.5"},
		},
		orderbook: horizon.HorizonOrderbook{
			Bids: []horizon.HorizonOrderbookEntry{{Price: bid, Amount: "100"}},
		},
		poolsErr: errors.New("no pools"),
	}

	svc := NewService(mock)
	result, err := svc.GetPrice(context.Background(), testAsset(), domain.EURMTLAsset(), "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	details, ok := result.Details.(*domain.BestDetails)
	if !ok {
		t.Fatal("expected BestDetails")
	}
	if details.ChosenSource != "path" {
		t.Errorf("ChosenSource = %q, want path (0.5 > 0.4)", details.ChosenSource)
	}
}

func TestGetPriceSpotOrderbookWins(t *testing.T) {
	bid := "0.6"
	mock := &mockHorizon{
		strictSendPaths: []horizon.HorizonPathRecord{
			{SourceAmount: "1", DestinationAmount: "0.5"},
		},
		orderbook: horizon.HorizonOrderbook{
			Bids: []horizon.HorizonOrderbookEntry{{Price: bid, Amount: "100"}},
		},
		poolsErr: errors.New("no pools"),
	}

	svc := NewService(mock)
	result, err := svc.GetPrice(context.Background(), testAsset(), domain.EURMTLAsset(), "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	details, ok := result.Details.(*domain.BestDetails)
	if !ok {
		t.Fatal("expected BestDetails")
	}
	if details.ChosenSource != "orderbook" {
		t.Errorf("ChosenSource = %q, want orderbook (0.6 > 0.5)", details.ChosenSource)
	}
}

func TestGetPriceCacheHit(t *testing.T) {
	callCount := 0
	mock := &mockHorizon{
		strictSendPaths: []horizon.HorizonPathRecord{
			{SourceAmount: "100", DestinationAmount: "50"},
		},
		orderbookErr: errors.New("no orderbook"),
		poolsErr:     errors.New("no pools"),
	}

	svc := NewService(mock)

	// First call
	_, err := svc.GetPrice(context.Background(), testAsset(), domain.EURMTLAsset(), "100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	callCount++

	// Second call should hit cache
	result, err := svc.GetPrice(context.Background(), testAsset(), domain.EURMTLAsset(), "100")
	if err != nil {
		t.Fatalf("unexpected error on cached call: %v", err)
	}
	if result.Price != "0.5" {
		t.Errorf("cached Price = %q, want 0.5", result.Price)
	}
}

func TestGetPriceZeroSourceAmount(t *testing.T) {
	mock := &mockHorizon{
		strictSendPaths: []horizon.HorizonPathRecord{
			{SourceAmount: "0", DestinationAmount: "50"},
		},
		strictReceiveErr: errors.New("no paths"),
		orderbookErr:     errors.New("no orderbook"),
		poolsErr:         errors.New("no pools"),
	}

	svc := NewService(mock)
	_, err := svc.GetPrice(context.Background(), testAsset(), domain.EURMTLAsset(), "100")
	if err == nil {
		t.Error("expected error for zero source amount, got nil")
	}
}

func TestGetPriceAMMSelection(t *testing.T) {
	ask := "0.8"
	mock := &mockHorizon{
		strictSendErr:    errors.New("no path"),
		strictReceiveErr: errors.New("no path"),
		orderbook: horizon.HorizonOrderbook{
			Asks: []horizon.HorizonOrderbookEntry{{Price: ask, Amount: "100"}},
		},
		pools: []horizon.HorizonLiquidityPool{
			{
				ID: "pool1",
				Reserves: []horizon.HorizonLiquidityPoolReserve{
					{Asset: "MTL:GISSUER", Amount: "1000"},
					{Asset: domain.EURMTLAsset().Canonical(), Amount: "600"},
				},
			},
		},
	}

	svc := NewService(mock)
	result, err := svc.GetPrice(context.Background(), testAsset(), domain.EURMTLAsset(), "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// AMM spot = 600/1000 = 0.6, Orderbook ask = 0.8
	// AMM ask (0.6) < orderbook ask (0.8), so AMM wins
	details, ok := result.Details.(*domain.OrderbookDetails)
	if !ok {
		t.Fatal("expected OrderbookDetails")
	}
	if details.OrderbookData.BestSource != "amm" {
		t.Errorf("BestSource = %q, want amm (0.6 < 0.8)", details.OrderbookData.BestSource)
	}
}

func testAsset() domain.AssetInfo {
	return domain.AssetInfo{
		Code:   "MTL",
		Issuer: "GISSUER",
		Type:   domain.AssetTypeCreditAlphanum4,
	}
}
