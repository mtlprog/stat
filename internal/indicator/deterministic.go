package indicator

// DeterministicIDs are the indicator IDs whose values can be re-derived from a single
// fund_snapshots.data JSONB row plus already-computed Layer 0 dependencies — no Horizon,
// no LiveMetrics, no historical-snapshot lookups. Used by `stat backfill-indicators` to
// avoid storing zeros for indicators whose history cannot be honestly reconstructed.
//
// Layer 0 (per-account totals): I51, I52, I53, I56, I57, I58, I59, I60, I61.
// Layer 1 derived from Layer 0 only: I3 (sum of subfond totals), I4 (operating balance).
//
// Excluded — even though the calculator runs, the result is meaningless without
// LiveMetrics, Horizon, or historical snapshots:
//   I1, I2, I5, I6, I7, I8, I10            — need MTLCirculation / market price (LiveMetrics or Horizon)
//   I11, I15, I16, I17, I33, I34, I54      — dividend chain (LiveMetrics + 12 months of history)
//   I18, I21, I22, I23, I27, I40           — Horizon-derived holder/shareholder metrics
//   I24, I25, I26                          — EURMTL participants & DEX volumes (Horizon)
//   I30                                    — Price/Book (depends on I8, I10)
//   I43–I48                                — analytics (need price history)
//   I49                                    — MTLRECT live price (Horizon)
//   I55                                    — Price year ago (historical snapshot)
var DeterministicIDs = map[int]bool{
	3: true, 4: true,
	51: true, 52: true, 53: true,
	56: true, 57: true, 58: true, 59: true, 60: true, 61: true,
}
