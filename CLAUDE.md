# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development Commands

```bash
# Build the binary
go build -o stat ./...

# Run tests
go test ./...

# Format and lint
go fmt ./...
go vet ./...
```

## Architecture

### Indicator System
- Indicators are computed **on-the-fly** from snapshots — never stored in the DB.
- Calculation is a layered DAG: `Layer0 → Layer1 → Layer2 → Dividend / Analytics / Tokenomics`.
- Each `Calculator` declares `IDs()` and `Dependencies()`; `Registry.CalculateAll` resolves order via topological sort.
- To add a new calculator: implement `Calculator` interface, define its Horizon interface in the same file, register in `service.go`, extend `IndicatorHorizon` if it needs `horizon.Client`.

### Snapshot Data Model
- `fund_snapshots.data` (JSONB) stores `domain.FundStructureData` with per-account token balances and prices.
- Token prices captured at snapshot time live in `FundAccountPortfolio.Tokens[].PriceInEURMTL` — use these for historical price lookups (see `findBTCPrice` in `layer0.go` as a pattern).
- `snapshot.Repository.GetByDate` requires exact date match (midnight UTC); snapshots are stored by the `ReportWorker` using `time.Date(..., time.UTC)`.

### Key Domain Constants
- `domain.IssuerAddress` — main fund issuer Stellar address
- `domain.EURMTLAsset()` — fund base asset (EUR-pegged stablecoin)
- `domain.AccountRegistry()` — all 11 fund accounts (used to exclude fund addresses from external payment filtering)

## Horizon API Patterns

### Service Wiring
- `horizon.Client` → `IndicatorHorizon` (combined interface: `TokenomicsHorizon + CirculationHorizon + DividendHorizon`)
- `price.Service` → `HorizonPriceSource` (orderbook / pathfinding only)
- Both are passed to `indicator.NewService(priceSvc, horizonClient, hist)` in `main.go`.

### Cursor-Based Pagination
```go
// Extract next-page path from Horizon's _links.next.href:
u, err := url.Parse(resp.Links.Next.Href)
if err != nil { break }
path = u.Path + "?" + u.RawQuery
```
- Add `Links.Next.Href` field to response structs when pagination is needed.
- When paginating payments ordered desc by time, **check the timestamp before type/direction filters** so non-payment records don't block early termination.

## Git Conventions

- **Commit messages**: Use [Conventional Commits](https://www.conventionalcommits.org/) format (e.g., `feat:`, `fix:`, `refactor:`, `docs:`, `chore:`)
- **PR Merge Strategy**: Repository only allows rebase merges. Use `gh pr merge --rebase --delete-branch`

## samber/lo - Utility Library

Use `github.com/samber/lo` for readable, type-safe slice/map operations. Prefer `lo` helpers over manual loops.

### Slice Operations
```go
lo.Filter(slice, func(x T, _ int) bool { return condition })  // Filter elements
lo.Map(slice, func(x T, _ int) R { return transform(x) })     // Transform elements
lo.Reduce(slice, func(acc R, x T, _ int) R { ... }, init)     // Reduce to single value
lo.ForEach(slice, func(x T, _ int) { ... })                   // Iterate with side effects
lo.Uniq(slice)                                                 // Remove duplicates
lo.UniqBy(slice, func(x T) K { return key })                  // Remove duplicates by key
lo.Compact(slice)                                              // Remove zero values ("", 0, nil)
lo.Flatten(nested)                                             // Flatten nested slices
lo.Chunk(slice, size)                                          // Split into chunks
lo.GroupBy(slice, func(x T) K { return key })                 // Group by key -> map[K][]T
lo.KeyBy(slice, func(x T) K { return key })                   // Index by key -> map[K]T
lo.Partition(slice, func(x T, _ int) bool { ... })            // Split into [match, nomatch]
```

### Search Operations
```go
lo.Find(slice, func(x T) bool { return condition })           // Returns (value, found)
lo.FindOrElse(slice, fallback, func(x T) bool { ... })        // Returns value or fallback
lo.Contains(slice, value)                                      // Check if exists
lo.IndexOf(slice, value)                                       // Find index (-1 if not found)
lo.Every(slice, func(x T, _ int) bool { ... })                // All match predicate
lo.Some(slice, func(x T, _ int) bool { ... })                 // Any matches predicate
```

### Map Operations
```go
lo.Keys(m)                                                     // Get all keys
lo.Values(m)                                                   // Get all values
lo.PickBy(m, func(k K, v V) bool { ... })                     // Filter map entries
lo.OmitBy(m, func(k K, v V) bool { ... })                     // Exclude map entries
lo.MapKeys(m, func(v V, k K) K2 { return newKey })            // Transform keys
lo.MapValues(m, func(v V, k K) V2 { return newValue })        // Transform values
lo.Invert(m)                                                   // Swap keys and values
lo.Assign(maps...)                                             // Merge maps (later wins)
```

### Safety & Error Handling
```go
lo.Must(val, err)                                              // Panic on error, return val
lo.Must0(err)                                                  // Panic on error (no return)
lo.Must2(v1, v2, err)                                          // Panic on error, return v1, v2
lo.Coalesce(vals...)                                           // First non-zero value
lo.CoalesceOrEmpty(vals...)                                    // First non-zero or zero value
lo.IsEmpty(val)                                                // Check if zero value
lo.FromPtr(ptr)                                                // Dereference or zero value
lo.ToPtr(val)                                                  // Create pointer to value
lo.Ternary(cond, ifTrue, ifFalse)                             // Inline conditional
lo.If(cond, val).Else(other)                                  // Fluent conditional
```

### Parallel Processing
```go
import lop "github.com/samber/lo/parallel"
lop.Map(slice, func(x T, _ int) R { ... })                    // Parallel map
lop.ForEach(slice, func(x T, _ int) { ... })                  // Parallel iteration
lop.Filter(slice, func(x T, _ int) bool { ... })              // Parallel filter
```
