package metrics

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/horizon"
	"github.com/mtlprog/stat/internal/indicator"
)

// fundSlug is the entity slug used by the report flow. Lives here because the
// metrics service is tightly coupled with the daily report and shares its
// indicator history.
const fundSlug = "mtlf"

// Horizon provides the Horizon API calls required to capture live metrics.
type Horizon interface {
	FetchAssetStats(ctx context.Context, asset domain.AssetInfo) (horizon.AssetStats, error)
	FetchAssetHolderBalancesByBalance(ctx context.Context, asset domain.AssetInfo, minBalance decimal.Decimal) (map[string]decimal.Decimal, error)
	FetchMonthlyEURMTLOutflow(ctx context.Context, accountID string, fundAddresses []string) (decimal.Decimal, error)
	FetchEURMTLPaymentVolume(ctx context.Context, since time.Time) (decimal.Decimal, error)
}

// PriceSource provides market price lookups.
type PriceSource interface {
	GetBidPrice(ctx context.Context, asset, baseAsset domain.AssetInfo) (decimal.Decimal, error)
}

// Service captures live metrics and writes them to FundStructureData.LiveMetrics.
// It is the single point of contact with Horizon for snapshot-time live values —
// indicator calculators downstream read only from LiveMetrics, never Horizon.
type Service struct {
	horizon   Horizon
	price     PriceSource
	indicator indicator.Repository
	fundAddrs []string
}

// NewService creates a new metrics Service. indicatorRepo is required for the
// sticky-fallback path (reusing the prior day's value when a fetch fails) and
// for the I26 incremental computation; passing nil disables both behaviours.
func NewService(h Horizon, p PriceSource, indicatorRepo indicator.Repository, fundAddrs []string) *Service {
	return &Service{
		horizon:   h,
		price:     p,
		indicator: indicatorRepo,
		fundAddrs: fundAddrs,
	}
}

// EnrichMetrics computes all live indicators (I6, I7, I10, I11, I23-I27, I40, I49)
// for the snapshot dated `date` and stores them in data.LiveMetrics. On any
// fetch failure it logs an error and falls back to the prior day's persisted
// value, never zero.
func (s *Service) EnrichMetrics(ctx context.Context, date time.Time, data *domain.FundStructureData) error {
	prev := s.priorMetrics(ctx, date)
	m := &domain.FundLiveMetrics{}

	mtlAsset := domain.NewAssetInfo("MTL", domain.IssuerAddress)
	mtlrectAsset := domain.NewAssetInfo("MTLRECT", domain.IssuerAddress)
	mtlapAsset := domain.MTLAPAsset()
	eurmtlAsset := domain.EURMTLAsset()

	// /assets stats — one HTTP call per asset gives circulation and holder count.
	if circ, ok := s.fetchCirculation(ctx, mtlAsset); ok {
		m.MTLCirculation = ptr(circ.String())
	} else {
		m.MTLCirculation = pickPrior(prev, 6)
	}
	if circ, ok := s.fetchCirculation(ctx, mtlrectAsset); ok {
		m.MTLRECTCirculation = ptr(circ.String())
	} else {
		m.MTLRECTCirculation = pickPrior(prev, 7)
	}
	if stats, err := s.horizon.FetchAssetStats(ctx, eurmtlAsset); err != nil {
		slog.Error("metrics: fetch EURMTL stats failed, reusing prior I24", "error", err)
		m.EURMTLParticipants = pickPrior(prev, 24)
	} else {
		m.EURMTLParticipants = ptr(decimal.NewFromInt(int64(stats.HoldersAuthorized)).String())
	}
	if stats, err := s.horizon.FetchAssetStats(ctx, mtlapAsset); err != nil {
		slog.Error("metrics: fetch MTLAP stats failed, reusing prior I40", "error", err)
		m.MTLAPHolders = pickPrior(prev, 40)
	} else {
		m.MTLAPHolders = ptr(decimal.NewFromInt(int64(stats.HoldersAuthorized)).String())
	}

	// I27 (count) and I23 (median) come from the same paginated holder walk.
	if count, median, ok := s.fetchShareholderStats(ctx, mtlAsset, mtlrectAsset); ok {
		m.MTLShareholders = ptr(decimal.NewFromInt(int64(count)).String())
		m.MTLShareholdersMedian = ptr(median.String())
	} else {
		m.MTLShareholders = pickPrior(prev, 27)
		m.MTLShareholdersMedian = pickPrior(prev, 23)
	}

	// I11: monthly dividends from issuer only.
	if div, err := s.horizon.FetchMonthlyEURMTLOutflow(ctx, domain.IssuerAddress, s.fundAddrs); err != nil {
		slog.Error("metrics: fetch monthly dividends failed, reusing prior I11", "error", err)
		m.MonthlyDividends = pickPrior(prev, 11)
	} else {
		m.MonthlyDividends = ptr(div.String())
	}

	// I25: EURMTL payment volume over the prior 24h.
	dailyVol, dailyOK := s.fetchDailyVolume(ctx, date)
	if dailyOK {
		m.EURMTLDailyVolume = ptr(dailyVol.String())
	} else {
		m.EURMTLDailyVolume = pickPrior(prev, 25)
	}

	// I26: incremental from yesterday's I26 + today's I25 − I25 from 30 days ago.
	// Falls back to yesterday's I26 if any input is missing.
	m.EURMTL30dVolume = s.computeI26(ctx, date, dailyVol, dailyOK, prev)

	// Bid prices.
	if bid, err := s.price.GetBidPrice(ctx, mtlAsset, eurmtlAsset); err != nil {
		slog.Error("metrics: fetch MTL bid price failed, reusing prior I10", "error", err)
		m.MTLMarketPrice = pickPrior(prev, 10)
	} else {
		m.MTLMarketPrice = ptr(bid.String())
	}
	if bid, err := s.price.GetBidPrice(ctx, mtlrectAsset, eurmtlAsset); err != nil {
		slog.Error("metrics: fetch MTLRECT bid price failed, reusing prior I49", "error", err)
		m.MTLRECTMarketPrice = pickPrior(prev, 49)
	} else {
		m.MTLRECTMarketPrice = ptr(bid.String())
	}

	data.LiveMetrics = m
	return nil
}

// priorMetrics loads the latest indicator set strictly before `date` for use as
// sticky-fallback. Returns nil if the repository is unset or has no prior data.
func (s *Service) priorMetrics(ctx context.Context, date time.Time) map[int]indicator.Indicator {
	if s.indicator == nil {
		return nil
	}
	prev, err := s.indicator.GetNearestBefore(ctx, fundSlug, date.AddDate(0, 0, -1))
	if err != nil {
		slog.Error("metrics: load prior indicators for fallback failed", "error", err)
		return nil
	}
	return prev
}

// fetchCirculation derives circulating supply from a single /assets call:
// total supply minus AMM-pool reserves. Returns ok=false on fetch failure.
func (s *Service) fetchCirculation(ctx context.Context, asset domain.AssetInfo) (decimal.Decimal, bool) {
	stats, err := s.horizon.FetchAssetStats(ctx, asset)
	if err != nil {
		slog.Error("metrics: fetch asset stats failed", "asset", asset.Code, "error", err)
		return decimal.Zero, false
	}
	c := stats.TotalSupply.Sub(stats.LiquidityPools)
	if c.IsNegative() {
		c = decimal.Zero
	}
	return c, true
}

// fetchShareholderStats walks all MTL and MTLRECT holders with balance ≥1 and
// returns the count of unique holders and the median per-holder total.
// One paginated sweep per asset; both asset failures result in ok=false.
func (s *Service) fetchShareholderStats(ctx context.Context, mtlAsset, mtlrectAsset domain.AssetInfo) (int, decimal.Decimal, bool) {
	minOne := decimal.NewFromInt(1)

	mtl, err := s.horizon.FetchAssetHolderBalancesByBalance(ctx, mtlAsset, minOne)
	if err != nil {
		slog.Error("metrics: fetch MTL holders failed", "error", err)
		return 0, decimal.Zero, false
	}
	mtlrect, err := s.horizon.FetchAssetHolderBalancesByBalance(ctx, mtlrectAsset, minOne)
	if err != nil {
		slog.Error("metrics: fetch MTLRECT holders failed", "error", err)
		return 0, decimal.Zero, false
	}

	merged := make(map[string]decimal.Decimal, len(mtl)+len(mtlrect))
	for id, bal := range mtl {
		merged[id] = bal
	}
	for id, bal := range mtlrect {
		merged[id] = merged[id].Add(bal)
	}

	values := make([]decimal.Decimal, 0, len(merged))
	for _, bal := range merged {
		values = append(values, bal)
	}
	return len(merged), median(values), true
}

// fetchDailyVolume returns total EURMTL payment volume in the 24h window
// preceding `date`.
func (s *Service) fetchDailyVolume(ctx context.Context, date time.Time) (decimal.Decimal, bool) {
	since := date.AddDate(0, 0, -1)
	vol, err := s.horizon.FetchEURMTLPaymentVolume(ctx, since)
	if err != nil {
		slog.Error("metrics: fetch EURMTL daily volume failed", "error", err)
		return decimal.Zero, false
	}
	return vol, true
}

// computeI26 produces the rolling 30-day EURMTL volume by subtracting the
// daily volume from 30 days ago and adding today's. Falls back to yesterday's
// I26 if any input is missing — never produces a 0 from a fetch gap.
func (s *Service) computeI26(ctx context.Context, date time.Time, dailyVol decimal.Decimal, dailyOK bool, prev map[int]indicator.Indicator) *string {
	yesterday30d := pickPrior(prev, 26)
	if !dailyOK || yesterday30d == nil {
		if yesterday30d == nil {
			slog.Error("metrics: no prior I26 in DB, skipping incremental — calculator will see 0")
		}
		return yesterday30d
	}

	thirtyDayHist := s.lookup(ctx, 25, date.AddDate(0, 0, -30))
	if thirtyDayHist == nil {
		slog.Error("metrics: no I25 from 30 days ago, reusing prior I26", "target", date.AddDate(0, 0, -30).Format("2006-01-02"))
		return yesterday30d
	}

	prevVal := domain.SafeParse(*yesterday30d)
	out := prevVal.Add(dailyVol).Sub(*thirtyDayHist)
	if out.IsNegative() {
		out = decimal.Zero
	}
	return ptr(out.String())
}

// lookup returns indicator `id` at-or-before `target` from the repository,
// or nil if missing.
func (s *Service) lookup(ctx context.Context, id int, target time.Time) *decimal.Decimal {
	if s.indicator == nil {
		return nil
	}
	res, err := s.indicator.GetNearestBefore(ctx, fundSlug, target)
	if err != nil {
		slog.Error("metrics: lookup historical indicator failed", "id", id, "target", target.Format("2006-01-02"), "error", err)
		return nil
	}
	if res == nil {
		return nil
	}
	ind, ok := res[id]
	if !ok {
		return nil
	}
	return &ind.Value
}

// pickPrior returns the prior day's value for the given indicator ID as a
// *string suitable for FundLiveMetrics fields. Nil if the prior set or ID is
// missing.
func pickPrior(prev map[int]indicator.Indicator, id int) *string {
	if prev == nil {
		return nil
	}
	ind, ok := prev[id]
	if !ok {
		return nil
	}
	v := ind.Value.String()
	return &v
}

func ptr(s string) *string { return &s }

// median returns the middle value of values, averaging the two middle elements
// if the count is even. Returns zero for an empty slice.
func median(values []decimal.Decimal) decimal.Decimal {
	n := len(values)
	if n == 0 {
		return decimal.Zero
	}
	sorted := make([]decimal.Decimal, n)
	copy(sorted, values)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].LessThan(sorted[j]) })
	if n%2 == 1 {
		return sorted[n/2]
	}
	return sorted[n/2-1].Add(sorted[n/2]).Div(decimal.NewFromInt(2))
}
