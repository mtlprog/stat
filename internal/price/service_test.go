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

	if result.Details == nil || result.Details.Source != "best" {
		t.Fatal("expected PriceDetails with Source=best")
	}
	if result.Details.ChosenSource != "path" {
		t.Errorf("ChosenSource = %q, want path (0.5 > 0.4)", result.Details.ChosenSource)
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

	if result.Details == nil || result.Details.Source != "best" {
		t.Fatal("expected PriceDetails with Source=best")
	}
	if result.Details.ChosenSource != "orderbook" {
		t.Errorf("ChosenSource = %q, want orderbook (0.6 > 0.5)", result.Details.ChosenSource)
	}
}

func TestGetPriceCacheHit(t *testing.T) {
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
	if result.Details == nil || result.Details.Source != "orderbook" {
		t.Fatal("expected PriceDetails with Source=orderbook")
	}
	if result.Details.OrderbookData == nil {
		t.Fatal("expected OrderbookData to be non-nil")
	}
	if result.Details.OrderbookData.BestSource != "amm" {
		t.Errorf("BestSource = %q, want amm (0.6 < 0.8)", result.Details.OrderbookData.BestSource)
	}
}

func TestGetTokenPricesCrossRateFromEURMTL(t *testing.T) {
	// EURMTL price succeeds, XLM fails → derive XLM via cross-rate
	mock := &mockHorizon{
		strictSendPaths: []horizon.HorizonPathRecord{
			{SourceAmount: "1", DestinationAmount: "2.0"},
		},
		orderbookErr: errors.New("no orderbook"),
		poolsErr:     errors.New("no pools"),
	}

	svc := NewService(mock)

	result, err := svc.GetTokenPrices(context.Background(), testAsset(), "100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PriceEURMTL == "" {
		t.Error("expected non-empty EURMTL price")
	}
	// XLM price should also be available (either direct or via cross-rate)
	if result.PriceXLM == "" {
		t.Error("expected non-empty XLM price (direct or cross-rate)")
	}
}

func TestGetTokenPricesBothFail(t *testing.T) {
	mock := &mockHorizon{
		strictSendErr:    errors.New("no path"),
		strictReceiveErr: errors.New("no path"),
		orderbookErr:     errors.New("no orderbook"),
		poolsErr:         errors.New("no pools"),
	}

	svc := NewService(mock)
	_, err := svc.GetTokenPrices(context.Background(), testAsset(), "100")
	if err == nil {
		t.Error("expected error when both EURMTL and XLM lookups fail")
	}
}

func TestGetPathPriceFallbackToStrictReceive(t *testing.T) {
	mock := &mockHorizon{
		strictSendErr: errors.New("strictSend failed"),
		strictReceivePaths: []horizon.HorizonPathRecord{
			{SourceAmount: "1", DestinationAmount: "0.75"},
		},
		orderbookErr: errors.New("no orderbook"),
		poolsErr:     errors.New("no pools"),
	}

	svc := NewService(mock)
	result, err := svc.GetPrice(context.Background(), testAsset(), domain.EURMTLAsset(), "100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Price != "0.75" {
		t.Errorf("Price = %q, want 0.75", result.Price)
	}
}

func TestGetBidPriceSuccess(t *testing.T) {
	mock := &mockHorizon{
		orderbook: horizon.HorizonOrderbook{
			Bids: []horizon.HorizonOrderbookEntry{{Price: "1.5", Amount: "100"}},
		},
	}

	svc := NewService(mock)
	bid, err := svc.GetBidPrice(context.Background(), testAsset(), domain.EURMTLAsset())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bid.String() != "1.5" {
		t.Errorf("bid = %s, want 1.5", bid.String())
	}
}

func TestGetBidPriceNoBids(t *testing.T) {
	mock := &mockHorizon{
		orderbook: horizon.HorizonOrderbook{},
	}

	svc := NewService(mock)
	_, err := svc.GetBidPrice(context.Background(), testAsset(), domain.EURMTLAsset())
	if !errors.Is(err, ErrNoPrice) {
		t.Errorf("err = %v, want ErrNoPrice", err)
	}
}

func TestGetBidPriceFetchError(t *testing.T) {
	mock := &mockHorizon{
		orderbookErr: errors.New("network error"),
	}

	svc := NewService(mock)
	_, err := svc.GetBidPrice(context.Background(), testAsset(), domain.EURMTLAsset())
	if err == nil {
		t.Error("expected error, got nil")
	}
}

type assetAwareMockHorizon struct {
	successDest  string
	paths        []horizon.HorizonPathRecord
	orderbookErr error
	poolsErr     error
}

func (m *assetAwareMockHorizon) FetchStrictSendPaths(_ context.Context, _ domain.AssetInfo, _ string, dest domain.AssetInfo) ([]horizon.HorizonPathRecord, error) {
	if dest.Code == m.successDest {
		return m.paths, nil
	}
	return nil, errors.New("no path to " + dest.Code)
}

func (m *assetAwareMockHorizon) FetchStrictReceivePaths(_ context.Context, _ domain.AssetInfo, _ domain.AssetInfo, _ string) ([]horizon.HorizonPathRecord, error) {
	return nil, errors.New("no receive paths")
}

func (m *assetAwareMockHorizon) FetchOrderbook(_ context.Context, _, _ domain.AssetInfo, _ int) (horizon.HorizonOrderbook, error) {
	return horizon.HorizonOrderbook{}, m.orderbookErr
}

func (m *assetAwareMockHorizon) FetchLiquidityPools(_ context.Context, _, _ domain.AssetInfo) ([]horizon.HorizonLiquidityPool, error) {
	return nil, m.poolsErr
}

func TestGetTokenPricesCrossRateFromXLM(t *testing.T) {
	// XLM path succeeds; EURMTL path fails.
	// Cross-rate EURMTL→XLM also succeeds via same mock.
	// Derived PriceEURMTL = PriceXLM / crossRate.
	mock := &assetAwareMockHorizon{
		successDest:  "XLM",
		paths:        []horizon.HorizonPathRecord{{SourceAmount: "1", DestinationAmount: "4.0"}},
		orderbookErr: errors.New("no orderbook"),
		poolsErr:     errors.New("no pools"),
	}
	svc := NewService(mock)
	result, err := svc.GetTokenPrices(context.Background(), testAsset(), "100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PriceXLM == "" {
		t.Error("expected non-empty XLM price")
	}
	if result.PriceEURMTL == "" {
		t.Error("expected non-empty EURMTL price derived via cross-rate")
	}
}

func testAsset() domain.AssetInfo {
	return domain.AssetInfo{
		Code:   "MTL",
		Issuer: "GISSUER",
		Type:   domain.AssetTypeCreditAlphanum4,
	}
}
