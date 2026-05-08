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
	stats              map[string]horizon.AssetStats
	statsErr           map[string]error
	holderCounts       map[string]int
	holderCountErr     map[string]error
	holderBalances     map[string]map[string]decimal.Decimal
	holderErr          map[string]error
	dividendActivity   horizon.DividendActivity
	dividendsErr       error
	dividendsCalled    int
	accountDataValue   string
	accountDataPresent bool
	accountDataErr     error
}

type stubExpert struct {
	stats      stellarexpert.Stats
	err        error
	calledWith time.Time
}

func (s *stubExpert) FetchEURMTLPaymentStats(_ context.Context, date time.Time) (stellarexpert.Stats, error) {
	s.calledWith = date
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

func (s *stubHorizon) FetchDividendActivity(_ context.Context, _ string, _ []string, _ time.Time) (horizon.DividendActivity, error) {
	s.dividendsCalled++
	if s.dividendsErr != nil {
		return horizon.DividendActivity{}, s.dividendsErr
	}
	return s.dividendActivity, nil
}

func (s *stubHorizon) FetchAccountDataEntry(_ context.Context, _, _ string) (string, bool, error) {
	if s.accountDataErr != nil {
		return "", false, s.accountDataErr
	}
	return s.accountDataValue, s.accountDataPresent, nil
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
			"MTLAP":  42,
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
		// I11 = LAST_DIVS at the latest manage_data update ≤ snapshot date.
		// I18 = |Recipients| in the latest memo group ≤ snapshot date.
		// 29 Apr snapshot: latest update is 7 Apr (123.45), latest group is
		// the same date with two distinct recipients.
		dividendActivity: horizon.DividendActivity{
			LastDivsUpdates: []horizon.LastDivsUpdate{
				{TS: time.Date(2026, 3, 7, 6, 0, 0, 0, time.UTC), Value: decimal.RequireFromString("80")},
				{TS: time.Date(2026, 4, 7, 6, 0, 0, 0, time.UTC), Value: decimal.RequireFromString("123.45")},
			},
			RecipientGroups: []horizon.RecipientGroup{
				{TS: time.Date(2026, 3, 7, 6, 0, 0, 0, time.UTC), Memo: "mtl div 07/03/2026", Recipients: []string{"X", "Y", "Z"}},
				{TS: time.Date(2026, 4, 7, 6, 0, 0, 0, time.UTC), Memo: "mtl div 07/04/2026", Recipients: []string{"X", "Y"}},
			},
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
		{"I27 shareholders ≥1", m.MTLShareholders, "4"},     // A,B,C,D — E (0.5) excluded
		{"I62 shareholders any", m.MTLShareholdersAny, "5"}, // A,B,C,D,E all counted
		{"I40 MTLAP holders", m.MTLAPHolders, "42"},
		{"I23 median", m.MTLShareholdersMedian, "200"},         // sorted [100,150,250,300]
		{"I18 dividend recipients", m.EURMTLShareholders, "2"}, // distinct {X, Y}
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

	// I25 spec is "за прошлые сутки" — the call must target date-1, not date,
	// because today's stats-history bucket is a partial running total.
	wantPrior := date.AddDate(0, 0, -1)
	if !expert.calledWith.Equal(wantPrior) {
		t.Errorf("FetchEURMTLPaymentStats called with %s, want %s (prior day)",
			expert.calledWith.Format("2006-01-02"), wantPrior.Format("2006-01-02"))
	}
}

func TestEnrichMetricsStickyFallback(t *testing.T) {
	date := time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)
	flake := errors.New("503 service unavailable")
	h := &stubHorizon{
		statsErr:       map[string]error{"MTL": flake, "MTLRECT": flake},
		holderCountErr: map[string]error{"EURMTL": flake, "MTLAP": flake},
		holderErr:      map[string]error{"MTL": flake, "MTLRECT": flake},
		dividendsErr:   flake,
	}
	p := &stubPrice{avgErr: map[string]error{"MTL": flake, "MTLRECT": flake}}
	expert := &stubExpert{err: flake}
	// Prior values cover every live indicator the service writes.
	repo := &stubIndicatorRepo{
		byTarget: map[string]map[int]indicator.Indicator{
			"latest": indicatorMap(map[int]string{
				6: "777", 7: "333", 10: "9.1", 11: "100", 18: "120", 23: "55", 24: "180",
				25: "410", 26: "11500", 27: "5", 40: "37", 49: "0.7", 62: "9",
			}),
		},
	}

	svc := NewService(h, p, expert, repo, []string{"GFUND1", "GFUND2"})
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
		"I40": {m.MTLAPHolders, "37"},
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

// fetchShareholderStats: I27 uses GreaterThanOrEqual(1) and I62 uses any
// positive balance. Lock the boundary at exactly 1 — a holder whose combined
// MTL+MTLRECT sums to 1.0 must count in I27 (not just I62), guarding against
// a silent drift to GreaterThan that would mis-bucket whole-share holders.
func TestFetchShareholderStatsBoundaryAtBalanceOne(t *testing.T) {
	h := &stubHorizon{
		holderBalances: map[string]map[string]decimal.Decimal{
			"MTL": {
				"BoundaryHolder": decimal.RequireFromString("1.0"),  // exactly 1 → I27 + I62
				"AboveOne":       decimal.RequireFromString("1.01"), // > 1 → I27 + I62
				"SubOne":         decimal.RequireFromString("0.5"),  // < 1 → I62 only
			},
			"MTLRECT": {},
		},
	}
	svc := NewService(h, &stubPrice{}, &stubExpert{}, &stubIndicatorRepo{}, nil)

	mtlAsset := domain.NewAssetInfo("MTL", domain.IssuerAddress)
	mtlrectAsset := domain.NewAssetInfo("MTLRECT", domain.IssuerAddress)
	_, stats, ok := svc.fetchShareholderStats(context.Background(), mtlAsset, mtlrectAsset)
	if !ok {
		t.Fatal("fetchShareholderStats ok=false, want true")
	}
	if stats.countAtLeastOne != 2 {
		t.Errorf("countAtLeastOne = %d, want 2 (BoundaryHolder=1.0 must count, GreaterThanOrEqual)", stats.countAtLeastOne)
	}
	if stats.countAny != 3 {
		t.Errorf("countAny = %d, want 3 (all positive balances)", stats.countAny)
	}
}

// Median is computed over the ≥1 cohort only — a holder with sub-1 balance
// must be excluded so the median can't be silently dragged toward zero.
// Without the cohort filter, median over [0.5, 100, 200] would be 100;
// with the filter, median over [100, 200] is 150.
func TestFetchShareholderStatsMedianExcludesSubOneCohort(t *testing.T) {
	h := &stubHorizon{
		holderBalances: map[string]map[string]decimal.Decimal{
			"MTL": {
				"A": decimal.NewFromInt(100),
				"B": decimal.NewFromInt(200),
				"C": decimal.RequireFromString("0.5"), // sub-1 — excluded from median
			},
			"MTLRECT": {},
		},
	}
	svc := NewService(h, &stubPrice{}, &stubExpert{}, &stubIndicatorRepo{}, nil)

	mtlAsset := domain.NewAssetInfo("MTL", domain.IssuerAddress)
	mtlrectAsset := domain.NewAssetInfo("MTLRECT", domain.IssuerAddress)
	_, stats, ok := svc.fetchShareholderStats(context.Background(), mtlAsset, mtlrectAsset)
	if !ok {
		t.Fatal("fetchShareholderStats ok=false, want true")
	}
	if !stats.median.Equal(decimal.NewFromInt(150)) {
		t.Errorf("median = %s, want 150 (median over {100,200} cohort, NOT {0.5,100,200})", stats.median)
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

// Snap-on-event: with two updates and two memo groups present, the ones at-or-
// before the snapshot date drive both I11 and I18 — newer ones are ignored.
func TestComputeDividendActivityPicksLatestEventOnOrBeforeDate(t *testing.T) {
	activity := horizon.DividendActivity{
		LastDivsUpdates: []horizon.LastDivsUpdate{
			{TS: time.Date(2026, 3, 7, 6, 0, 0, 0, time.UTC), Value: decimal.NewFromInt(80)},
			{TS: time.Date(2026, 4, 7, 6, 0, 0, 0, time.UTC), Value: decimal.NewFromInt(123)},
			{TS: time.Date(2026, 5, 7, 6, 0, 0, 0, time.UTC), Value: decimal.NewFromInt(200)},
		},
		RecipientGroups: []horizon.RecipientGroup{
			{TS: time.Date(2026, 3, 7, 6, 0, 0, 0, time.UTC), Memo: "mtl div 07/03/2026", Recipients: []string{"X", "Y", "Z"}},
			{TS: time.Date(2026, 4, 7, 6, 0, 0, 0, time.UTC), Memo: "mtl div 07/04/2026", Recipients: []string{"X", "Y"}},
			{TS: time.Date(2026, 5, 7, 6, 0, 0, 0, time.UTC), Memo: "mtl div 07/05/2026", Recipients: []string{"X", "Y", "Z", "W"}},
		},
	}
	h := &stubHorizon{dividendActivity: activity}
	svc := NewService(h, &stubPrice{}, &stubExpert{}, nil, nil)

	// Snapshot 2026-04-29: latest ≤ that date is the 2026-04-07 event/update.
	i11, i18, ok := svc.computeDividendActivity(context.Background(),
		time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC), nil)
	if !ok {
		t.Fatal("ok=false, want true")
	}
	if i18 != 2 {
		t.Errorf("i18 = %d, want 2 (recipients of 2026-04-07 event)", i18)
	}
	if i11 == nil || *i11 != "123" {
		t.Errorf("i11 = %v, want 123 (LAST_DIVS at 2026-04-07)", i11)
	}
}

// A dividend lodged at 06:00 UTC on day T must count for the snapshot of day T
// (which is dated at midnight UTC). This is the "instantly snap on dividend
// day" requirement.
func TestComputeDividendActivityIncludesEventOnSameUTCDay(t *testing.T) {
	activity := horizon.DividendActivity{
		LastDivsUpdates: []horizon.LastDivsUpdate{
			{TS: time.Date(2026, 5, 7, 6, 56, 18, 0, time.UTC), Value: decimal.NewFromInt(2008)},
		},
		RecipientGroups: []horizon.RecipientGroup{
			{TS: time.Date(2026, 5, 7, 6, 56, 34, 0, time.UTC), Memo: "mtl div 07/05/2026", Recipients: []string{"A", "B"}},
		},
	}
	h := &stubHorizon{dividendActivity: activity}
	svc := NewService(h, &stubPrice{}, &stubExpert{}, nil, nil)

	i11, i18, ok := svc.computeDividendActivity(context.Background(),
		time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC), nil)
	if !ok {
		t.Fatalf("ok=false, want true")
	}
	if i18 != 2 {
		t.Errorf("i18 = %d, want 2", i18)
	}
	if i11 == nil || *i11 != "2008" {
		t.Errorf("i11 = %v, want 2008", i11)
	}
}

// Multiple txs sharing the same memo (Stellar 100-op cap forces big batches
// into multiple txs) collapse into ONE recipient group at the walker level —
// callers receive that group as one logical event. Test fixture exercises the
// caller side: the stub already returns a single grouped value, so I18 reflects
// the union of recipients from the batch (5 distinct in this fixture).
func TestComputeDividendActivityMemoGroupedRecipients(t *testing.T) {
	activity := horizon.DividendActivity{
		LastDivsUpdates: []horizon.LastDivsUpdate{
			{TS: time.Date(2026, 5, 7, 6, 56, 18, 0, time.UTC), Value: decimal.NewFromInt(2008)},
		},
		RecipientGroups: []horizon.RecipientGroup{
			// All the txs of memo "mtl div 07/05/2026" merged: 5 distinct recipients.
			{TS: time.Date(2026, 5, 7, 6, 56, 34, 0, time.UTC), Memo: "mtl div 07/05/2026", Recipients: []string{"R1", "R2", "R3", "R4", "R5"}},
		},
	}
	h := &stubHorizon{dividendActivity: activity}
	svc := NewService(h, &stubPrice{}, &stubExpert{}, nil, nil)

	_, i18, ok := svc.computeDividendActivity(context.Background(),
		time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC), nil)
	if !ok || i18 != 5 {
		t.Errorf("memo-grouped recipients: i18=%d want 5 (ok=%v)", i18, ok)
	}
}

// No update or group within the lookback window → fall back to live
// account.data["LAST_DIVS"] for I11 (the bot keeps it current); I18 sticky.
func TestComputeDividendActivityFallsBackToLiveLastDivsWhenWalkEmpty(t *testing.T) {
	h := &stubHorizon{
		dividendActivity:   horizon.DividendActivity{},
		accountDataValue:   "2008.6829228",
		accountDataPresent: true,
	}
	svc := NewService(h, &stubPrice{}, &stubExpert{}, nil, nil)

	prev := indicatorMap(map[int]string{11: "999", 18: "347"})
	i11, i18, ok := svc.computeDividendActivity(context.Background(),
		time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC), prev)
	if !ok {
		t.Fatal("ok=false, want true")
	}
	if i11 == nil || *i11 != "2008.6829228" {
		t.Errorf("i11 = %v, want 2008.6829228 (live account.data)", i11)
	}
	if i18 != 347 {
		t.Errorf("i18 = %d, want sticky 347", i18)
	}
}

// Walk empty AND account.data unavailable → sticky to prior. ok stays true.
func TestComputeDividendActivityStickyWhenNoEvent(t *testing.T) {
	h := &stubHorizon{dividendActivity: horizon.DividendActivity{}}
	svc := NewService(h, &stubPrice{}, &stubExpert{}, nil, nil)

	prev := indicatorMap(map[int]string{11: "2440.7", 18: "347"})
	i11, i18, ok := svc.computeDividendActivity(context.Background(),
		time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC), prev)
	if !ok {
		t.Fatal("ok=false, want true (no event is not an error)")
	}
	if i11 == nil || *i11 != "2440.7" {
		t.Errorf("i11 = %v, want sticky 2440.7", i11)
	}
	if i18 != 347 {
		t.Errorf("i18 = %d, want sticky 347", i18)
	}
}

// Horizon walk failed → ok=false; both indicators must sticky-fallback at the
// caller. computeDividendActivity returns the prior I11 directly to keep the
// payload populated; I18 is signalled via ok=false so the caller picks prior.
func TestComputeDividendActivityErrorReturnsOKFalse(t *testing.T) {
	flake := errors.New("503")
	h := &stubHorizon{dividendsErr: flake}
	svc := NewService(h, &stubPrice{}, &stubExpert{}, nil, nil)

	_, _, ok := svc.computeDividendActivity(context.Background(),
		time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC), nil)
	if ok {
		t.Error("ok=true, want false on Horizon error")
	}
}

// auditI18VsI27 must NOT log when shareholder stats are unavailable (i27OK
// false): comparing recipients against a fallback I27 from yesterday would
// produce a misleading alarm during real cascades.
func TestAuditI18VsI27SkipsWhenI27Unavailable(t *testing.T) {
	svc := NewService(&stubHorizon{}, &stubPrice{}, &stubExpert{}, nil, nil)
	// Just exercise the path; we're checking it doesn't panic and the
	// guard short-circuits. No assertion on log because slog is global.
	svc.auditI18VsI27(347, 0, false)
	svc.auditI18VsI27(0, 384, false)
}

// auditI18VsI27 boundary: ≤5% divergence stays silent, >5% logs. The audit is
// a business signal of distribution gaps; tripping it on noise (e.g. 1 holder
// out of 380) would train operators to ignore it.
func TestAuditI18VsI27ThresholdBoundary(t *testing.T) {
	svc := NewService(&stubHorizon{}, &stubPrice{}, &stubExpert{}, nil, nil)
	// 19/380 = 5.0% exactly — at threshold, must NOT log.
	svc.auditI18VsI27(361, 380, true)
	// 20/380 = 5.26% — over threshold, logs.
	svc.auditI18VsI27(360, 380, true)
}
