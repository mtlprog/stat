package metrics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/horizon"
	"github.com/mtlprog/stat/internal/indicator"
)

// --- mocks ---

type stubHorizon struct {
	stats           map[string]horizon.AssetStats
	statsErr        map[string]error
	holderCounts    map[string]int
	holderCountErr  map[string]error
	holderBalances  map[string]map[string]decimal.Decimal
	holderErr       map[string]error
	dividends       map[string]decimal.Decimal // by account address
	dividendsErr    map[string]error
	dailyVolume     decimal.Decimal
	dailyVolumeErr  error
	dividendsCalled int
}

func (s *stubHorizon) FetchAssetHolderCountByBalance(_ context.Context, asset domain.AssetInfo, _ decimal.Decimal) (int, error) {
	if err, ok := s.holderCountErr[asset.Code]; ok {
		return 0, err
	}
	if c, ok := s.holderCounts[asset.Code]; ok {
		return c, nil
	}
	return 0, nil
}

func (s *stubHorizon) FetchAssetStats(_ context.Context, asset domain.AssetInfo) (horizon.AssetStats, error) {
	if err, ok := s.statsErr[asset.Code]; ok {
		return horizon.AssetStats{}, err
	}
	return s.stats[asset.Code], nil
}

func (s *stubHorizon) FetchAssetHolderBalancesByBalance(_ context.Context, asset domain.AssetInfo, _ decimal.Decimal) (map[string]decimal.Decimal, error) {
	if err, ok := s.holderErr[asset.Code]; ok {
		return nil, err
	}
	return s.holderBalances[asset.Code], nil
}

func (s *stubHorizon) FetchMonthlyEURMTLOutflow(_ context.Context, accountID string, _ []string) (decimal.Decimal, error) {
	s.dividendsCalled++
	if err, ok := s.dividendsErr[accountID]; ok {
		return decimal.Zero, err
	}
	if v, ok := s.dividends[accountID]; ok {
		return v, nil
	}
	return decimal.Zero, nil
}

func (s *stubHorizon) FetchEURMTLPaymentVolume(_ context.Context, _ time.Time) (decimal.Decimal, error) {
	return s.dailyVolume, s.dailyVolumeErr
}

type stubPrice struct {
	bidByAsset map[string]decimal.Decimal
	bidErr     map[string]error
}

func (s *stubPrice) GetBidPrice(_ context.Context, asset, _ domain.AssetInfo) (decimal.Decimal, error) {
	if err, ok := s.bidErr[asset.Code]; ok {
		return decimal.Zero, err
	}
	return s.bidByAsset[asset.Code], nil
}

type stubIndicatorRepo struct {
	byTarget map[string]map[int]indicator.Indicator
}

func (s *stubIndicatorRepo) Save(_ context.Context, _ int, _ time.Time, _ []indicator.Indicator) error {
	return nil
}
func (s *stubIndicatorRepo) GetByDate(_ context.Context, _ string, _ time.Time) ([]indicator.Indicator, error) {
	return nil, indicator.ErrNotFound
}
func (s *stubIndicatorRepo) GetLatest(_ context.Context, _ string) ([]indicator.Indicator, time.Time, error) {
	return nil, time.Time{}, indicator.ErrNotFound
}
func (s *stubIndicatorRepo) GetHistory(_ context.Context, _ string, _ []int, _ time.Time) ([]indicator.HistoryPoint, error) {
	return nil, nil
}
func (s *stubIndicatorRepo) GetNearestBefore(_ context.Context, _ string, date time.Time) (map[int]indicator.Indicator, error) {
	if s.byTarget == nil {
		return nil, nil
	}
	if v, ok := s.byTarget[date.Format("2006-01-02")]; ok {
		return v, nil
	}
	// Fall through: simulate "newest at-or-before target".
	return s.byTarget["latest"], nil
}

// --- helpers ---

func indicatorMap(values map[int]string) map[int]indicator.Indicator {
	out := make(map[int]indicator.Indicator, len(values))
	for id, v := range values {
		out[id] = indicator.Indicator{ID: id, Value: decimal.RequireFromString(v)}
	}
	return out
}

// --- tests ---

func TestEnrichMetricsHappyPath(t *testing.T) {
	date := time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)
	h := &stubHorizon{
		stats: map[string]horizon.AssetStats{
			"MTL":     {TotalSupply: decimal.NewFromInt(1000), LiquidityPools: decimal.NewFromInt(150)},
			"MTLRECT": {TotalSupply: decimal.NewFromInt(500), LiquidityPools: decimal.NewFromInt(50)},
		},
		holderCounts: map[string]int{
			"EURMTL": 200,
			"MTLAP":  42,
		},
		holderBalances: map[string]map[string]decimal.Decimal{
			"MTL": {
				"A": decimal.NewFromInt(100),
				"B": decimal.NewFromInt(200),
				"C": decimal.NewFromInt(300),
			},
			"MTLRECT": {
				"B": decimal.NewFromInt(50),
				"D": decimal.NewFromInt(150),
			},
		},
		dividends: map[string]decimal.Decimal{
			"GFUND1": decimal.RequireFromString("100"),
			"GFUND2": decimal.RequireFromString("23.45"),
		},
		dailyVolume: decimal.RequireFromString("500.00"),
	}
	p := &stubPrice{
		bidByAsset: map[string]decimal.Decimal{
			"MTL":     decimal.RequireFromString("8.5"),
			"MTLRECT": decimal.RequireFromString("0.4"),
		},
	}
	repo := &stubIndicatorRepo{
		byTarget: map[string]map[int]indicator.Indicator{
			// Yesterday (used for I26_yesterday): I26 = 12000, I25 = 400.
			date.AddDate(0, 0, -1).Format("2006-01-02"): indicatorMap(map[int]string{
				26: "12000",
				25: "400",
			}),
			// 30 days ago (I25_(today-30)): I25 = 350.
			date.AddDate(0, 0, -30).Format("2006-01-02"): indicatorMap(map[int]string{
				25: "350",
			}),
		},
	}

	svc := NewService(h, p, repo, []string{"GFUND1", "GFUND2"})
	data := &domain.FundStructureData{}

	if err := svc.EnrichMetrics(context.Background(), date, data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data.LiveMetrics == nil {
		t.Fatal("LiveMetrics not populated")
	}
	m := data.LiveMetrics

	checks := []struct {
		field string
		got   *string
		want  string
	}{
		{"I6 MTL circulation", m.MTLCirculation, "850"},        // 1000 - 150
		{"I7 MTLRECT circulation", m.MTLRECTCirculation, "450"}, // 500 - 50
		{"I24 EURMTL participants", m.EURMTLParticipants, "200"},
		{"I40 MTLAP holders", m.MTLAPHolders, "42"},
		{"I27 shareholders", m.MTLShareholders, "4"},                  // A,B,C,D unique
		{"I23 median", m.MTLShareholdersMedian, "200"},                // sorted [100,150,250,300]
		{"I11 dividends", m.MonthlyDividends, "123.45"},
		{"I25 daily volume", m.EURMTLDailyVolume, "500"},
		{"I26 incremental", m.EURMTL30dVolume, "12150"}, // 12000 + 500 - 350
		{"I10 MTL bid", m.MTLMarketPrice, "8.5"},
		{"I49 MTLRECT bid", m.MTLRECTMarketPrice, "0.4"},
	}
	for _, c := range checks {
		if c.got == nil {
			t.Errorf("%s = nil, want %s", c.field, c.want)
			continue
		}
		if !decimal.RequireFromString(*c.got).Equal(decimal.RequireFromString(c.want)) {
			t.Errorf("%s = %s, want %s", c.field, *c.got, c.want)
		}
	}
}

func TestEnrichMetricsStickyFallback(t *testing.T) {
	date := time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)
	flake := errors.New("503 service unavailable")
	h := &stubHorizon{
		statsErr:       map[string]error{"MTL": flake, "MTLRECT": flake},
		holderCountErr: map[string]error{"EURMTL": flake, "MTLAP": flake},
		holderErr:      map[string]error{"MTL": flake, "MTLRECT": flake},
		dividendsErr:   map[string]error{"GFUND1": flake, "GFUND2": flake},
		dailyVolumeErr: flake,
	}
	p := &stubPrice{bidErr: map[string]error{"MTL": flake, "MTLRECT": flake}}
	// Prior values cover every live indicator the service writes.
	repo := &stubIndicatorRepo{
		byTarget: map[string]map[int]indicator.Indicator{
			"latest": indicatorMap(map[int]string{
				6: "777", 7: "333", 10: "9.1", 11: "100", 23: "55", 24: "180",
				25: "410", 26: "11500", 27: "5", 40: "33", 49: "0.7",
			}),
		},
	}

	svc := NewService(h, p, repo, nil)
	data := &domain.FundStructureData{}

	if err := svc.EnrichMetrics(context.Background(), date, data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := data.LiveMetrics

	// Every field should have fallen back to the prior day's value.
	checks := map[string]struct {
		got  *string
		want string
	}{
		"I6":  {m.MTLCirculation, "777"},
		"I7":  {m.MTLRECTCirculation, "333"},
		"I10": {m.MTLMarketPrice, "9.1"},
		"I11": {m.MonthlyDividends, "100"},
		"I23": {m.MTLShareholdersMedian, "55"},
		"I24": {m.EURMTLParticipants, "180"},
		"I25": {m.EURMTLDailyVolume, "410"},
		"I26": {m.EURMTL30dVolume, "11500"},
		"I27": {m.MTLShareholders, "5"},
		"I40": {m.MTLAPHolders, "33"},
		"I49": {m.MTLRECTMarketPrice, "0.7"},
	}
	for id, c := range checks {
		if c.got == nil {
			t.Errorf("%s = nil, want %s (sticky-fallback)", id, c.want)
			continue
		}
		if *c.got != c.want {
			t.Errorf("%s = %s, want %s (sticky-fallback)", id, *c.got, c.want)
		}
	}
}

func TestEnrichMetricsNoRepoLeavesNil(t *testing.T) {
	flake := errors.New("503")
	h := &stubHorizon{statsErr: map[string]error{"MTL": flake}}
	p := &stubPrice{bidErr: map[string]error{"MTL": flake}}

	svc := NewService(h, p, nil, nil) // repo is nil → no fallback source
	data := &domain.FundStructureData{}

	if err := svc.EnrichMetrics(context.Background(), time.Now(), data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data.LiveMetrics.MTLCirculation != nil {
		t.Errorf("MTLCirculation = %v, want nil when repo is missing", *data.LiveMetrics.MTLCirculation)
	}
}

func TestComputeI26FallsBackWhenDailyMissing(t *testing.T) {
	date := time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)
	repo := &stubIndicatorRepo{
		byTarget: map[string]map[int]indicator.Indicator{
			"latest": indicatorMap(map[int]string{26: "9000"}),
		},
	}
	svc := NewService(&stubHorizon{}, &stubPrice{}, repo, nil)
	prev := repo.byTarget["latest"]

	got := svc.computeI26(context.Background(), date, decimal.Zero, false /* dailyOK */, prev)
	if got == nil || *got != "9000" {
		t.Errorf("computeI26 = %v, want 9000 (fallback to prior I26)", got)
	}
}

func TestComputeI26FallsBackWhen30dMissing(t *testing.T) {
	date := time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)
	// Repo has yesterday's I26 but no I25 from 30 days ago.
	repo := &stubIndicatorRepo{
		byTarget: map[string]map[int]indicator.Indicator{
			"latest": indicatorMap(map[int]string{26: "9000"}),
		},
	}
	svc := NewService(&stubHorizon{}, &stubPrice{}, repo, nil)
	prev := repo.byTarget["latest"]

	got := svc.computeI26(context.Background(), date, decimal.NewFromInt(500), true, prev)
	if got == nil || *got != "9000" {
		t.Errorf("computeI26 = %v, want 9000 (fallback when 30d-old I25 absent)", got)
	}
}

func TestComputeI26ClampsNegativeToZero(t *testing.T) {
	date := time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)
	// 30d-ago I25 enormous → arithmetic goes negative → clamp to 0.
	repo := &stubIndicatorRepo{
		byTarget: map[string]map[int]indicator.Indicator{
			date.AddDate(0, 0, -30).Format("2006-01-02"): indicatorMap(map[int]string{25: "100000"}),
		},
	}
	svc := NewService(&stubHorizon{}, &stubPrice{}, repo, nil)
	prev := indicatorMap(map[int]string{26: "1000"})

	got := svc.computeI26(context.Background(), date, decimal.NewFromInt(50), true, prev)
	if got == nil || *got != "0" {
		t.Errorf("computeI26 = %v, want 0 (negative clamped)", got)
	}
}

func TestMedianOddCount(t *testing.T) {
	got := median([]decimal.Decimal{
		decimal.NewFromInt(3), decimal.NewFromInt(1), decimal.NewFromInt(2),
	})
	if !got.Equal(decimal.NewFromInt(2)) {
		t.Errorf("median = %s, want 2", got)
	}
}

func TestMedianEvenCount(t *testing.T) {
	got := median([]decimal.Decimal{
		decimal.NewFromInt(10), decimal.NewFromInt(20), decimal.NewFromInt(30), decimal.NewFromInt(40),
	})
	if !got.Equal(decimal.NewFromInt(25)) {
		t.Errorf("median = %s, want 25", got)
	}
}

func TestMedianEmpty(t *testing.T) {
	got := median(nil)
	if !got.IsZero() {
		t.Errorf("median(nil) = %s, want 0", got)
	}
}

// I11 sticky-on-zero: when every account legitimately returns zero (no recent
// dividends) but yesterday had a non-zero figure, we keep yesterday's value.
// Distinct from the Horizon-error case covered by TestEnrichMetricsStickyFallback.
func TestComputeI11StickyWhenLiveSumIsZero(t *testing.T) {
	h := &stubHorizon{
		// All accounts return zero with no error.
		dividends: map[string]decimal.Decimal{},
	}
	repo := &stubIndicatorRepo{
		byTarget: map[string]map[int]indicator.Indicator{
			"latest": indicatorMap(map[int]string{11: "2440.7"}),
		},
	}
	svc := NewService(h, &stubPrice{}, repo, []string{"GFUND1", "GFUND2", "GFUND3"})

	got := svc.computeI11(context.Background(), repo.byTarget["latest"])
	if got == nil || *got != "2440.7" {
		t.Errorf("computeI11 = %v, want sticky to 2440.7 (live zero, prior non-zero)", got)
	}
	if h.dividendsCalled != 3 {
		t.Errorf("dividendsCalled = %d, want 3 (one walk per fund account)", h.dividendsCalled)
	}
}

// I11 should write a real zero — not nil — when both live and prior are zero
// (or prior is missing entirely). The downstream calculator needs a definite
// value, not a sticky from nothing.
func TestComputeI11WritesZeroWhenPriorIsAlsoZero(t *testing.T) {
	svc := NewService(&stubHorizon{}, &stubPrice{}, &stubIndicatorRepo{}, []string{"GFUND1"})
	got := svc.computeI11(context.Background(), indicatorMap(map[int]string{11: "0"}))
	if got == nil || *got != "0" {
		t.Errorf("computeI11 = %v, want \"0\" (live zero, prior zero)", got)
	}
}

func TestComputeI11WritesZeroWhenNoPrior(t *testing.T) {
	svc := NewService(&stubHorizon{}, &stubPrice{}, nil, []string{"GFUND1"})
	got := svc.computeI11(context.Background(), nil)
	if got == nil || *got != "0" {
		t.Errorf("computeI11 = %v, want \"0\" (no prior available)", got)
	}
}
