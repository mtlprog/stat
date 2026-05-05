package indicator

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/snapshot"
)

// Snapshots before the LiveMetrics rollout (Feb 2026) cannot supply an MTL
// price — the year-ago snapshot has neither LiveMetrics nor an MTL token in
// any account portfolio (the fund doesn't hold its own issued shares).
// fetchPriceYearAgo must fall back to I10 in the indicator repository, which
// has continuous history from the legacy MONITORING import.
func TestFetchPriceYearAgoFallsBackToIndicatorRepo(t *testing.T) {
	emptyData, err := json.Marshal(domain.FundStructureData{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	snapRepo := &stubSnapshotRepo{
		nearest: &snapshot.Snapshot{Data: emptyData}, // no LiveMetrics, no tokens
	}
	indRepo := &stubIndicatorRepoForDividend{
		byID: map[int]Indicator{
			10: {ID: 10, Value: decimal.RequireFromString("6.28")},
		},
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
	data := domain.FundStructureData{
		LiveMetrics: &domain.FundLiveMetrics{MTLMarketPrice: &priceStr},
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	snapRepo := &stubSnapshotRepo{nearest: &snapshot.Snapshot{Data: raw}}
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

// fetchMonthlyDividends12m must return I11 from the indicator repository for
// months that lack a snapshot, so I16 / I33 can use the full 12-point window
// for medianing even before the LiveMetrics era.
func TestFetchMonthlyDividends12mUsesIndicatorRepoForMissingMonths(t *testing.T) {
	snapRepo := &stubSnapshotRepo{notFound: true}
	indRepo := &stubIndicatorRepoForDividend{
		byID: map[int]Indicator{11: {ID: 11, Value: decimal.RequireFromString("2926")}},
	}
	hist := &HistoricalData{Repo: snapRepo, IndicatorRepo: indRepo, Slug: "mtlf"}

	got := fetchMonthlyDividends12m(context.Background(), hist)
	if len(got) != 12 {
		t.Errorf("len = %d, want 12 months filled from indicator repo", len(got))
	}
	for i, v := range got {
		if !v.Equal(decimal.RequireFromString("2926")) {
			t.Errorf("month %d = %s, want 2926", i, v)
		}
	}
}

// --- mocks ---

type stubSnapshotRepo struct {
	nearest  *snapshot.Snapshot
	notFound bool
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
func (s *stubSnapshotRepo) GetNearestBefore(_ context.Context, _ string, _ time.Time) (*snapshot.Snapshot, error) {
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
	byID map[int]Indicator
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
	return nil, nil
}
func (s *stubIndicatorRepoForDividend) GetNearestBefore(_ context.Context, _ string, _ time.Time) (map[int]Indicator, error) {
	return s.byID, nil
}
