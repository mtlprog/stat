package indicator

import (
	"context"
	"fmt"
	"sort"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
	"github.com/mtlprog/stat/internal/snapshot"
)

// IndicatorMeta holds the canonical name and unit for an indicator.
type IndicatorMeta struct {
	Name string
	Unit string
}

// indicatorRegistry maps indicator IDs to their canonical metadata.
// All calculators MUST use NewIndicator() to construct indicators from this registry.
var indicatorRegistry = map[int]IndicatorMeta{
	1:  {Name: "Market Cap EUR", Unit: "EURMTL"},
	2:  {Name: "Market Cap BTC", Unit: "BTC"},
	3:  {Name: "Assets Value MTLF", Unit: "EURMTL"},
	4:  {Name: "Operating Balance", Unit: "EURMTL"},
	5:  {Name: "Total Shares", Unit: "shares"},
	6:  {Name: "MTL in Circulation", Unit: "MTL"},
	7:  {Name: "MTLRECT in Circulation", Unit: "MTLRECT"},
	8:  {Name: "Share Book Value", Unit: "EURMTL"},
	10: {Name: "Share Market Price", Unit: "EURMTL"},
	11: {Name: "Monthly Dividends", Unit: "EURMTL"},
	15: {Name: "Dividends Per Share", Unit: "EURMTL"},
	16: {Name: "Annual Dividend Yield 1", Unit: "%"},
	17: {Name: "Annual Dividend Yield 2", Unit: "%"},
	18: {Name: "Shareholders by EURMTL", Unit: "accounts"},
	21: {Name: "Average Shareholding", Unit: "shares"},
	22: {Name: "Average Value per Shareholder", Unit: "EURMTL"},
	23: {Name: "Median Shareholding", Unit: "shares"},
	24: {Name: "EURMTL Participants", Unit: "accounts"},
	25: {Name: "EURMTL Daily Volume", Unit: "EURMTL"},
	26: {Name: "EURMTL 30d Volume", Unit: "EURMTL"},
	27: {Name: "MTL Shareholders (>=1)", Unit: "accounts"},
	30: {Name: "Price/Book Ratio", Unit: "ratio"},
	33: {Name: "Earnings Per Share", Unit: "EURMTL"},
	34: {Name: "Price/Earnings Ratio", Unit: "ratio"},
	40: {Name: "Association Participants", Unit: "accounts"},
	43: {Name: "Total ROI", Unit: "%"},
	44: {Name: "Beta", Unit: "ratio"},
	45: {Name: "Sharpe Ratio", Unit: "ratio"},
	46: {Name: "Sortino Ratio", Unit: "ratio"},
	47: {Name: "Value at Risk", Unit: "%"},
	48: {Name: "Dividend/Book Value", Unit: "ratio"},
	49: {Name: "MTLRECT Market Price", Unit: "EURMTL"},
	51: {Name: "DEFI Total Value", Unit: "EURMTL"},
	52: {Name: "MCITY Total Value", Unit: "EURMTL"},
	53: {Name: "MABIZ Total Value", Unit: "EURMTL"},
	54: {Name: "Annual DPS", Unit: "EURMTL"},
	55: {Name: "Price Year Ago", Unit: "EURMTL"},
	56: {Name: "MFApart Total Value", Unit: "EURMTL"},
	57: {Name: "MFBond Total Value", Unit: "EURMTL"},
	58: {Name: "Issuer Free Assets", Unit: "EURMTL"},
	59: {Name: "BOSS Total Value", Unit: "EURMTL"},
	60: {Name: "ADMIN Total Value", Unit: "EURMTL"},
	61: {Name: "BTC Rate", Unit: "EUR"},
}

// Indicator represents a calculated statistical indicator.
type Indicator struct {
	ID    int             `json:"id"`
	Name  string          `json:"name"`
	Value decimal.Decimal `json:"value"`
	Unit  string          `json:"unit"`
}

// NewIndicator creates an indicator using the canonical metadata from the registry.
// Falls back to the provided name and unit if the ID is not registered.
func NewIndicator(id int, value decimal.Decimal, name, unit string) Indicator {
	if meta, ok := indicatorRegistry[id]; ok {
		return Indicator{ID: id, Name: meta.Name, Value: value, Unit: meta.Unit}
	}
	return Indicator{ID: id, Name: name, Value: value, Unit: unit}
}

// Calculator computes one or more indicators given a snapshot and previously computed indicators.
type Calculator interface {
	IDs() []int
	Dependencies() []int
	Calculate(ctx context.Context, data domain.FundStructureData, deps map[int]Indicator, hist *HistoricalData) ([]Indicator, error)
}

// HistoricalData provides access to historical snapshots for time-series calculations.
type HistoricalData struct {
	Repo     snapshot.Repository
	Slug     string
	Calculus func(ctx context.Context, data domain.FundStructureData, deps map[int]Indicator, hist *HistoricalData) ([]Indicator, error)
}

// Registry manages the execution of calculators in dependency order.
type Registry struct {
	calculators   []Calculator
	registeredIDs map[int]bool
}

// NewRegistry creates a new indicator registry.
func NewRegistry() *Registry {
	return &Registry{registeredIDs: make(map[int]bool)}
}

// Register adds a calculator to the registry.
// Panics if any indicator ID is already registered (programming error).
func (r *Registry) Register(calc Calculator) {
	for _, id := range calc.IDs() {
		if r.registeredIDs[id] {
			panic(fmt.Sprintf("duplicate indicator ID %d registered", id))
		}
		r.registeredIDs[id] = true
	}
	r.calculators = append(r.calculators, calc)
}

// CalculateAll runs all registered calculators in dependency order.
func (r *Registry) CalculateAll(ctx context.Context, data domain.FundStructureData, hist *HistoricalData) ([]Indicator, error) {
	ordered, err := r.topologicalSort()
	if err != nil {
		return nil, fmt.Errorf("sorting calculators: %w", err)
	}

	computed := make(map[int]Indicator)
	var allIndicators []Indicator

	for _, calc := range ordered {
		// Check dependencies are satisfied
		for _, dep := range calc.Dependencies() {
			if _, ok := computed[dep]; !ok {
				return nil, fmt.Errorf("indicator %v depends on I%d which is not yet computed", calc.IDs(), dep)
			}
		}

		indicators, err := calc.Calculate(ctx, data, computed, hist)
		if err != nil {
			return nil, fmt.Errorf("calculating indicators %v: %w", calc.IDs(), err)
		}

		for _, ind := range indicators {
			computed[ind.ID] = ind
			allIndicators = append(allIndicators, ind)
		}
	}

	sort.Slice(allIndicators, func(i, j int) bool {
		return allIndicators[i].ID < allIndicators[j].ID
	})

	return allIndicators, nil
}

// topologicalSort orders calculators so dependencies come first.
// Returns an error if a dependency cycle is detected.
func (r *Registry) topologicalSort() ([]Calculator, error) {
	// Build dependency graph
	calcByID := make(map[int]Calculator)
	for _, calc := range r.calculators {
		for _, id := range calc.IDs() {
			calcByID[id] = calc
		}
	}

	visited := make(map[Calculator]bool)
	inProgress := make(map[Calculator]bool)
	var ordered []Calculator

	var visit func(calc Calculator) error
	visit = func(calc Calculator) error {
		if visited[calc] {
			return nil
		}
		if inProgress[calc] {
			return fmt.Errorf("dependency cycle detected involving indicators %v", calc.IDs())
		}
		inProgress[calc] = true

		for _, dep := range calc.Dependencies() {
			if depCalc, ok := calcByID[dep]; ok {
				if err := visit(depCalc); err != nil {
					return err
				}
			}
		}

		delete(inProgress, calc)
		visited[calc] = true
		ordered = append(ordered, calc)
		return nil
	}

	for _, calc := range r.calculators {
		if err := visit(calc); err != nil {
			return nil, err
		}
	}

	return lo.Uniq(ordered), nil
}
