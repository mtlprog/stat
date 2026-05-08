package indicator

// DeterministicIDs are the indicator IDs whose values can be re-derived from a single
// fund_snapshots.data JSONB row plus already-computed Layer 0 dependencies — no Horizon,
// no LiveMetrics, no historical-snapshot lookups. Used by `stat backfill-indicators` to
// avoid storing zeros for indicators whose history cannot be honestly reconstructed.
//
// Layer 0 (per-account totals): I51, I52, I53, I56, I58, I59, I60, I61.
// Layer 1 derived from Layer 0 only: I3 (sum of subfond totals), I4 (operating balance).
// Manually-managed constant: I39 (BPP) — value is hard-coded in bpp.go.
//
// Excluded — even though the calculator runs, the result is meaningless without
// LiveMetrics, Horizon, or historical snapshots:
//
//	I1, I2, I6, I7, I8, I10            — need MTLCirculation / market price (LiveMetrics or Horizon)
//	I5                                 — transitively non-deterministic via I6, I7
//	I11, I15, I17, I34, I43, I54       — dividend chain (LiveMetrics + history for I55)
//	I18, I21, I22, I23, I27, I62       — Horizon-derived holder/shareholder metrics
//	I24                                — EURMTL participants (Horizon)
//	I25, I26                           — daily / cumulative payment volume (stellar.expert)
//	I30                                — Price/Book (depends on I8, I10)
//	I49                                — MTLRECT live price (Horizon)
//	I55                                — Price year ago (historical snapshot)
var DeterministicIDs = map[int]bool{
	3: true, 4: true,
	39: true,
	51: true, 52: true, 53: true,
	56: true, 58: true, 59: true, 60: true, 61: true,
}
