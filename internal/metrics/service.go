package metrics

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/horizon"
	"github.com/mtlprog/stat/internal/indicator"
	"github.com/mtlprog/stat/internal/stellarexpert"
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
	FetchAssetHolderIDsByBalance(ctx context.Context, asset domain.AssetInfo, minBalance decimal.Decimal) ([]string, error)
	FetchMonthlyEURMTLOutflow(ctx context.Context, accountID string, fundAddresses []string) (decimal.Decimal, error)
}

// PriceSource provides market price lookups.
type PriceSource interface {
	GetAverageTradePrice(ctx context.Context, base, counter domain.AssetInfo, limit int) (decimal.Decimal, error)
}

// PaymentStatsSource provides daily and cumulative EURMTL payment volume —
// the data backing I25 and I26. The production implementation hits
// stellar.expert's pre-aggregated /stats-history endpoint; tests pass a stub.
type PaymentStatsSource interface {
	FetchEURMTLPaymentStats(ctx context.Context, date time.Time) (stellarexpert.Stats, error)
}

// tradesAvgWindow is the number of most-recent trades averaged to produce
// market-price indicators (I10 for MTL, I49 for MTLRECT). Matches the legacy
// Python `stellar_get_trade_cost`.
const tradesAvgWindow = 100

// Service captures live metrics and writes them to FundStructureData.LiveMetrics.
// It is the single point of contact with Horizon for snapshot-time live values —
// indicator calculators downstream read only from LiveMetrics, never Horizon.
type Service struct {
	horizon   Horizon
	price     PriceSource
	expert    PaymentStatsSource
	indicator indicator.Repository
	fundAddrs []string
}

// NewService creates a new metrics Service. indicatorRepo is required for the
// sticky-fallback path (reusing the prior day's value when a fetch fails);
// passing nil disables it.
func NewService(h Horizon, p PriceSource, expert PaymentStatsSource, indicatorRepo indicator.Repository, fundAddrs []string) *Service {
	return &Service{
		horizon:   h,
		price:     p,
		expert:    expert,
		indicator: indicatorRepo,
		fundAddrs: fundAddrs,
	}
}

// EnrichMetrics computes all live indicators (I6, I7, I10, I11, I18, I23-I27,
// I40, I49, I62) for the snapshot dated `date` and stores them in
// data.LiveMetrics. On any fetch failure it logs an error and falls back to
// the prior day's persisted value, never zero.
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
	merged, stats, shareholdersOK := s.fetchShareholderStats(ctx, mtlAsset, mtlrectAsset)
	if shareholdersOK {
		m.MTLShareholders = ptr(decimal.NewFromInt(int64(stats.countAtLeastOne)).String())
		m.MTLShareholdersAny = ptr(decimal.NewFromInt(int64(stats.countAny)).String())
		m.MTLShareholdersMedian = ptr(stats.median.String())
	} else {
		m.MTLShareholders = pickPrior(prev, 27)
		m.MTLShareholdersAny = pickPrior(prev, 62)
		m.MTLShareholdersMedian = pickPrior(prev, 23)
	}
	done()

	// I18: shareholders (MTL+MTLRECT > 1) who also hold a non-zero EURMTL trustline.
	// Reuses the merged map from the I27 walk to avoid a second MTL/MTLRECT
	// pagination round-trip; only the EURMTL ID list is fetched here.
	done = stage("EURMTL_shareholders_intersection")
	if shareholdersOK {
		if i18, ok := s.computeI18(ctx, eurmtlAsset, merged); ok {
			m.EURMTLShareholders = ptr(decimal.NewFromInt(int64(i18)).String())
		} else {
			m.EURMTLShareholders = pickPrior(prev, 18)
		}
	} else {
		// Cascade: a failed shareholder walk (already logged at Error inside
		// fetchShareholderStats) degrades I18, I23, I27, AND I62 simultaneously.
		// Info-level surface so the second-order effect is visible without
		// duplicating the upstream Error severity.
		slog.Info("metrics: I18 falls back to prior because the shareholder walk failed upstream (cascade with I23, I27, I62)")
		m.EURMTLShareholders = pickPrior(prev, 18)
	}
	done()

	// I11: monthly dividend outflow summed across every fund account in
	// `s.fundAddrs` (the full domain.AccountRegistry, no ordering). The 30-day
	// rolling window means the value can legitimately go to zero on the last
	// day a payment falls off — but the user's expectation is that I11 is a
	// "last known monthly amount", monotonic between disbursements. So when
	// the live sum is zero AND yesterday's persisted value was non-zero, we
	// keep yesterday's value rather than write a zero.
	done = stage("dividends_walk_all_accounts")
	{
		m.MonthlyDividends = s.computeI11(ctx, prev)
	}
	done()

	// I25 (daily) and I26 (cumulative) come from a single call to
	// stellar.expert's pre-aggregated /stats-history. Falls back to the prior
	// day's persisted values on any failure — including ErrNoDailyEntry, which
	// fires when stellar.expert hasn't ingested today yet.
	done = stage("eurmtl_payment_stats")
	{
		stepCtx, cancel := withStepTimeout(ctx)
		stats, err := s.expert.FetchEURMTLPaymentStats(stepCtx, date)
		cancel()
		switch {
		case err == nil:
			m.EURMTLDailyVolume = ptr(stats.Daily.String())
			m.EURMTLPaymentTotal = ptr(stats.Cumulative.String())
		case errors.Is(err, stellarexpert.ErrNoDailyEntry):
			slog.Info("metrics: stellar.expert has no entry for today yet, reusing prior I25/I26",
				"date", date.Format("2006-01-02"))
			m.EURMTLDailyVolume = pickPrior(prev, 25)
			m.EURMTLPaymentTotal = pickPrior(prev, 26)
		default:
			slog.Error("metrics: fetch stellar.expert stats failed, reusing prior I25/I26", "error", err)
			m.EURMTLDailyVolume = pickPrior(prev, 25)
			m.EURMTLPaymentTotal = pickPrior(prev, 26)
		}
	}
	done()

	done = stage("MTL_trades_avg")
	{
		stepCtx, cancel := withStepTimeout(ctx)
		if avg, err := s.price.GetAverageTradePrice(stepCtx, mtlAsset, eurmtlAsset, tradesAvgWindow); err != nil {
			slog.Error("metrics: fetch MTL trades-average failed, reusing prior I10", "error", err)
			m.MTLMarketPrice = pickPrior(prev, 10)
		} else {
			m.MTLMarketPrice = ptr(avg.String())
		}
		cancel()
	}
	done()

	done = stage("MTLRECT_trades_avg")
	{
		stepCtx, cancel := withStepTimeout(ctx)
		if avg, err := s.price.GetAverageTradePrice(stepCtx, mtlrectAsset, eurmtlAsset, tradesAvgWindow); err != nil {
			slog.Error("metrics: fetch MTLRECT trades-average failed, reusing prior I49", "error", err)
			m.MTLRECTMarketPrice = pickPrior(prev, 49)
		} else {
			m.MTLRECTMarketPrice = ptr(avg.String())
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

// shareholderStats bundles the two holder counts and the median per-holder
// total derived from a single MTL+MTLRECT walk.
type shareholderStats struct {
	countAtLeastOne int             // I27: holders with combined MTL+MTLRECT ≥ 1
	countAny        int             // I62: holders with any positive combined balance (≥ 1 stroop)
	median          decimal.Decimal // I23: median per-holder combined balance, ≥1 cohort
}

// fetchShareholderStats walks all MTL and MTLRECT holders with any positive
// balance and returns the merged per-account balance map plus counts at two
// thresholds (≥1 for I27 / I23, >0 for I62). The merged map is reused
// downstream to compute I18 (intersection with EURMTL trustline holders).
// Each per-asset sweep gets its own step timeout so the slower side can't
// drag the report past its overall budget; either failure aborts to ok=false
// and the caller falls back to the prior day's persisted I27 / I23 / I18 / I62.
func (s *Service) fetchShareholderStats(ctx context.Context, mtlAsset, mtlrectAsset domain.AssetInfo) (map[string]decimal.Decimal, shareholderStats, bool) {
	minNonZero := decimal.New(1, -7)

	mtlCtx, mtlCancel := withStepTimeout(ctx)
	defer mtlCancel()
	mtl, err := s.horizon.FetchAssetHolderBalancesByBalance(mtlCtx, mtlAsset, minNonZero)
	if err != nil {
		slog.Error("metrics: fetch MTL holders failed, cascade falls I23/I27/I62 (and I18 downstream) to prior", "error", err)
		return nil, shareholderStats{}, false
	}

	mtlrectCtx, mtlrectCancel := withStepTimeout(ctx)
	defer mtlrectCancel()
	mtlrect, err := s.horizon.FetchAssetHolderBalancesByBalance(mtlrectCtx, mtlrectAsset, minNonZero)
	if err != nil {
		slog.Error("metrics: fetch MTLRECT holders failed, cascade falls I23/I27/I62 (and I18 downstream) to prior", "error", err)
		return nil, shareholderStats{}, false
	}

	merged := make(map[string]decimal.Decimal, len(mtl)+len(mtlrect))
	for id, bal := range mtl {
		merged[id] = bal
	}
	for id, bal := range mtlrect {
		merged[id] = merged[id].Add(bal)
	}

	one := decimal.NewFromInt(1)
	atLeastOne := make([]decimal.Decimal, 0, len(merged))
	for _, bal := range merged {
		if bal.GreaterThanOrEqual(one) {
			atLeastOne = append(atLeastOne, bal)
		}
	}
	return merged, shareholderStats{
		countAtLeastOne: len(atLeastOne),
		countAny:        len(merged),
		median:          median(atLeastOne),
	}, true
}

// computeI18 counts shareholders (MTL+MTLRECT sum > 1) who also hold an EURMTL
// trustline with a non-zero balance. Only the EURMTL ID list is fetched here;
// the merged shareholder balances are reused from the prior I27 walk.
func (s *Service) computeI18(ctx context.Context, eurmtlAsset domain.AssetInfo, merged map[string]decimal.Decimal) (int, bool) {
	stepCtx, cancel := withStepTimeout(ctx)
	defer cancel()
	minNonZero := decimal.New(1, -7)
	eurmtlIDs, err := s.horizon.FetchAssetHolderIDsByBalance(stepCtx, eurmtlAsset, minNonZero)
	if err != nil {
		slog.Error("metrics: fetch EURMTL holder IDs failed, I18 falls back to prior", "error", err)
		return 0, false
	}

	// Strictly > 1, not ≥ 1, on purpose: the legacy Python rule was
	// `mtl_mtlrect_balance > 1` (scripts/update_report.py
	// calculate_statistics). I27 uses ≥ 1 because Horizon is queried per asset
	// with minBalance=1 — that's a separate eligibility set. Don't unify them.
	one := decimal.NewFromInt(1)
	eligible := make(map[string]struct{}, len(merged))
	for id, bal := range merged {
		if bal.GreaterThan(one) {
			eligible[id] = struct{}{}
		}
	}

	count := 0
	for _, id := range eurmtlIDs {
		if _, ok := eligible[id]; ok {
			count++
		}
	}
	return count, true
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
