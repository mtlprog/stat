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

// stepTimeout caps each Horizon-bound enrichment step. If a step doesn't
// finish within this budget, we abandon it and fall back to the prior day's
// persisted value — so one slow endpoint can't poison the whole report.
const stepTimeout = 90 * time.Second

// Horizon provides the Horizon API calls required to capture live metrics.
type Horizon interface {
	FetchAssetStats(ctx context.Context, asset domain.AssetInfo) (horizon.AssetStats, error)
	FetchAssetHolderCountByBalance(ctx context.Context, asset domain.AssetInfo, minBalance decimal.Decimal) (int, error)
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

	stage := func(name string) func() {
		t := time.Now()
		slog.Debug("metrics step start", "step", name)
		return func() {
			slog.Debug("metrics step done", "step", name, "duration_ms", time.Since(t).Milliseconds())
		}
	}

	done := stage("MTL_circulation")
	if circ, ok := s.fetchCirculation(ctx, mtlAsset); ok {
		m.MTLCirculation = ptr(circ.String())
	} else {
		m.MTLCirculation = pickPrior(prev, 6)
	}
	done()

	done = stage("MTLRECT_circulation")
	if circ, ok := s.fetchCirculation(ctx, mtlrectAsset); ok {
		m.MTLRECTCirculation = ptr(circ.String())
	} else {
		m.MTLRECTCirculation = pickPrior(prev, 7)
	}
	done()

	// I24: count of EURMTL trustlines with non-zero balance. Uses a paginated
	// walk because /assets `accounts.authorized` includes empty trustlines and
	// would inflate the count by ~3x.
	done = stage("EURMTL_holders")
	{
		stepCtx, cancel := withStepTimeout(ctx)
		minNonZero := decimal.New(1, -7)
		if count, err := s.horizon.FetchAssetHolderCountByBalance(stepCtx, eurmtlAsset, minNonZero); err != nil {
			slog.Error("metrics: fetch EURMTL holders failed, reusing prior I24", "error", err)
			m.EURMTLParticipants = pickPrior(prev, 24)
		} else {
			m.EURMTLParticipants = ptr(decimal.NewFromInt(int64(count)).String())
		}
		cancel()
	}
	done()

	// I40: count of MTLAP holders with balance ≥1. /assets `accounts.authorized`
	// for MTLAP returns ~1 because most holders are AUTHORIZED_TO_MAINTAIN_LIABILITIES,
	// not authorized — so we have to walk and apply the balance filter.
	done = stage("MTLAP_holders")
	{
		stepCtx, cancel := withStepTimeout(ctx)
		minOne := decimal.NewFromInt(1)
		if count, err := s.horizon.FetchAssetHolderCountByBalance(stepCtx, mtlapAsset, minOne); err != nil {
			slog.Error("metrics: fetch MTLAP holders failed, reusing prior I40", "error", err)
			m.MTLAPHolders = pickPrior(prev, 40)
		} else {
			m.MTLAPHolders = ptr(decimal.NewFromInt(int64(count)).String())
		}
		cancel()
	}
	done()

	done = stage("MTL_MTLRECT_shareholders_walk")
	if count, median, ok := s.fetchShareholderStats(ctx, mtlAsset, mtlrectAsset); ok {
		m.MTLShareholders = ptr(decimal.NewFromInt(int64(count)).String())
		m.MTLShareholdersMedian = ptr(median.String())
	} else {
		m.MTLShareholders = pickPrior(prev, 27)
		m.MTLShareholdersMedian = pickPrior(prev, 23)
	}
	done()

	// I11: monthly dividend outflow summed across every fund account that might
	// be a dividend source (issuer first, then APART/etc). The 30-day rolling
	// window means the value can legitimately go to zero on the last day a
	// payment falls off — but the user's expectation is that I11 is a "last
	// known monthly amount", monotonic between disbursements. So when the live
	// sum is zero AND yesterday's persisted value was non-zero, we keep
	// yesterday's value rather than write a zero.
	done = stage("dividends_walk_all_accounts")
	{
		m.MonthlyDividends = s.computeI11(ctx, prev)
	}
	done()

	done = stage("eurmtl_daily_volume")
	dailyVol, dailyOK := s.fetchDailyVolume(ctx, date)
	if dailyOK {
		m.EURMTLDailyVolume = ptr(dailyVol.String())
	} else {
		m.EURMTLDailyVolume = pickPrior(prev, 25)
	}
	done()

	done = stage("i26_incremental")
	m.EURMTL30dVolume = s.computeI26(ctx, date, dailyVol, dailyOK, prev)
	done()

	done = stage("MTL_bid")
	{
		stepCtx, cancel := withStepTimeout(ctx)
		if bid, err := s.price.GetBidPrice(stepCtx, mtlAsset, eurmtlAsset); err != nil {
			slog.Error("metrics: fetch MTL bid price failed, reusing prior I10", "error", err)
			m.MTLMarketPrice = pickPrior(prev, 10)
		} else {
			m.MTLMarketPrice = ptr(bid.String())
		}
		cancel()
	}
	done()

	done = stage("MTLRECT_bid")
	{
		stepCtx, cancel := withStepTimeout(ctx)
		if bid, err := s.price.GetBidPrice(stepCtx, mtlrectAsset, eurmtlAsset); err != nil {
			slog.Error("metrics: fetch MTLRECT bid price failed, reusing prior I49", "error", err)
			m.MTLRECTMarketPrice = pickPrior(prev, 49)
		} else {
			m.MTLRECTMarketPrice = ptr(bid.String())
		}
		cancel()
	}
	done()

	data.LiveMetrics = m
	return nil
}

// priorMetrics loads yesterday-or-earlier indicators for sticky-fallback.
//
// We anchor at `date - 1 day` rather than `date` so that re-running the report
// for today (idempotent) doesn't shadow the fallback with the just-written —
// possibly broken — values it was meant to recover from. GetNearestBefore is
// inclusive on the upper bound, so an anchor of `date-1` returns the most
// recent indicator set whose snapshot_date ≤ yesterday.
//
// Returns nil if no repository is configured or no prior data exists.
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

// withStepTimeout returns a child context bounded by stepTimeout, so a single
// slow Horizon endpoint can't burn the report's overall budget.
func withStepTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, stepTimeout)
}

// fetchCirculation derives circulating supply from a single /assets call:
// total supply minus AMM-pool reserves. Returns ok=false on fetch failure.
func (s *Service) fetchCirculation(ctx context.Context, asset domain.AssetInfo) (decimal.Decimal, bool) {
	stepCtx, cancel := withStepTimeout(ctx)
	defer cancel()
	stats, err := s.horizon.FetchAssetStats(stepCtx, asset)
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
// Each per-asset sweep gets its own step timeout so the slower side can't drag
// the report past its overall budget; either failure aborts to ok=false and
// the caller falls back to the prior day's persisted I27 / I23.
func (s *Service) fetchShareholderStats(ctx context.Context, mtlAsset, mtlrectAsset domain.AssetInfo) (int, decimal.Decimal, bool) {
	minOne := decimal.NewFromInt(1)

	mtlCtx, mtlCancel := withStepTimeout(ctx)
	defer mtlCancel()
	mtl, err := s.horizon.FetchAssetHolderBalancesByBalance(mtlCtx, mtlAsset, minOne)
	if err != nil {
		slog.Error("metrics: fetch MTL holders failed", "error", err)
		return 0, decimal.Zero, false
	}

	mtlrectCtx, mtlrectCancel := withStepTimeout(ctx)
	defer mtlrectCancel()
	mtlrect, err := s.horizon.FetchAssetHolderBalancesByBalance(mtlrectCtx, mtlrectAsset, minOne)
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

// computeI11 sums monthly EURMTL dividend outflow across every fund account.
// On any per-account walk failure the contribution is treated as zero (logged),
// so a single Horizon hiccup doesn't poison the whole figure. If the final sum
// is zero but the prior day had a non-zero I11, we keep yesterday's value —
// the user-visible cell is meant to be "last known monthly dividend" and must
// not flicker to zero between monthly disbursements.
//
// Trade-off: the rolling 30-day window means I11 can legitimately drop to zero
// the day a payment falls off the window. Sticky-on-zero will mask that until
// a new dividend is posted. Per-product requirement: never show a misleading
// zero in MONITORING; reflect the last known value instead.
func (s *Service) computeI11(ctx context.Context, prev map[int]indicator.Indicator) *string {
	total := decimal.Zero
	successCount := 0
	for _, addr := range s.fundAddrs {
		stepCtx, cancel := withStepTimeout(ctx)
		amt, err := s.horizon.FetchMonthlyEURMTLOutflow(stepCtx, addr, s.fundAddrs)
		cancel()
		if err != nil {
			slog.Error("metrics: fetch dividends from account failed, treating as zero contribution",
				"account", addr, "error", err)
			continue
		}
		successCount++
		total = total.Add(amt)
	}

	// Distinguish "all walks failed" from "everyone really paid 0": the cascade
	// is what an operator should see, not 11 individual ERROR lines plus a
	// quiet sticky reuse.
	if successCount == 0 && len(s.fundAddrs) > 0 {
		slog.Error("metrics: all dividend account walks failed, I11 will fall back to prior",
			"accounts", len(s.fundAddrs))
	}

	if total.IsZero() {
		if priorStr := pickPrior(prev, 11); priorStr != nil && !domain.SafeParse(*priorStr).IsZero() {
			slog.Info("metrics: I11 live sum is zero, reusing prior (intentional sticky-on-zero)",
				"prior", *priorStr)
			return priorStr
		}
	}
	return ptr(total.String())
}

// fetchDailyVolume returns total EURMTL payment volume in the 24h window
// preceding `date`. Wrapped in stepTimeout because the underlying /payments
// endpoint can paginate through thousands of pages on busy days.
func (s *Service) fetchDailyVolume(ctx context.Context, date time.Time) (decimal.Decimal, bool) {
	stepCtx, cancel := withStepTimeout(ctx)
	defer cancel()
	since := date.AddDate(0, 0, -1)
	vol, err := s.horizon.FetchEURMTLPaymentVolume(stepCtx, since)
	if err != nil {
		slog.Error("metrics: fetch EURMTL daily volume failed", "error", err)
		return decimal.Zero, false
	}
	return vol, true
}

// computeI26 produces the rolling 30-day EURMTL volume by subtracting the
// daily volume from 30 days ago and adding today's. Falls back to yesterday's
// persisted I26 when today's daily fetch failed or the 30-day-ago snapshot is
// missing.
//
// Cold-start caveat: if both today's daily fetch failed AND no prior I26
// exists in the DB, the function returns nil and the calculator resolves I26
// to zero. This path is not expected in production (the prod DB is fully
// seeded) and intentionally fails loud rather than fabricate a value.
func (s *Service) computeI26(ctx context.Context, date time.Time, dailyVol decimal.Decimal, dailyOK bool, prev map[int]indicator.Indicator) *string {
	yesterday30d := pickPrior(prev, 26)
	if !dailyOK || yesterday30d == nil {
		if yesterday30d == nil {
			slog.Error("metrics: no prior I26 in DB, skipping incremental — calculator will see 0",
				"dailyOK", dailyOK)
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
