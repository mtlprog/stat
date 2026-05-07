# Indicators — spec vs implementation

Source of truth for this audit: the indicator table provided by the product owner (2026-05-07).
Code state surveyed against `internal/indicator/{indicator,layer0,layer1,layer2,dividend,analytics,tokenomics}.go`
and `internal/metrics/service.go`.

Legend:
- ✅ done — implemented and matches the spec
- ⚠️ done, mismatch — implemented but the formula / scope / wording diverges from the spec
- ❌ missing — required by the spec, not implemented
- 🗑️ deprecated — present in code but not in the spec; flagged for removal
- ➖ table-only deprecation — spec lists it but explicitly out of scope (MFBond)

---

## Status by indicator

| ID  | Name                          | Spec formula / source                | Status | Code locus                                                | Notes |
| --- | ----------------------------- | ------------------------------------ | ------ | --------------------------------------------------------- | ----- |
| I1  | Market Cap EUR                | I5 × I10                             | ✅     | `layer2.go` (deps I3, I5, I10, I61)                       |       |
| I2  | Market Cap BTC                | I1 / I61                             | ✅     | `layer2.go`                                               |       |
| I3  | Assets Value MTLF             | I51+I52+I53+I58+I59+I60              | ✅     | `layer1.go`                                               |       |
| I4  | Operating Balance             | Σ(EURMTL+XLM) over subfunds          | ✅     | `layer1.go::calculateOperatingBalance` (only `AccountTypeSubfond`) |       |
| I5  | Shares                        | I6 + I7                              | ✅     | `layer1.go`                                               |       |
| I6  | MTL in circulation            | live (paper emission − pool balance) | ✅     | `metrics.service.go` → `LiveMetrics.MTLCirculation`       |       |
| I7  | MTLRECT in circulation        | live (paper emission − pool balance) | ✅     | `metrics.service.go` → `LiveMetrics.MTLRECTCirculation`   |       |
| I8  | Share Book Value              | I3 / I5                              | ✅     | `layer2.go`                                               |       |
| I10 | Share Market Price            | VWAP last ≤100 trades MTL/EURMTL DEX | ✅     | populated upstream into `LiveMetrics.MTLMarketPrice`      |       |
| I11 | Dividends                     | EURMTL paid out, last month          | ✅     | `dividend.go` (chain over snapshots)                      |       |
| I15 | Dividends per share           | I11 / I5                             | ✅     | `dividend.go`                                             |       |
| I17 | Annual Dividend Yield 2       | (I54 / I55) × 100                    | ✅     | `dividend.go`                                             |       |
| I18 | Shareholders by EURMTL        | recipients of last-month divs        | ✅     | `tokenomics.go` (live)                                    |       |
| I21 | Average Shareholding          | I5 / I27                             | ✅     | `tokenomics.go`                                           |       |
| I22 | Average Share Price           | I1 / I27                             | ✅     | `tokenomics.go`                                           |       |
| I23 | Median shareholding size      | median MTL+MTLRECT per holder        | ✅     | `tokenomics.go` (live, `MTLShareholdersMedian`)           |       |
| I24 | Tokenomics participants       | EURMTL trustlines, nonzero balance   | ✅     | `tokenomics.go` (live)                                    |       |
| I25 | EURMTL daily payment volume   | last-day Stellar payments            | ✅     | `tokenomics.go` (live, `EURMTLDailyVolume`)               |       |
| I26 | EURMTL overall payment total  | cumulative tokenomics turnover       | ✅     | `metrics.service.go` reads from `stellarexpert` /stats-history (cumulative running sum) | |
| I27 | More-one-share Shareholders   | accounts with ≥1 MTL or MTLRECT      | ✅     | `metrics.service.go::fetchShareholderStats` (≥1)          | Registry description in `indicator.go:60` says "более 1" — pure copy bug, fix the string to "не менее 1" |
| I30 | Price-to-book ratio           | I10 / I8                             | ✅     | `layer2.go`                                               |       |
| I34 | P/E                           | I10 / I54                            | ✅     | `dividend.go`                                             |       |
| I39 | Bitcoin purchase price        | EURMTL spent on BTC / BTCMTL held    | ❌     | —                                                         | See Q2 |
| I43 | Total ROI                     | ((I10 − I55) + I54) / I55            | ✅     | `analytics.go`                                            |       |
| I49 | MTLRECT Market Price          | VWAP last 100 trades MTLRECT/EURMTL  | ✅     | `layer1.go` (live, `MTLRECTMarketPrice`)                  |       |
| I50 | MTL and MTLRECT diff          | control: divergence MTL vs MTLRECT   | ❌     | —                                                         | Spec itself marks as "уточняется"; see Q3 |
| I51 | Assets Value DEFI             | sum subfund DEFI                     | ✅     | `layer0.go`                                               |       |
| I52 | Assets Value MCITY            | sum subfund MCITY                    | ✅     | `layer0.go`                                               |       |
| I53 | Assets Value MABIZ            | sum subfund MABIZ                    | ✅     | `layer0.go`                                               |       |
| I54 | Annual Dividends per share    | Σ I15 last 12 months                 | ✅     | `dividend.go`                                             |       |
| I55 | Share Market Price Year ago   | I10 a year ago                       | ✅     | `dividend.go` (via `HistoricalData.IndicatorRepo`)        |       |
| I56 | Assets Value MFApart          | sum ПИФ MFApart                      | ✅     | `layer0.go`                                               |       |
| I57 | Assets Value MFBond           | sum ПИФ MFBond                       | ➖     | already absent from code/registry                         | User note: deprecated in spec, no action needed in code |
| I58 | Free Assets Value MTLF Issuer | issuer free balance                  | ✅     | `layer0.go`                                               |       |
| I59 | Assets Value BOSS             | sum BOSS account                     | ✅     | `layer0.go`                                               |       |
| I60 | Assets Value ADMIN            | sum ADMIN account                    | ✅     | `layer0.go`                                               |       |
| I61 | Bitcoin rate                  | global BTC/EUR rate                  | ✅     | `layer0.go` (via CoinGecko-backed price service)          |       |
| I62 | Shareholders                  | accounts with nonzero MTL or MTLRECT | ❌     | —                                                         | Distinct from I27 (≥1 vs >0). See Q4 |

---

## Open questions (need answers before implementing the ❌/⚠️ rows)

### Q1. I25 / I26 — daily and cumulative EURMTL payment volume

**Status (2026-05-07):** both indicators are sourced from stellar.expert's
`/explorer/public/asset/EURMTL-…-2/stats-history` endpoint, which exposes a
pre-aggregated daily breakdown (`payments_amount` in stroops, sorted ascending
by `ts`).

- I25 = today's row's `payments_amount / 10⁷`
- I26 = running sum of all `payments_amount` up to and including today / 10⁷

The endpoint is hit once per `stat report` run via `internal/stellarexpert`;
no Horizon `/payments` pagination, no separate seed/backfill, no in-repo
delta math. If stellar.expert hasn't ingested today yet, the client returns
`ErrNoDailyEntry` and metrics sticky-fallback to yesterday's persisted values.

The full historical I25/I26 table in `fund_indicators` was rewritten from
this same endpoint on 2026-05-07 (3548 rows; sum-of-I25 = 28 538 906.3718
matches the cumulative figure shown on stellar.expert's UI).

### Q2. I39 — Bitcoin purchase price

Formula: «Сумма EURMTL, потраченная на закуп BTC, делённая на количество BTCMTL на балансах
Фонда».

- **Source for the numerator (EURMTL spent):**
  - All EURMTL outflows from fund accounts that net-resulted in BTCMTL inflow within the same
    Stellar transaction / path-payment? Or all EURMTL→BTCMTL on-DEX trades by fund accounts?
  - If the fund acquires BTCMTL via path-payments through XLM or other intermediates, does the
    cost basis count the EURMTL leg or the realized exchange-rate-equivalent at trade time?
  - Time scope: cumulative since first BTC purchase, or reset whenever the fund's BTCMTL
    balance reaches zero (true average-cost-basis)?
- **Source for the denominator (BTCMTL balance):** which fund accounts hold BTC — only BOSS,
  or every subfund? Confirm the asset is `BTCMTL` (issuer?) and not native Stellar `BTC`.
- **Sells:** when the fund sells BTCMTL, do we (a) leave the cumulative cost numerator alone
  (FIFO/LIFO accounting), (b) reduce it pro-rata by the fraction of BTCMTL sold, or (c) reset
  to the last-purchase-only basis?

Пока не делать, там сложно. Пометь, что TODO, нужно больше информации.

### Q3. I50 — MTL/MTLRECT divergence

The spec explicitly says «уточняется». We need a concrete definition before coding:

- What variables to compare — market price (I10 vs I49), book-value-equivalents, supply
  ratios, or all three combined into a composite?
- What metric — absolute diff, percentage diff `|I10−I49|/I10`, ratio `I10/I49`, or a discrete
  flag (`OK` / `WARN` / `CRITICAL`) with thresholds?
- Should it be a number, a string label, or both? Consider: this is the only "control"
  indicator and the rest of the pipeline assumes a `decimal.Decimal` value.

Точно так же, как и с I39

### Q4. I62 — Shareholders (any nonzero balance)

I62's spec («ненулевым количеством») contrasts with I27's «не менее одного» — the natural
reading is that I62 counts accounts with `0 < balance` while I27 counts `balance ≥ 1`.

- Confirm: I62 = union of MTL-holders ∪ MTLRECT-holders where balance > 0 (Stellar stroop
  precision, so >= `decimal.New(1, -7)`). I27 stays as `balance >= 1`.
- Re-using `fetchShareholderStats` requires walking the holders list with a different
  threshold; is it OK to extend that function to emit both counts in one Horizon walk
  (preferred — single sweep, one extra integer in `LiveMetrics`) instead of duplicating it?
- Display precision: integer (`Precision: 0`)?
- MONITORING column: this column does not exist in `monitoringColumns` — should we add a new
  column at the end of the sheet, or repurpose an existing unmapped position?

Confirm, balances > 0.

Precision 0

Should add.

### Q5. I27 description fix (cosmetic, no code logic change)

`internal/indicator/indicator.go:60` describes I27 as «более 1 MTL или MTLRECT». The actual
metric (and the spec) is «не менее 1». Confirm the fix to «Число Stellar-аккаунтов, на
которых не менее 1 MTL или MTLRECT»; also rename the registry `Name` from
`"MTL Shareholders (>=1)"` to match the spec's `"More-one-share Shareholders"`?

Yes

---

## Deprecations — present in code, absent from spec (proposed for removal)

These IDs are registered in `indicatorRegistry` and computed by the production pipeline, but
do not appear in the supplied table. Pending product confirmation, plan to remove all
references (registry entry, calculator math, MONITORING column mapping if any, DB rows are
left in place — read-only history is preserved by the schema, but new writes stop).

| ID  | Name                       | Calculator        | Removal touchpoints |
| --- | -------------------------- | ----------------- | ------------------- |
| I16 | Annual Dividend Yield 1    | `dividend.go`     | registry, `DividendCalculator.IDs()`, MONITORING mapping (if present) |
| I33 | Earnings Per Share         | `dividend.go`     | registry, `DividendCalculator.IDs()` and Calculate output, downstream consumers |
| I40 | Association Participants   | `tokenomics.go`   | registry, `TokenomicsCalculator.IDs()`, `metrics.service.go` MTLAP fetch (the entire MTLAP holders flow), `LiveMetrics.MTLAPHolders` field |
| I44 | Beta                       | `analytics.go`    | registry, whole `AnalyticsCalculator` may collapse |
| I45 | Sharpe Ratio               | `analytics.go`    | "                                                  |
| I46 | Sortino Ratio              | `analytics.go`    | "                                                  |
| I47 | Value at Risk              | `analytics.go`    | "                                                  |
| I48 | Dividend/Book Value        | `analytics.go`    | "                                                  |

After removing I44–I48, `AnalyticsCalculator` is left only with I43 (Total ROI). Either:
- (a) move I43 into `DividendCalculator` (it already depends on I54/I55) and delete the
  analytics file outright, or
- (b) keep `AnalyticsCalculator` as a single-output home for I43.

Recommend (a) — fewer files, fewer registrations, and I43's deps overlap perfectly with the
dividend chain.

a

### Other cleanup uncovered while auditing

- `internal/indicator/deterministic.go::DeterministicIDs` includes the strict subset used by
  `stat backfill-indicators`. After deprecations, the set is unaffected (none of I16, I33,
  I40, I44–I48 are deterministic) — verify and update the comment if the list narrows.
- `internal/export/monitoring.go` `monitoringColumns` slice: any deprecated ID that maps to a
  column needs the slot zeroed (or the column removed if product confirms the sheet should
  drop it). Column order is load-bearing — see CLAUDE.md.
- `stat import-indicators-from-sheets` reads MONITORING for IDs in `monitoringColumns`. If a
  column is dropped from the sheet, the importer's mapping must follow.

---

## Summary

- **Implemented and correct:** 33 indicators (I1–I8, I10, I11, I15, I17, I18, I21–I25, I27,
  I30, I34, I43, I49, I51–I56, I58–I61).
- **Implemented with mismatch:** 1 (I26 — 30d rolling vs spec's cumulative).
- **Cosmetic doc bug:** 1 (I27 description string).
- **Missing from code:** 3 (I39, I50, I62).
- **In code but not in spec — flagged for deletion:** 8 (I16, I33, I40, I44–I48).
- **Spec-listed but explicitly deprecated:** 1 (I57 MFBond — already absent from code).
