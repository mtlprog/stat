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
	"github.com/mtlprog/stat/internal/stellarexpert"
)

// --- mocks ---

type stubHorizon struct {
	stats           map[string]horizon.AssetStats
	statsErr        map[string]error
	holderCounts    map[string]int
	holderCountErr  map[string]error
	holderBalances  map[string]map[string]decimal.Decimal
	holderErr       map[string]error
	holderIDs       map[string][]string
	holderIDsErr    map[string]error
	dividends       map[string]decimal.Decimal // by account address
	dividendsErr    map[string]error
	dividendsCalled int
}

type stubExpert struct {
	stats stellarexpert.Stats
	err   error
}

func (s *stubExpert) FetchEURMTLPaymentStats(_ context.Context, _ time.Time) (stellarexpert.Stats, error) {
	return s.stats, s.err
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

func (s *stubHorizon) FetchAssetHolderIDsByBalance(_ context.Context, asset domain.AssetInfo, _ decimal.Decimal) ([]string, error) {
	if err, ok := s.holderIDsErr[asset.Code]; ok {
		return nil, err
	}
	return s.holderIDs[asset.Code], nil
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

type stubPrice struct {
	avgByAsset map[string]decimal.Decimal
	avgErr     map[string]error
}

func (s *stubPrice) GetAverageTradePrice(_ context.Context, base, _ domain.AssetInfo, _ int) (decimal.Decimal, error) {
	if err, ok := s.avgErr[base.Code]; ok {
		return decimal.Zero, err
	}
	return s.avgByAsset[base.Code], nil
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
		},
		holderBalances: map[string]map[string]decimal.Decimal{
			// Includes a sub-1 balance (E) so we can also exercise the I62/I27
			// threshold split: I62 counts {A,B,C,D,E}, I27 counts only {A,B,C,D}.
			"MTL": {
				"A": decimal.NewFromInt(100),
				"B": decimal.NewFromInt(200),
				"C": decimal.NewFromInt(300),
				"E": decimal.RequireFromString("0.5"),
			},
			"MTLRECT": {
				"B": decimal.NewFromInt(50),
				"D": decimal.NewFromInt(150),
			},
		},
		holderIDs: map[string][]string{
			// EURMTL trustline holders. A and C are also shareholders → I18 = 2.
			// X and Y are outsiders. B and D are shareholders without EURMTL.
			"EURMTL": {"A", "C", "X", "Y"},
		},
		dividends: map[string]decimal.Decimal{
			"GFUND1": decimal.RequireFromString("100"),
			"GFUND2": decimal.RequireFromString("23.45"),
		},
	}
	p := &stubPrice{
		avgByAsset: map[string]decimal.Decimal{
			"MTL":     decimal.RequireFromString("8.5"),
			"MTLRECT": decimal.RequireFromString("0.4"),
		},
	}
	expert := &stubExpert{
		stats: stellarexpert.Stats{
			Daily:      decimal.RequireFromString("500"),
			Cumulative: decimal.RequireFromString("12500"),
		},
	}
	repo := &stubIndicatorRepo{}

	svc := NewService(h, p, expert, repo, []string{"GFUND1", "GFUND2"})
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
		{"I6 MTL circulation", m.MTLCirculation, "850"},         // 1000 - 150
		{"I7 MTLRECT circulation", m.MTLRECTCirculation, "450"}, // 500 - 50
		{"I24 EURMTL participants", m.EURMTLParticipants, "200"},
		{"I27 shareholders ≥1", m.MTLShareholders, "4"},        // A,B,C,D — E (0.5) excluded
		{"I62 shareholders any", m.MTLShareholdersAny, "5"},    // A,B,C,D,E all counted
		{"I23 median", m.MTLShareholdersMedian, "200"},         // sorted [100,150,250,300]
		{"I18 EURMTL shareholders", m.EURMTLShareholders, "2"}, // {A,B,C,D} ∩ {A,C,X,Y} = {A,C}
		{"I11 dividends", m.MonthlyDividends, "123.45"},
		{"I25 daily volume", m.EURMTLDailyVolume, "500"},
		{"I26 cumulative", m.EURMTLPaymentTotal, "12500"}, // 12000 + 500
		{"I10 MTL trades-avg", m.MTLMarketPrice, "8.5"},
		{"I49 MTLRECT trades-avg", m.MTLRECTMarketPrice, "0.4"},
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
		holderCountErr: map[string]error{"EURMTL": flake},
		holderErr:      map[string]error{"MTL": flake, "MTLRECT": flake},
		holderIDsErr:   map[string]error{"EURMTL": flake},
		dividendsErr:   map[string]error{"GFUND1": flake, "GFUND2": flake},
	}
	p := &stubPrice{avgErr: map[string]error{"MTL": flake, "MTLRECT": flake}}
	expert := &stubExpert{err: flake}
	// Prior values cover every live indicator the service writes.
	repo := &stubIndicatorRepo{
		byTarget: map[string]map[int]indicator.Indicator{
			"latest": indicatorMap(map[int]string{
				6: "777", 7: "333", 10: "9.1", 11: "100", 18: "120", 23: "55", 24: "180",
				25: "410", 26: "11500", 27: "5", 49: "0.7", 62: "9",
			}),
		},
	}

	svc := NewService(h, p, expert, repo, nil)
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
		"I18": {m.EURMTLShareholders, "120"},
		"I23": {m.MTLShareholdersMedian, "55"},
		"I24": {m.EURMTLParticipants, "180"},
		"I25": {m.EURMTLDailyVolume, "410"},
		"I26": {m.EURMTLPaymentTotal, "11500"},
		"I27": {m.MTLShareholders, "5"},
		"I49": {m.MTLRECTMarketPrice, "0.7"},
		"I62": {m.MTLShareholdersAny, "9"},
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
	p := &stubPrice{avgErr: map[string]error{"MTL": flake}}
	expert := &stubExpert{err: flake}

	svc := NewService(h, p, expert, nil, nil) // repo is nil → no fallback source
	data := &domain.FundStructureData{}

	if err := svc.EnrichMetrics(context.Background(), time.Now(), data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data.LiveMetrics.MTLCirculation != nil {
		t.Errorf("MTLCirculation = %v, want nil when repo is missing", *data.LiveMetrics.MTLCirculation)
	}
}

// stellar.expert hasn't ingested the requested date yet → ErrNoDailyEntry
// must collapse to sticky-fallback, NOT propagate as an error.
func TestEnrichMetricsExpertNoDailyEntryUsesPrior(t *testing.T) {
	date := time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)
	h := &stubHorizon{}
	p := &stubPrice{}
	expert := &stubExpert{err: stellarexpert.ErrNoDailyEntry}
	repo := &stubIndicatorRepo{
		byTarget: map[string]map[int]indicator.Indicator{
			"latest": indicatorMap(map[int]string{25: "410", 26: "11500"}),
		},
	}

	svc := NewService(h, p, expert, repo, nil)
	data := &domain.FundStructureData{}
	if err := svc.EnrichMetrics(context.Background(), date, data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data.LiveMetrics.EURMTLDailyVolume == nil || *data.LiveMetrics.EURMTLDailyVolume != "410" {
		t.Errorf("I25 = %v, want 410 (sticky)", data.LiveMetrics.EURMTLDailyVolume)
	}
	if data.LiveMetrics.EURMTLPaymentTotal == nil || *data.LiveMetrics.EURMTLPaymentTotal != "11500" {
		t.Errorf("I26 = %v, want 11500 (sticky)", data.LiveMetrics.EURMTLPaymentTotal)
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
	svc := NewService(h, &stubPrice{}, &stubExpert{}, repo, []string{"GFUND1", "GFUND2", "GFUND3"})

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
	svc := NewService(&stubHorizon{}, &stubPrice{}, &stubExpert{}, &stubIndicatorRepo{}, []string{"GFUND1"})
	got := svc.computeI11(context.Background(), indicatorMap(map[int]string{11: "0"}))
	if got == nil || *got != "0" {
		t.Errorf("computeI11 = %v, want \"0\" (live zero, prior zero)", got)
	}
}

// I18 must fall back to prior when the EURMTL holder-IDs walk fails, even if
// the shareholder walk succeeded — otherwise a single Horizon hiccup on
// /accounts?asset=EURMTL would silently zero out the column.
func TestEnrichMetricsI18StickyOnEURMTLHoldersFailure(t *testing.T) {
	date := time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)
	flake := errors.New("503 service unavailable")
	h := &stubHorizon{
		holderBalances: map[string]map[string]decimal.Decimal{
			"MTL":     {"A": decimal.NewFromInt(50)},
			"MTLRECT": {"B": decimal.NewFromInt(60)},
		},
		holderIDsErr: map[string]error{"EURMTL": flake},
	}
	repo := &stubIndicatorRepo{
		byTarget: map[string]map[int]indicator.Indicator{
			"latest": indicatorMap(map[int]string{18: "347"}),
		},
	}
	svc := NewService(h, &stubPrice{}, &stubExpert{}, repo, nil)
	data := &domain.FundStructureData{}

	if err := svc.EnrichMetrics(context.Background(), date, data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data.LiveMetrics.EURMTLShareholders == nil || *data.LiveMetrics.EURMTLShareholders != "347" {
		t.Errorf("EURMTLShareholders = %v, want 347 (sticky-fallback)", data.LiveMetrics.EURMTLShareholders)
	}
	// I27 must still reflect the live shareholder walk that succeeded.
	if data.LiveMetrics.MTLShareholders == nil || *data.LiveMetrics.MTLShareholders != "2" {
		t.Errorf("MTLShareholders = %v, want 2 (live, not sticky)", data.LiveMetrics.MTLShareholders)
	}
}

// Empty merged map → I18 must be 0 with ok=true. The intersection of an
// empty set with anything is empty; nil maps must not panic.
func TestComputeI18EmptyMerged(t *testing.T) {
	h := &stubHorizon{
		holderIDs: map[string][]string{"EURMTL": {"A", "B", "C"}},
	}
	svc := NewService(h, &stubPrice{}, &stubExpert{}, nil, nil)
	got, ok := svc.computeI18(context.Background(), domain.EURMTLAsset(), nil)
	if !ok {
		t.Fatal("computeI18(nil merged) ok=false, want true")
	}
	if got != 0 {
		t.Errorf("computeI18(nil merged) = %d, want 0", got)
	}
}

// computeI18 filters merged shareholders by sum>1 before intersecting with
// EURMTL holders. An account with combined balance exactly 1 must NOT count.
func TestComputeI18FiltersSumGreaterThanOne(t *testing.T) {
	merged := map[string]decimal.Decimal{
		"A": decimal.RequireFromString("1.0"),  // exactly 1 — excluded
		"B": decimal.RequireFromString("1.01"), // > 1 — included
		"C": decimal.NewFromInt(50),            // > 1 — included
		"D": decimal.RequireFromString("0.5"),  // < 1 — excluded
	}
	h := &stubHorizon{
		holderIDs: map[string][]string{
			"EURMTL": {"A", "B", "C", "D", "Z"},
		},
	}
	svc := NewService(h, &stubPrice{}, &stubExpert{}, nil, nil)
	got, ok := svc.computeI18(context.Background(), domain.EURMTLAsset(), merged)
	if !ok {
		t.Fatal("computeI18 ok=false, want true")
	}
	if got != 2 {
		t.Errorf("computeI18 = %d, want 2 (B and C only)", got)
	}
}

func TestComputeI11WritesZeroWhenNoPrior(t *testing.T) {
	svc := NewService(&stubHorizon{}, &stubPrice{}, &stubExpert{}, nil, []string{"GFUND1"})
	got := svc.computeI11(context.Background(), nil)
	if got == nil || *got != "0" {
		t.Errorf("computeI11 = %v, want \"0\" (no prior available)", got)
	}
}
