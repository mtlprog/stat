package indicator

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/snapshot"
)

// Snapshots before the LiveMetrics rollout cannot supply an MTL price — the
// year-ago snapshot has neither LiveMetrics nor an MTL token in any account
// portfolio (the fund doesn't hold its own issued shares). fetchPriceYearAgo
// must fall back to I10 in the indicator repository, which has continuous
// history from the legacy MONITORING import.
func TestFetchPriceYearAgoFallsBackToIndicatorRepo(t *testing.T) {
	snapRepo := &stubSnapshotRepo{
		nearest: makeSnap(t, domain.FundStructureData{}), // no LiveMetrics, no tokens
	}
	indRepo := &stubIndicatorRepoForDividend{
		byID: map[int]Indicator{10: {ID: 10, Value: decimal.RequireFromString("6.28")}},
	}
	hist := &HistoricalData{Repo: snapRepo, IndicatorRepo: indRepo, Slug: "mtlf"}

	got, err := fetchPriceYearAgo(context.Background(), hist)
	if err != nil {
		t.Fatalf("fetchPriceYearAgo: %v", err)
	}
	if !got.Equal(decimal.RequireFromString("6.28")) {
		t.Errorf("fetchPriceYearAgo = %s, want 6.28 (from indicator repo)", got)
	}
}

// When the year-ago snapshot has a non-zero MTLMarketPrice in LiveMetrics, the
// indicator-repo fallback must NOT fire — the snapshot is the authoritative
// source for dates after the LiveMetrics rollout.
func TestFetchPriceYearAgoPrefersSnapshotLiveMetrics(t *testing.T) {
	priceStr := "9.42"
	snapRepo := &stubSnapshotRepo{
		nearest: makeSnap(t, domain.FundStructureData{
			LiveMetrics: &domain.FundLiveMetrics{MTLMarketPrice: &priceStr},
		}),
	}
	// indicator repo has a different value — must not be consulted.
	indRepo := &stubIndicatorRepoForDividend{
		byID: map[int]Indicator{10: {ID: 10, Value: decimal.RequireFromString("999")}},
	}
	hist := &HistoricalData{Repo: snapRepo, IndicatorRepo: indRepo, Slug: "mtlf"}

	got, err := fetchPriceYearAgo(context.Background(), hist)
	if err != nil {
		t.Fatalf("fetchPriceYearAgo: %v", err)
	}
	if !got.Equal(decimal.RequireFromString("9.42")) {
		t.Errorf("fetchPriceYearAgo = %s, want 9.42 (snapshot LiveMetrics)", got)
	}
}

// When no year-ago snapshot exists at all (snapshot table only goes back ~6
// months), fetchPriceYearAgo must still pick up I10 from the indicator repo.
func TestFetchPriceYearAgoFallsBackWhenSnapshotMissing(t *testing.T) {
	snapRepo := &stubSnapshotRepo{notFound: true}
	indRepo := &stubIndicatorRepoForDividend{
		byID: map[int]Indicator{10: {ID: 10, Value: decimal.RequireFromString("5.5")}},
	}
	hist := &HistoricalData{Repo: snapRepo, IndicatorRepo: indRepo, Slug: "mtlf"}

	got, err := fetchPriceYearAgo(context.Background(), hist)
	if err != nil {
		t.Fatalf("fetchPriceYearAgo: %v", err)
	}
	if !got.Equal(decimal.RequireFromString("5.5")) {
		t.Errorf("fetchPriceYearAgo = %s, want 5.5 (indicator repo, snapshot ErrNotFound)", got)
	}
}

// CLAUDE.md: snapshot.ErrNotFound and a real DB error must NOT be conflated.
// A transient pg blip on the snapshot query has to surface as an error from
// fetchPriceYearAgo so the caller can fail loud — silently chaining to
// indicator-repo would mask infrastructure issues as "data unavailable".
func TestFetchPriceYearAgoPropagatesSnapshotDBError(t *testing.T) {
	snapRepo := &stubSnapshotRepo{err: errors.New("conn lost")}
	// Even though indicator-repo could answer, we must NOT fall through.
	indRepo := &stubIndicatorRepoForDividend{
		byID: map[int]Indicator{10: {ID: 10, Value: decimal.RequireFromString("4.2")}},
	}
	hist := &HistoricalData{Repo: snapRepo, IndicatorRepo: indRepo, Slug: "mtlf"}

	_, err := fetchPriceYearAgo(context.Background(), hist)
	if err == nil {
		t.Fatal("fetchPriceYearAgo err=nil, want error on snapshot DB failure (no silent fallback)")
	}
}

// Symmetric case: ErrNotFound on snapshot, real DB error on indicator-repo
// must propagate — chaining sources never "absorbs" a real error.
func TestFetchPriceYearAgoPropagatesIndicatorRepoError(t *testing.T) {
	snapRepo := &stubSnapshotRepo{notFound: true}
	indRepo := &stubIndicatorRepoForDividend{nearestErr: errors.New("conn lost")}
	hist := &HistoricalData{Repo: snapRepo, IndicatorRepo: indRepo, Slug: "mtlf"}

	_, err := fetchPriceYearAgo(context.Background(), hist)
	if err == nil {
		t.Fatal("fetchPriceYearAgo err=nil, want error on indicator-repo DB failure")
	}
}

// Both sources empty → (zero, nil). No error, just an honest zero.
func TestFetchPriceYearAgoReturnsZeroWhenAllSourcesEmpty(t *testing.T) {
	snapRepo := &stubSnapshotRepo{notFound: true}
	indRepo := &stubIndicatorRepoForDividend{byID: map[int]Indicator{}}
	hist := &HistoricalData{Repo: snapRepo, IndicatorRepo: indRepo, Slug: "mtlf"}

	got, err := fetchPriceYearAgo(context.Background(), hist)
	if err != nil {
		t.Fatalf("fetchPriceYearAgo err = %v, want nil", err)
	}
	if !got.IsZero() {
		t.Errorf("fetchPriceYearAgo = %s, want 0 when both sources empty", got)
	}
}

// lookupIndicatorAt with no repository wired must return zero, not panic
// or error — that's "not configured", indistinguishable from "no data".
func TestLookupIndicatorAtNilGuards(t *testing.T) {
	v, err := lookupIndicatorAt(context.Background(), nil, 10, time.Now())
	if err != nil || !v.IsZero() {
		t.Errorf("lookupIndicatorAt(nil hist) = (%s, %v), want (0, nil)", v, err)
	}
	hist := &HistoricalData{Slug: "mtlf"}
	v, err = lookupIndicatorAt(context.Background(), hist, 10, time.Now())
	if err != nil || !v.IsZero() {
		t.Errorf("lookupIndicatorAt(nil IndicatorRepo) = (%s, %v), want (0, nil)", v, err)
	}
}

// A real repo error is wrapped and propagated. CLAUDE.md: never conflate
// ErrNotFound with connection/query failures.
func TestLookupIndicatorAtRepoErrorPropagates(t *testing.T) {
	hist := &HistoricalData{
		Slug:          "mtlf",
		IndicatorRepo: &stubIndicatorRepoForDividend{nearestErr: errors.New("conn lost")},
	}
	_, err := lookupIndicatorAt(context.Background(), hist, 10, time.Now())
	if err == nil {
		t.Fatal("lookupIndicatorAt err=nil, want wrapped error")
	}
}

// End-to-end: DividendCalculator.Calculate produces non-zero I17 / I43 / I55
// when only the indicator-repo path is available for I55 — the headline
// regression this guards against is silent-zero ROI when snapshots predate
// the LiveMetrics rollout.
func TestDividendCalculatorEndToEndUsesIndicatorRepoForI55(t *testing.T) {
	snapRepo := &stubSnapshotRepo{notFound: true}
	indRepo := &stubIndicatorRepoForDividend{
		byID: map[int]Indicator{10: {ID: 10, Value: decimal.RequireFromString("6.28")}},
	}
	hist := &HistoricalData{Repo: snapRepo, IndicatorRepo: indRepo, Slug: "mtlf"}

	monthlyDiv := "2440.7"
	data := domain.FundStructureData{
		LiveMetrics: &domain.FundLiveMetrics{MonthlyDividends: &monthlyDiv},
	}
	deps := map[int]Indicator{
		5:  {ID: 5, Value: decimal.NewFromInt(58000)},
		10: {ID: 10, Value: decimal.RequireFromString("6.7")},
	}
	calc := &DividendCalculator{}

	out, err := calc.Calculate(context.Background(), data, deps, hist)
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}

	by := make(map[int]decimal.Decimal, len(out))
	for _, ind := range out {
		by[ind.ID] = ind.Value
	}

	if by[55].IsZero() {
		t.Errorf("I55 = 0, want non-zero (indicator-repo fallback should resolve year-ago price)")
	}
	if by[17].IsZero() {
		t.Errorf("I17 = 0, want non-zero")
	}
	if by[43].IsZero() {
		t.Errorf("I43 = 0, want non-zero")
	}
	// Display precision contract: percentages/ratios at 2 decimals, per-share
	// amounts at 4 decimals (rounding per-share to hundredths zeros them out).
	for _, id := range []int{17, 34, 43} {
		if by[id].Exponent() < -2 {
			t.Errorf("I%d = %s, want exponent ≥ -2 (ratio rounded to hundredths)", id, by[id])
		}
	}
	for _, id := range []int{15, 54} {
		if by[id].Exponent() < -4 {
			t.Errorf("I%d = %s, want exponent ≥ -4 (per-share rounded to ten-thousandths)", id, by[id])
		}
	}
}

// DividendCalculator.Calculate must propagate underlying DB errors so a
// failed daily report is loud, not silently zero.
func TestDividendCalculatorPropagatesSnapshotDBError(t *testing.T) {
	snapRepo := &stubSnapshotRepo{err: errors.New("conn lost")}
	indRepo := &stubIndicatorRepoForDividend{}
	hist := &HistoricalData{Repo: snapRepo, IndicatorRepo: indRepo, Slug: "mtlf"}

	deps := map[int]Indicator{
		5:  {ID: 5, Value: decimal.NewFromInt(58000)},
		10: {ID: 10, Value: decimal.RequireFromString("6.7")},
	}
	_, err := (&DividendCalculator{}).Calculate(context.Background(), domain.FundStructureData{}, deps, hist)
	if err == nil {
		t.Fatal("Calculate err=nil, want propagated DB error")
	}
}

// --- helpers ---

func makeSnap(t *testing.T, data domain.FundStructureData) *snapshot.Snapshot {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return &snapshot.Snapshot{Data: raw}
}

// --- mocks ---

type stubSnapshotRepo struct {
	nearest  *snapshot.Snapshot
	notFound bool
	err      error
	dateFunc func(target time.Time) (*snapshot.Snapshot, error)
}

func (s *stubSnapshotRepo) Save(_ context.Context, _ int, _ time.Time, _ json.RawMessage) error {
	return nil
}
func (s *stubSnapshotRepo) GetLatest(_ context.Context, _ string) (*snapshot.Snapshot, error) {
	return s.nearest, nil
}
func (s *stubSnapshotRepo) GetByDate(_ context.Context, _ string, _ time.Time) (*snapshot.Snapshot, error) {
	return s.nearest, nil
}
func (s *stubSnapshotRepo) GetNearestBefore(_ context.Context, _ string, target time.Time) (*snapshot.Snapshot, error) {
	if s.dateFunc != nil {
		return s.dateFunc(target)
	}
	if s.err != nil {
		return nil, s.err
	}
	if s.notFound {
		return nil, snapshot.ErrNotFound
	}
	return s.nearest, nil
}
func (s *stubSnapshotRepo) List(_ context.Context, _ string, _ int) ([]snapshot.Snapshot, error) {
	return nil, nil
}
func (s *stubSnapshotRepo) ListMeta(_ context.Context, _ string) ([]snapshot.SnapshotMeta, error) {
	return nil, nil
}
func (s *stubSnapshotRepo) GetEntityID(_ context.Context, _ string) (int, error) { return 1, nil }
func (s *stubSnapshotRepo) EnsureEntity(_ context.Context, _, _, _ string) (int, error) {
	return 1, nil
}

type stubIndicatorRepoForDividend struct {
	byID       map[int]Indicator
	history    []HistoryPoint
	nearestErr error
	historyErr error
}

func (s *stubIndicatorRepoForDividend) Save(_ context.Context, _ int, _ time.Time, _ []Indicator) error {
	return nil
}
func (s *stubIndicatorRepoForDividend) GetByDate(_ context.Context, _ string, _ time.Time) ([]Indicator, error) {
	return nil, ErrNotFound
}
func (s *stubIndicatorRepoForDividend) GetLatest(_ context.Context, _ string) ([]Indicator, time.Time, error) {
	return nil, time.Time{}, ErrNotFound
}
func (s *stubIndicatorRepoForDividend) GetHistory(_ context.Context, _ string, _ []int, _ time.Time) ([]HistoryPoint, error) {
	if s.historyErr != nil {
		return nil, s.historyErr
	}
	return s.history, nil
}
func (s *stubIndicatorRepoForDividend) GetNearestBefore(_ context.Context, _ string, _ time.Time) (map[int]Indicator, error) {
	if s.nearestErr != nil {
		return nil, s.nearestErr
	}
	return s.byID, nil
}
