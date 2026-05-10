# Indicators — spec vs implementation

Source of truth: indicator table provided by the product owner (2026-05-07).
Code state surveyed against `internal/indicator/{indicator,layer0,layer1,layer2,dividend,tokenomics,bpp}.go`,
`internal/metrics/service.go`, `internal/stellarexpert/client.go`.

This document is written for independent audit: each indicator's formula and primary
data source are explicit enough to recompute the value from public sources (Stellar
Horizon, stellar.expert, CoinGecko, the `fund_indicators` table) without reading the
calculator code.

## Conventions

- Stellar amounts are stroops (1 unit = 10⁻⁷ asset). Convert with `decimal.Shift(-7)`.
- "VWAP last ≤100 trades" = volume-weighted average price over the last *N* trades from
  Horizon `/trades?base_asset_…=…&counter_asset_…=…&order=desc&limit=100`, rounded to 7 dp.
- `EURMTL` = `EURMTL-GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V` (issuer in `domain.IssuerAddress`).
- "Subfund / fund accounts" = the 11 accounts in `domain.AccountRegistry()` (DEFI, MCITY, MABIZ, MFApart, BOSS, ADMIN, issuer, …).
- "Holders" cohorts come from the union of `/accounts?asset=MTL-…` and `/accounts?asset=MTLRECT-…`, deduplicated by account ID.
- Derived values (those listed as a pure formula `Ix … Iy`) are computed from other indicators in the same snapshot — verify by recomputing the formula from the indicator values you read independently.

## Implementation table

| ID  | Name                          | Formula                                                                | Source / verification                                                       | Code                                                       |
| --- | ----------------------------- | ---------------------------------------------------------------------- | --------------------------------------------------------------------------- | ---------------------------------------------------------- |
| I1  | Market Cap EUR                | `I5 × I10`                                                             | derived                                                                     | `layer2.go`                                                |
| I2  | Market Cap BTC                | `I1 / I61`                                                             | derived                                                                     | `layer2.go`                                                |
| I3  | Assets Value MTLF             | `I51 + I52 + I53 + I58 + I59 + I60`                                    | derived                                                                     | `layer1.go`                                                |
| I4  | Operating Balance             | `Σ subfund (EURMTL + XLM)`                                             | Horizon `/accounts/{id}` balances over `domain.AccountRegistry()` subfunds  | `layer1.go::calculateOperatingBalance`                     |
| I5  | Shares                        | `I6 + I7`                                                              | derived                                                                     | `layer1.go`                                                |
| I6  | MTL in circulation            | `paper emission − exchange-pool MTL balance`                           | Horizon: emission account holdings minus liquidity-pool MTL                 | `metrics/service.go::LiveMetrics.MTLCirculation`           |
| I7  | MTLRECT in circulation        | `paper emission − exchange-pool MTLRECT balance`                       | same, for MTLRECT                                                           | `metrics/service.go::LiveMetrics.MTLRECTCirculation`       |
| I8  | Share Book Value              | `I3 / I5`                                                              | derived                                                                     | `layer2.go`                                                |
| I10 | Share Market Price            | VWAP of last ≤100 trades MTL ↔ EURMTL on Stellar DEX                   | Horizon `/trades` for the MTL/EURMTL pair                                   | `LiveMetrics.MTLMarketPrice` (populated upstream)          |
| I11 | Dividends                     | Σ EURMTL dividend payments over the last calendar month                | chain over `fund_snapshots` and `fund_indicators` history                   | `dividend.go`                                              |
| I15 | Dividends per share           | `I11 / I5`                                                             | derived                                                                     | `dividend.go`                                              |
| I17 | Annual Dividend Yield 2       | `(I54 / I55) × 100`  (percent)                                         | derived                                                                     | `dividend.go`                                              |
| I18 | Shareholders by EURMTL        | count of distinct recipient accounts of last-month dividend payments   | Horizon `/payments`; per spec coincides with I27                            | `tokenomics.go`                                            |
| I21 | Average Shareholding          | `I5 / I27`                                                             | derived                                                                     | `tokenomics.go`                                            |
| I22 | Average Share Price           | `I1 / I27`                                                             | derived                                                                     | `tokenomics.go`                                            |
| I23 | Median shareholding size      | `median(MTL + MTLRECT)` over holders                                   | Horizon, union of MTL ∪ MTLRECT holders                                     | `tokenomics.go::MTLShareholdersMedian`                     |
| I24 | Tokenomics participants       | count(accounts with EURMTL trustline AND balance > 0)                  | Horizon `/accounts?asset=EURMTL-…`                                          | `tokenomics.go`                                            |
| I25 | EURMTL daily payment volume   | `payments_amount` for the last full UTC day                            | stellar.expert `/stats-history` (single GET, see contract below)            | `stellarexpert.Client.FetchEURMTLPaymentStats`             |
| I26 | EURMTL overall payment total  | running Σ `payments_amount` since genesis                              | same endpoint, cumulative                                                   | same                                                       |
| I27 | More-one-share Shareholders   | count(accounts with `MTL + MTLRECT ≥ 1`)                               | Horizon, union of MTL ∪ MTLRECT holders, threshold ≥ 1 token                | `metrics/service.go::fetchShareholderStats` (≥1 cohort)    |
| I30 | Price-to-book ratio           | `I10 / I8`                                                             | derived                                                                     | `layer2.go`                                                |
| I34 | P/E                           | `I10 / I54`                                                            | derived                                                                     | `dividend.go`                                              |
| I39 | Bitcoin purchase price        | manual constant `bppValue` (currently `24000`)                         | edit constant + redeploy; real formula deferred — see Q1                    | `bpp.go`                                                   |
| I40 | Association Participants      | count(accounts with `MTLAP ≥ 1`)                                       | Horizon `/accounts?asset=MTLAP-…`                                           | `metrics/service.go` (`MTLAPHolders`)                      |
| I43 | Total ROI                     | `((I10 − I55) + I54) / I55`                                            | derived                                                                     | `dividend.go`                                              |
| I49 | MTLRECT Market Price          | VWAP of last ≤100 trades MTLRECT ↔ EURMTL on Stellar DEX               | Horizon `/trades` for MTLRECT/EURMTL                                        | `layer1.go::MTLRECTMarketPrice`                            |
| I51 | Assets Value DEFI             | `Σ (balance × token price in EURMTL)` over DEFI subfund accounts       | Horizon balances + `price.Service` (orderbook / pathfinding)                | `layer0.go`                                                |
| I52 | Assets Value MCITY            | same, MCITY accounts                                                   | same                                                                        | `layer0.go`                                                |
| I53 | Assets Value MABIZ            | same, MABIZ accounts                                                   | same                                                                        | `layer0.go`                                                |
| I54 | Annual Dividends per share    | `Σ I15` over trailing 12 calendar months                               | `fund_indicators` history                                                   | `dividend.go`                                              |
| I55 | Share Market Price Year ago   | `I10` as of `today − 365d`                                             | snapshot match → `fund_indicators` nearest-before fallback                  | `dividend.go`                                              |
| I56 | Assets Value MFApart          | sum, MFApart subfund accounts                                          | as I51                                                                      | `layer0.go`                                                |
| I58 | Free Assets Value MTLF Issuer | issuer-account balance not packaged into any subfund                   | Horizon balances on `domain.IssuerAddress`                                  | `layer0.go`                                                |
| I59 | Assets Value BOSS             | sum, BOSS account                                                      | as I51                                                                      | `layer0.go`                                                |
| I60 | Assets Value ADMIN            | sum, ADMIN account                                                     | as I51                                                                      | `layer0.go`                                                |
| I61 | Bitcoin rate                  | global BTC/EUR rate                                                    | CoinGecko (`price.Service`)                                                 | `layer0.go`                                                |
| I62 | Shareholders                  | count(accounts with `MTL + MTLRECT > 0`, i.e. ≥ 1 stroop)              | Horizon, MTL ∪ MTLRECT, no minimum-pack threshold                           | `metrics/service.go::fetchShareholderStats` (>0 cohort)    |

## Out of scope

| ID  | Name              | Reason                                                                              |
| --- | ----------------- | ----------------------------------------------------------------------------------- |
| I57 | Assets Value MFBond | Deprecated by product owner. Not in registry, not computed, not exported.         |

Indicators removed from the calculator entirely: **I16 (ADY1), I33 (EPS), I44 (Beta),
I45 (Sharpe), I46 (Sortino), I47 (VaR), I48 (D/BV)**. Historical `fund_indicators` rows
for these IDs are left untouched (read-only history). The MONITORING sheet keeps their
column slots zeroed because column order is load-bearing.

## Deferred (need product input before coding)

### Q1. I39 — Bitcoin purchase price (real formula)

Shipped as a manually-managed constant in `internal/indicator/bpp.go` (`bppValue`). Edit
the constant and redeploy when product wants the value bumped; existing `fund_indicators`
rows are intentionally **not** rewritten on changes (history is frozen at whatever value
was current on each snapshot date).

The eventual real formula per the spec — «Сумма EURMTL, потраченная на закуп BTC,
делённая на количество BTCMTL на балансах Фонда» — is deferred («там сложно, нужно
больше информации»). Open questions:

- **Numerator (EURMTL spent):** all EURMTL outflows that net-resulted in BTCMTL inflow
  inside the same Stellar transaction / path-payment, or only direct on-DEX
  EURMTL→BTCMTL trades by fund accounts? When BTCMTL is acquired via path-payment
  through XLM, does the cost basis count the EURMTL leg or the realized
  exchange-rate-equivalent at trade time?
- **Time scope:** cumulative since first BTC purchase, or reset whenever the fund's
  BTCMTL balance reaches zero (true average-cost-basis)?
- **Denominator (BTCMTL balance):** which fund accounts hold BTC — only BOSS, or every
  subfund? Confirm the asset is `BTCMTL` (and which issuer) and not native Stellar `BTC`.
- **Sells:** when the fund sells BTCMTL — leave the cumulative numerator alone (FIFO/LIFO),
  reduce pro-rata by the fraction sold, or reset to last-purchase-only basis?

### Q2. I50 — MTL/MTLRECT divergence

The spec itself marks this «уточняется». A concrete definition is required:

- What variables to compare — market prices (`I10` vs `I49`), book-value-equivalents,
  supply ratios, or a composite?
- What metric — absolute diff, percentage `|I10−I49|/I10`, ratio `I10/I49`, or a
  discrete flag (`OK`/`WARN`/`CRITICAL`) with thresholds?
- Numeric, string label, or both? The rest of the pipeline assumes `decimal.Decimal`;
  a string-tagged status would need a separate sink.

Status: deferred per product owner — "точно так же, как и с I39".

## Data-source contracts

### I25 / I26 — stellar.expert `/stats-history`

`internal/stellarexpert.Client.FetchEURMTLPaymentStats` issues a single GET to:

```
https://api.stellar.expert/explorer/public/asset/EURMTL-GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V-2/stats-history
```

The response is an array of per-day rows ordered ascending by `ts` (UTC midnight). Per
row, `payments_amount` is an integer in stroops; the client converts via `decimal.Shift(-7)`.

- **I25** for date *D* = `payments_amount` of the row whose `ts` equals *D*.
- **I26** for date *D* = running sum of `payments_amount` over rows with `ts ≤ D`.

When stellar.expert hasn't yet ingested the requested date the client returns
`ErrNoDailyEntry` and the metrics service sticky-falls back to yesterday's persisted
values; any other transport / decode error logs at Error and also sticky-falls back —
empty payloads or stale-only datasets are surfaced as errors, never silently treated as
"no data".

## Refresh recipe — verifying I26 against external truth

```
curl https://api.stellar.expert/explorer/public/asset/EURMTL-GACKTN5DAZGWXRWB2WLM6OPBDHAMT6SJNGLJZPQMEZBUR4JUGBX2UK7V-2 \
  | jq '.payments_amount'
# divide by 10^7 → cumulative EURMTL — should equal max(I26) in fund_indicators
```

The sum of all daily I25 values must also equal `max(I26)`. The last external
reconciliation on 2026-05-07 gave `28 538 906.3718166`, matching stellar.expert's
own cumulative figure. The same endpoint, plus `/stats-history`, is what `stat report`
consumes daily.
