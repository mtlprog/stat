# MTLF Stat API

MTLF Stat tracks the financial health of the [Montelibero Fund](https://montelibero.org) (Stellar blockchain). Base URL: `https://stat.mtlprog.xyz`. All endpoints return JSON; only snapshot generation requires authentication.

---

## Snapshots

**GET /api/v1/snapshots/latest** — latest snapshot.

**GET /api/v1/snapshots/{date}** — snapshot for a specific date (`YYYY-MM-DD`, midnight UTC).

**GET /api/v1/snapshots?limit=N** — list of snapshots, newest first. Default limit 30, max 365.

**POST /api/v1/snapshots/generate** — triggers a fresh snapshot. Requires `Authorization: Bearer <ADMIN_API_KEY>`. Returns the generated `FundStructureData`.

### Snapshot shape

```json
{
  "id": 1,
  "snapshotDate": "2026-02-18T00:00:00Z",
  "data": {
    "accounts": [...],
    "mutualFunds": [...],
    "otherAccounts": [...],
    "aggregatedTotals": { "totalEURMTL": "...", "totalXLM": "..." },
    "liveMetrics": {
      "mtlMarketPrice": "12.50",
      "mtlCirculation": "50000.00",
      "mtlrectCirculation": "5000.00",
      "monthlyDividends": "2500.00"
    },
    "warnings": []
  }
}
```

Each `accounts[]` entry:
```json
{
  "id": "G...",
  "name": "MABIZ",
  "type": "subfond",
  "tokens": [{ "code": "EURMTL", "balance": "10000.00", "priceInEURMTL": "1.00", "valueInEURMTL": "10000.00" }],
  "xlmBalance": "5000.0000000",
  "xlmPriceInEURMTL": "0.08",
  "totalEURMTL": "10400.00"
}
```

---

## Indicators

Indicators are computed on-the-fly from a snapshot.

**GET /api/v1/indicators** — indicators from latest snapshot.

**GET /api/v1/indicators?compare=30d** — same, plus `changeAbs` / `changePct` vs. a snapshot N days ago. Accepted values: `30d`, `90d`, `180d`, `365d`.

**GET /api/v1/indicators/{date}** — indicators from a specific snapshot (`YYYY-MM-DD`).

### Response shape

```json
// without compare: omit changeAbs and changePct
[{ "id": "I1", "name": "Market Cap EUR", "value": "625000.00", "unit": "EURMTL",
   "changeAbs": "25000.00", "changePct": "4.17" }]
```

### Key indicators

| ID | Name | Unit |
|----|------|------|
| I1 | Market Cap EUR | EURMTL |
| I2 | Market Cap BTC | BTC |
| I3 | Assets Value MTLF | EURMTL |
| I8 | Share Book Value | EURMTL |
| I10 | Share Market Price | EURMTL |
| I11 | Monthly Dividends | EURMTL |
| I15 | Dividends Per Share | EURMTL |
| I16 | Annual Dividend Yield 1 | % |
| I27 | MTL Shareholders (≥1 MTL) | count |
| I30 | Price/Book Ratio | — |
| I34 | P/E Ratio | — |
| I43 | Total ROI | % |
| I51–I60 | Per-subfund totals | EURMTL |

---

## Key domain concepts

**EURMTL** — fund base currency (EUR-pegged). **MTL** — main share token. **MTLRECT** — restricted share token. Snapshots are stored daily at midnight UTC.

---

## Error responses

HTTP status codes: `400` bad request, `401` unauthorized, `404` not found, `500` internal error.
