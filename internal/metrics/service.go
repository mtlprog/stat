package metrics

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
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
	FetchDividendActivity(ctx context.Context, distributor string, fundAddresses []string, since time.Time) (horizon.DividendActivity, error)
	FetchAccountDataEntry(ctx context.Context, accountID, key string) (string, bool, error)
}

// dividendLookbackWindow caps how far back the live path scans for the most
// recent dividend event. The fund cadence is monthly; 3 months is generous
// enough that even a multi-cycle gap won't fall through to the sticky-fallback
// path while keeping the per-snapshot Horizon walk bounded.
const dividendLookbackWindow = 90 * 24 * time.Hour

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
	// Subtract 1 to exclude the Secretariat's distribution account (holds MTLAP
	// stock but is not a participant).
	done = stage("MTLAP_holders")
	{
		stepCtx, cancel := withStepTimeout(ctx)
		minOne := decimal.NewFromInt(1)
		if count, err := s.horizon.FetchAssetHolderCountByBalance(stepCtx, mtlapAsset, minOne); err != nil {
			slog.Error("metrics: fetch MTLAP holders failed, reusing prior I40", "error", err)
			m.MTLAPHolders = pickPrior(prev, 40)
		} else {
			m.MTLAPHolders = ptr(decimal.NewFromInt(int64(count - 1)).String())
		}
		cancel()
	}
	done()

	done = stage("MTL_MTLRECT_shareholders_walk")
	_, stats, shareholdersOK := s.fetchShareholderStats(ctx, mtlAsset, mtlrectAsset)
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

	// I11 + I18 are derived from the latest dividend event from the canonical
	// distributor (domain.MTLDividendDistributor) at-or-before `date`. Between
	// events both numbers stay sticky to prior — the indicator changes
	// instantaneously the day a distribution lands, then holds. Per the spec,
	// I18 (recipients) is expected to closely match I27 (≥1-share holders)
	// because the oferta is one-recipient-per-shareholder. The audit only fires
	// when I18 is fresh (came from a real recipient group, not sticky-prior),
	// otherwise yesterday's I18 vs today's I27 would always look mismatched.
	done = stage("dividends_walk")
	{
		i11Str, i18Count, i18Fresh, divOK := s.computeDividendActivity(ctx, date, prev)
		if divOK {
			m.MonthlyDividends = i11Str
			m.EURMTLShareholders = ptr(decimal.NewFromInt(int64(i18Count)).String())
			if i18Fresh {
				s.auditI18VsI27(i18Count, stats.countAtLeastOne, shareholdersOK)
			}
		} else {
			m.MonthlyDividends = pickPrior(prev, 11)
			m.EURMTLShareholders = pickPrior(prev, 18)
		}
	}
	done()

	// I25 (daily) and I26 (cumulative) come from a single call to
	// stellar.expert's pre-aggregated /stats-history. Spec for I25 is
	// "оборот за прошлые сутки" — today's stats-history bucket is a partial
	// running total (00:00 UTC → now), so we always query the previous full
	// UTC day. I26 cumulative shifts in lockstep so the invariant
	// I26[T] = I26[T-1] + I25[T] holds. Falls back to the prior day's
	// persisted values on any failure — including ErrNoDailyEntry, which
	// fires when stellar.expert hasn't ingested yesterday yet.
	done = stage("eurmtl_payment_stats")
	{
		stepCtx, cancel := withStepTimeout(ctx)
		priorDay := date.AddDate(0, 0, -1)
		stats, err := s.expert.FetchEURMTLPaymentStats(stepCtx, priorDay)
		cancel()
		switch {
		case err == nil:
			m.EURMTLDailyVolume = ptr(stats.Daily.String())
			m.EURMTLPaymentTotal = ptr(stats.Cumulative.String())
		case errors.Is(err, stellarexpert.ErrNoDailyEntry):
			slog.Info("metrics: stellar.expert has no entry for prior day yet, reusing persisted I25/I26",
				"prior_day", priorDay.Format("2006-01-02"))
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

// computeDividendActivity derives I11 and I18 from the canonical dividend
// distributor (domain.MTLDividendDistributor):
//
//   - I11 = the value of the distributor's `LAST_DIVS` manage_data entry as of
//     the most recent update at-or-before `date`. Authoritative — published
//     by the fund's bot, includes both raw dividends and adjacent donate flow
//     in one canonical "last distribution amount".
//   - I18 = |distinct non-fund recipients| in the most recent memo-grouped
//     "mtl div ..." payment batch at-or-before `date`. Memos are unique per
//     distribution date so all the txs comprising one batch (Stellar caps at
//     100 ops/tx, big batches split into multiple txs) collapse into one
//     logical event.
//
// Both indicators snap on event and stay sticky between events.
//
// Returns (i11, i18, i18Fresh, ok):
//   - ok=false ⇒ Horizon walk failed; caller sticky-falls back BOTH I11 and I18.
//   - ok=true, i18Fresh=false ⇒ no event in the lookback window; both values
//     reflect prior (sticky). Caller writes them but must NOT trip the audit
//     comparison against today's live I27.
//   - ok=true, i18Fresh=true ⇒ a recipient group was found ≤ date; I18 reflects
//     today's reality and the audit may compare it against I27.
//
// `date` matches the snapshot policy (midnight UTC of the report day); events
// dated up to and including that UTC day count for the snapshot.
func (s *Service) computeDividendActivity(ctx context.Context, date time.Time, prev map[int]indicator.Indicator) (*string, int, bool, bool) {
	stepCtx, cancel := withStepTimeout(ctx)
	defer cancel()
	since := date.Add(-dividendLookbackWindow)
	activity, err := s.horizon.FetchDividendActivity(stepCtx, domain.MTLDividendDistributor, s.fundAddrs, since)
	if err != nil {
		slog.Error("metrics: fetch dividend activity failed, I11 and I18 fall back to prior", "error", err)
		return nil, 0, false, false
	}

	cutoff := date.AddDate(0, 0, 1) // include events on the same UTC day as the snapshot

	var latestUpdate *horizon.LastDivsUpdate
	for i := range activity.LastDivsUpdates {
		if activity.LastDivsUpdates[i].TS.Before(cutoff) {
			latestUpdate = &activity.LastDivsUpdates[i]
		}
	}
	var latestGroup *horizon.RecipientGroup
	for i := range activity.RecipientGroups {
		if activity.RecipientGroups[i].TS.Before(cutoff) {
			latestGroup = &activity.RecipientGroups[i]
		}
	}

	// Live read of `account.data["LAST_DIVS"]` is a fallback for the case where
	// the lookback window misses the most recent update (e.g. distribution
	// older than dividendLookbackWindow). It does not replace the walk —
	// the walk is still needed for the recipient-group history.
	if latestUpdate == nil {
		liveCtx, liveCancel := withStepTimeout(ctx)
		raw, present, derr := s.horizon.FetchAccountDataEntry(liveCtx, domain.MTLDividendDistributor, "LAST_DIVS")
		liveCancel()
		switch {
		case derr != nil:
			slog.Error("metrics: live read of LAST_DIVS data entry failed, I11 falls back to prior",
				"account", domain.MTLDividendDistributor, "error", derr)
		case !present:
			slog.Info("metrics: distributor account has no LAST_DIVS data entry, I11 falls back to prior",
				"account", domain.MTLDividendDistributor)
		default:
			v, perr := decimal.NewFromString(strings.TrimSpace(raw))
			if perr != nil {
				slog.Error("metrics: live LAST_DIVS data entry not numeric, I11 falls back to prior",
					"account", domain.MTLDividendDistributor, "raw", raw, "error", perr)
			} else {
				return ptr(v.String()), recipientCountOrPrior(latestGroup, prev), latestGroup != nil, true
			}
		}
	}

	i11 := pickPrior(prev, 11)
	if latestUpdate != nil {
		i11 = ptr(latestUpdate.Value.String())
	}
	if latestUpdate == nil && latestGroup == nil {
		slog.Info("metrics: no dividend activity within lookback window, I11/I18 sticky to prior",
			"lookback_days", int(dividendLookbackWindow.Hours()/24))
	}
	return i11, recipientCountOrPrior(latestGroup, prev), latestGroup != nil, true
}

func recipientCountOrPrior(group *horizon.RecipientGroup, prev map[int]indicator.Indicator) int {
	if group == nil {
		return pickPriorInt(prev, 18)
	}
	return len(group.Recipients)
}

// pickPriorInt is a small helper for callers that want the prior value of an
// integer-valued indicator (counts) in int form, falling back to zero if the
// pickPriorInt is the int-typed analogue of pickPrior for count indicators.
func pickPriorInt(prev map[int]indicator.Indicator, id int) int {
	if prev == nil {
		return 0
	}
	ind, ok := prev[id]
	if !ok {
		return 0
	}
	return int(ind.Value.IntPart())
}

// auditI18VsI27 emits an Info-level audit log when I18 (dividend recipients)
// diverges from I27 (≥1-share holders) by more than 5% — per the spec, оферта
// guarantees these two should match. Divergence is a symmetric business signal
// (recipients < shareholders → missed payouts; recipients > shareholders →
// payouts to non-shareholders), so the log is also symmetric. Skips when
// shareholder stats are unavailable (i27OK=false) or population is too small
// to be meaningful.
const i18AuditDivergenceThreshold = 0.05
const i18AuditMinPopulation = 5

func (s *Service) auditI18VsI27(i18, i27 int, i27OK bool) {
	if !i27OK {
		return
	}
	maxV := i18
	if i27 > maxV {
		maxV = i27
	}
	if maxV < i18AuditMinPopulation {
		return
	}
	diff := i18 - i27
	if diff < 0 {
		diff = -diff
	}
	if float64(diff)/float64(maxV) > i18AuditDivergenceThreshold {
		slog.Info("metrics: I18 diverges from I27 by >5% — recipients vs shareholders mismatch",
			"i18", i18, "i27", i27, "abs_diff", diff)
	}
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
