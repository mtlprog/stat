# Indicators — spec vs implementation

Source of truth: the indicator table provided by the product owner (2026-05-07).
Code state surveyed against `internal/indicator/{indicator,layer0,layer1,layer2,dividend,tokenomics}.go`,
`internal/metrics/service.go`, and `internal/stellarexpert/client.go`.

Legend:
- ✅ done — implemented and matches the spec
- ❌ TODO — required by the spec, not implemented (needs more product input)
- ➖ table-only deprecation — spec lists it but explicitly out of scope (MFBond)

---

## Status by indicator

| ID  | Name                          | Spec formula / source                | Status | Code locus                                                |
| --- | ----------------------------- | ------------------------------------ | ------ | --------------------------------------------------------- |
| I1  | Market Cap EUR                | I5 × I10                             | ✅     | `layer2.go` (deps I3, I5, I10, I61)                       |
| I2  | Market Cap BTC                | I1 / I61                             | ✅     | `layer2.go`                                               |
| I3  | Assets Value MTLF             | I51+I52+I53+I58+I59+I60              | ✅     | `layer1.go`                                               |
| I4  | Operating Balance             | Σ(EURMTL+XLM) over subfunds          | ✅     | `layer1.go::calculateOperatingBalance` (subfunds only)    |
| I5  | Shares                        | I6 + I7                              | ✅     | `layer1.go`                                               |
| I6  | MTL in circulation            | live (paper emission − pool balance) | ✅     | `metrics/service.go` → `LiveMetrics.MTLCirculation`       |
| I7  | MTLRECT in circulation        | live (paper emission − pool balance) | ✅     | `metrics/service.go` → `LiveMetrics.MTLRECTCirculation`   |
| I8  | Share Book Value              | I3 / I5                              | ✅     | `layer2.go`                                               |
| I10 | Share Market Price            | VWAP last ≤100 trades MTL/EURMTL DEX | ✅     | `LiveMetrics.MTLMarketPrice` (populated upstream)         |
| I11 | Dividends                     | EURMTL paid out, last month          | ✅     | `dividend.go` (chain over snapshots + indicator history)  |
| I15 | Dividends per share           | I11 / I5                             | ✅     | `dividend.go`                                             |
| I17 | Annual Dividend Yield 2       | (I54 / I55) × 100                    | ✅     | `dividend.go`                                             |
| I18 | Shareholders by EURMTL        | recipients of last-month divs        | ✅     | `tokenomics.go` (live)                                    |
| I21 | Average Shareholding          | I5 / I27                             | ✅     | `tokenomics.go`                                           |
| I22 | Average Share Price           | I1 / I27                             | ✅     | `tokenomics.go`                                           |
| I23 | Median shareholding size      | median MTL+MTLRECT per holder        | ✅     | `tokenomics.go` (live, `MTLShareholdersMedian`)           |
| I24 | Tokenomics participants       | EURMTL trustlines, nonzero balance   | ✅     | `tokenomics.go` (live)                                    |
| I25 | EURMTL daily payment volume   | last-day Stellar payments            | ✅     | sourced from `stellarexpert` /stats-history (per-day row) |
| I26 | EURMTL overall payment total  | cumulative tokenomics turnover       | ✅     | sourced from `stellarexpert` /stats-history (running sum) |
| I27 | More-one-share Shareholders   | accounts with ≥1 MTL or MTLRECT      | ✅     | `metrics/service.go::fetchShareholderStats` (≥1 cohort)   |
| I30 | Price-to-book ratio           | I10 / I8                             | ✅     | `layer2.go`                                               |
| I34 | P/E                           | I10 / I54                            | ✅     | `dividend.go`                                             |
| I39 | Bitcoin purchase price        | EURMTL spent on BTC / BTCMTL held    | ❌     | TODO — see Q1                                             |
| I40 | Association Participants      | MTLAP holders with balance ≥1        | ✅     | `metrics/service.go` (live, `MTLAPHolders`)               |
| I43 | Total ROI                     | ((I10 − I55) + I54) / I55            | ✅     | `dividend.go` (folded in from the deleted analytics calc) |
| I49 | MTLRECT Market Price          | VWAP last 100 trades MTLRECT/EURMTL  | ✅     | `layer1.go` (live, `MTLRECTMarketPrice`)                  |
| I50 | MTL and MTLRECT diff          | control: divergence MTL vs MTLRECT   | ❌     | TODO — see Q2                                             |
| I51 | Assets Value DEFI             | sum subfund DEFI                     | ✅     | `layer0.go`                                               |
| I52 | Assets Value MCITY            | sum subfund MCITY                    | ✅     | `layer0.go`                                               |
| I53 | Assets Value MABIZ            | sum subfund MABIZ                    | ✅     | `layer0.go`                                               |
| I54 | Annual Dividends per share    | Σ I15 last 12 months                 | ✅     | `dividend.go`                                             |
| I55 | Share Market Price Year ago   | I10 a year ago                       | ✅     | `dividend.go` (snapshot → indicator-repo fallback)        |
| I56 | Assets Value MFApart          | sum ПИФ MFApart                      | ✅     | `layer0.go`                                               |
| I57 | Assets Value MFBond           | sum ПИФ MFBond                       | ➖     | absent from code/registry; deprecated in spec             |
| I58 | Free Assets Value MTLF Issuer | issuer free balance                  | ✅     | `layer0.go`                                               |
| I59 | Assets Value BOSS             | sum BOSS account                     | ✅     | `layer0.go`                                               |
| I60 | Assets Value ADMIN            | sum ADMIN account                    | ✅     | `layer0.go`                                               |
| I61 | Bitcoin rate                  | global BTC/EUR rate                  | ✅     | `layer0.go` (CoinGecko-backed price service)              |
| I62 | Shareholders                  | accounts with nonzero MTL or MTLRECT | ✅     | `metrics/service.go::fetchShareholderStats` (>0 cohort)   |

---

## Open TODOs (need more product input)

### Q1. I39 — Bitcoin purchase price

Formula: «Сумма EURMTL, потраченная на закуп BTC, делённая на количество BTCMTL на балансах
Фонда».

Open questions before implementation:

- **Numerator (EURMTL spent):** all EURMTL outflows that net-resulted in BTCMTL inflow within
  the same Stellar transaction / path-payment, or all on-DEX EURMTL→BTCMTL trades by fund
  accounts? When BTCMTL is acquired via path-payment through XLM, does the cost basis count
  the EURMTL leg or the realized exchange-rate-equivalent at trade time?
- **Time scope:** cumulative since first BTC purchase, or reset whenever the fund's BTCMTL
  balance reaches zero (true average-cost-basis)?
- **Denominator (BTCMTL balance):** which fund accounts hold BTC — only BOSS, or every
  subfund? Confirm the asset is `BTCMTL` (which issuer?) and not native Stellar `BTC`.
- **Sells:** when the fund sells BTCMTL, do we (a) leave the cumulative cost numerator alone
  (FIFO/LIFO), (b) reduce it pro-rata by the fraction of BTCMTL sold, or (c) reset to the
  last-purchase-only basis?

Status: deferred per product owner — "там сложно, нужно больше информации".

### Q2. I50 — MTL/MTLRECT divergence

The spec itself marks this as «уточняется». A concrete definition is required before coding:

- What variables to compare — market price (I10 vs I49), book-value-equivalents, supply
  ratios, or all three combined into a composite?
- What metric — absolute diff, percentage diff `|I10−I49|/I10`, ratio `I10/I49`, or a discrete
  flag (`OK` / `WARN` / `CRITICAL`) with thresholds?
- Should the value be a number, a string label, or both? The rest of the pipeline assumes
  `decimal.Decimal`; a string-tagged status would need a separate sink.

Status: deferred per product owner — "точно так же, как и с I39".

---

## Decision log

These decisions were ratified by the product owner during the spec audit; the code already
reflects them.

- **I26 redefinition.** Previously a 30-day rolling window via Horizon `/payments`
  pagination; now the cumulative-since-genesis EURMTL payment total. Source switched to
  stellar.expert `/stats-history` (one HTTP GET per `stat report` instead of paginating
  thousands of /payments pages). The full historical `fund_indicators` table for I25 and I26
  was rewritten from `/stats-history` on 2026-05-07; sum of all daily I25 = max I26 =
  `28 538 906.3718166` matches stellar.expert's own cumulative figure.
- **I27 metadata fix.** Renamed to "More-one-share Shareholders" with description
  "Число Stellar-аккаунтов, на которых не менее 1 MTL или MTLRECT". Computation logic was
  always `≥1`; only the registry strings were misleading.
- **I62 introduced.** Distinct from I27: counts accounts with any positive MTL+MTLRECT
  balance (`>0`, i.e. ≥1 stroop). Computed alongside I27 in a single `fetchShareholderStats`
  walk — no extra Horizon round-trip. Display precision 0 (integer), new MONITORING column
  appended at the end (the sheet grew from 40 to 41 data columns).
- **I43 home.** Moved from the now-deleted `AnalyticsCalculator` into `DividendCalculator`
  (its dependencies — I54, I55 — already lived there).
- **Deprecated and removed.** I16 (ADY1 median-based), I33 (EPS), I44 (Beta), I45 (Sharpe),
  I46 (Sortino), I47 (VaR), I48 (D/BV). All registry entries gone; calculator code gone;
  MONITORING column slots zeroed (column order is load-bearing — see `CLAUDE.md`);
  historical `fund_indicators` rows for these IDs are left untouched (read-only history).
- **Source contract for I25 / I26.** `internal/stellarexpert.Client.FetchEURMTLPaymentStats`
  is a single GET to `/explorer/public/asset/EURMTL-…-2/stats-history`. Daily I25 is the
  exact `payments_amount` row whose `ts` matches `date` (UTC midnight); cumulative I26 is the
  running sum of `payments_amount` up to and including that row. Both are converted from
  stroops via `decimal.Shift(-7)`. When stellar.expert hasn't yet ingested the requested
  date, the client returns `ErrNoDailyEntry` and the metrics service sticky-falls back to
  yesterday's persisted values; any other error logs at Error and also sticky-falls back.

---

## Refresh recipe

If I26's seed ever needs to be re-checked against external truth:

```
curl https://api.stellar.expert/explorer/public/asset/EURMTL-GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V-2 \
  | jq '.payments_amount'
# divide by 10^7 → cumulative EURMTL — should equal max(I26) in fund_indicators
```

The same endpoint, plus `/stats-history`, is what `stat report` consumes daily.
