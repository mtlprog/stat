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

	got := fetchPriceYearAgo(context.Background(), hist)
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

	got := fetchPriceYearAgo(context.Background(), hist)
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

	got := fetchPriceYearAgo(context.Background(), hist)
	if !got.Equal(decimal.RequireFromString("5.5")) {
		t.Errorf("fetchPriceYearAgo = %s, want 5.5 (indicator repo, snapshot ErrNotFound)", got)
	}
}

// A transient snapshot DB error must NOT short-circuit the indicator-repo
// fallback. A flaky pg connection on the snapshot query is exactly when we
// most want the alternate source.
func TestFetchPriceYearAgoFallsThroughOnSnapshotDBError(t *testing.T) {
	snapRepo := &stubSnapshotRepo{err: errors.New("conn lost")}
	indRepo := &stubIndicatorRepoForDividend{
		byID: map[int]Indicator{10: {ID: 10, Value: decimal.RequireFromString("4.2")}},
	}
	hist := &HistoricalData{Repo: snapRepo, IndicatorRepo: indRepo, Slug: "mtlf"}

	got := fetchPriceYearAgo(context.Background(), hist)
	if !got.Equal(decimal.RequireFromString("4.2")) {
		t.Errorf("fetchPriceYearAgo = %s, want 4.2 (fallback despite snapshot error)", got)
	}
}

// Both sources empty → zero. No panic, just a zero return.
func TestFetchPriceYearAgoReturnsZeroWhenAllSourcesEmpty(t *testing.T) {
	snapRepo := &stubSnapshotRepo{notFound: true}
	indRepo := &stubIndicatorRepoForDividend{byID: map[int]Indicator{}}
	hist := &HistoricalData{Repo: snapRepo, IndicatorRepo: indRepo, Slug: "mtlf"}

	if got := fetchPriceYearAgo(context.Background(), hist); !got.IsZero() {
		t.Errorf("fetchPriceYearAgo = %s, want 0 when both sources empty", got)
	}
}

// fetchMonthlyDividends12m: every month falls back to I11 from the indicator
// repo when no snapshot is available.
func TestFetchMonthlyDividends12mUsesIndicatorRepoForMissingMonths(t *testing.T) {
	snapRepo := &stubSnapshotRepo{notFound: true}
	indRepo := &stubIndicatorRepoForDividend{
		history: constantHistory(11, "2926", 13), // 13 monthly points covering 12 lookups
	}
	hist := &HistoricalData{Repo: snapRepo, IndicatorRepo: indRepo, Slug: "mtlf"}

	got := fetchMonthlyDividends12m(context.Background(), hist)
	if len(got) != 12 {
		t.Fatalf("len = %d, want 12 months filled from indicator repo", len(got))
	}
	for i, v := range got {
		if !v.Equal(decimal.RequireFromString("2926")) {
			t.Errorf("month %d = %s, want 2926", i, v)
		}
	}
}

// Mixed-source: snapshot covers recent months (LiveMetrics era), indicator
// repo covers older months. The realistic production state for the next ~6
// months after the LiveMetrics rollout. Use a date-keyed snapshot stub.
func TestFetchMonthlyDividends12mMixedSnapshotAndRepo(t *testing.T) {
	now := time.Now().UTC()
	livePeriodCutoff := now.AddDate(0, -6, 0) // newer than this gets a LiveMetrics snapshot
	snapRepo := &stubSnapshotRepo{
		dateFunc: func(target time.Time) (*snapshot.Snapshot, error) {
			if target.Before(livePeriodCutoff) {
				return nil, snapshot.ErrNotFound
			}
			liveStr := "1500"
			return makeSnap(t, domain.FundStructureData{
				LiveMetrics: &domain.FundLiveMetrics{MonthlyDividends: &liveStr},
			}), nil
		},
	}
	indRepo := &stubIndicatorRepoForDividend{
		history: constantHistory(11, "2926", 13),
	}
	hist := &HistoricalData{Repo: snapRepo, IndicatorRepo: indRepo, Slug: "mtlf"}

	got := fetchMonthlyDividends12m(context.Background(), hist)
	if len(got) != 12 {
		t.Fatalf("len = %d, want 12", len(got))
	}
	var snapCount, repoCount int
	for _, v := range got {
		switch v.String() {
		case "1500":
			snapCount++
		case "2926":
			repoCount++
		default:
			t.Errorf("unexpected value: %s", v)
		}
	}
	if snapCount == 0 || repoCount == 0 {
		t.Errorf("want a mix of snapshot+repo values; got snap=%d repo=%d", snapCount, repoCount)
	}
}

// A genuine zero in the snapshot LiveMetrics is a real data point — it must
// be appended, not silently dropped via a "0 means missing" coincidence.
func TestFetchMonthlyDividends12mPreservesGenuineZeroFromSnapshot(t *testing.T) {
	zero := "0"
	snapRepo := &stubSnapshotRepo{
		nearest: makeSnap(t, domain.FundStructureData{
			LiveMetrics: &domain.FundLiveMetrics{MonthlyDividends: &zero},
		}),
	}
	// Indicator repo has a non-zero value — must NOT be used as a substitute,
	// because the snapshot answered authoritatively with zero.
	indRepo := &stubIndicatorRepoForDividend{
		history: constantHistory(11, "9999", 13),
	}
	hist := &HistoricalData{Repo: snapRepo, IndicatorRepo: indRepo, Slug: "mtlf"}

	got := fetchMonthlyDividends12m(context.Background(), hist)
	if len(got) != 12 {
		t.Fatalf("len = %d, want 12 (zeros preserved)", len(got))
	}
	for i, v := range got {
		if !v.IsZero() {
			t.Errorf("month %d = %s, want 0 (snapshot zero preserved over repo 9999)", i, v)
		}
	}
}

// lookupIndicatorAt with no repository wired must return zero, not panic.
func TestLookupIndicatorAtNilGuards(t *testing.T) {
	if got := lookupIndicatorAt(context.Background(), nil, 10, time.Now()); !got.IsZero() {
		t.Errorf("lookupIndicatorAt(nil hist) = %s, want 0", got)
	}
	hist := &HistoricalData{Slug: "mtlf"} // IndicatorRepo nil
	if got := lookupIndicatorAt(context.Background(), hist, 10, time.Now()); !got.IsZero() {
		t.Errorf("lookupIndicatorAt(nil IndicatorRepo) = %s, want 0", got)
	}
}

// lookupIndicatorAt returns zero on a real repo error and never panics on a
// nil response map.
func TestLookupIndicatorAtRepoError(t *testing.T) {
	hist := &HistoricalData{
		Slug:          "mtlf",
		IndicatorRepo: &stubIndicatorRepoForDividend{nearestErr: errors.New("conn lost")},
	}
	if got := lookupIndicatorAt(context.Background(), hist, 10, time.Now()); !got.IsZero() {
		t.Errorf("lookupIndicatorAt(repo err) = %s, want 0", got)
	}
}

// End-to-end: DividendCalculator.Calculate produces non-zero I16/I17/I55
// when only the indicator-repo path is available — the headline regression
// the PR is meant to fix.
func TestDividendCalculatorEndToEndUsesIndicatorRepoForI55(t *testing.T) {
	snapRepo := &stubSnapshotRepo{notFound: true}
	indRepo := &stubIndicatorRepoForDividend{
		// I10 year-ago = 6.28 (ratio with i10=6.7 keeps factor in (0,1)).
		byID:    map[int]Indicator{10: {ID: 10, Value: decimal.RequireFromString("6.28")}},
		history: constantHistory(11, "2440.7", 13),
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
	if by[16].IsZero() {
		t.Errorf("I16 = 0, want non-zero")
	}
	if by[17].IsZero() {
		t.Errorf("I17 = 0, want non-zero")
	}
	// Sanity check: I17 = (I54 / I55) * 100.
	wantI17 := by[54].Div(by[55]).Mul(decimal.NewFromInt(100))
	if !by[17].Equal(wantI17) {
		t.Errorf("I17 = %s, want %s ((I54/I55)*100)", by[17], wantI17)
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

// constantHistory returns count monthly HistoryPoints for indicator id, all
// with the given decimal value, oldest first (matching GetHistory's ASC ordering).
func constantHistory(id int, value string, count int) []HistoryPoint {
	now := time.Now().UTC()
	v := decimal.RequireFromString(value)
	pts := make([]HistoryPoint, count)
	for i := range count {
		pts[i] = HistoryPoint{
			SnapshotDate: now.AddDate(0, -(count - i), 0),
			IndicatorID:  id,
			Value:        v,
		}
	}
	return pts
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
