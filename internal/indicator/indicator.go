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

// Indicator represents a calculated statistical indicator.
type Indicator struct {
	ID    int             `json:"id"`
	Name  string          `json:"name"`
	Value decimal.Decimal `json:"value"`
	Unit  string          `json:"unit"`
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
	calculators []Calculator
}

// NewRegistry creates a new indicator registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a calculator to the registry.
func (r *Registry) Register(calc Calculator) {
	r.calculators = append(r.calculators, calc)
}

// CalculateAll runs all registered calculators in dependency order.
func (r *Registry) CalculateAll(ctx context.Context, data domain.FundStructureData, hist *HistoricalData) ([]Indicator, error) {
	ordered := r.topologicalSort()

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
func (r *Registry) topologicalSort() []Calculator {
	// Build dependency graph
	calcByID := make(map[int]Calculator)
	for _, calc := range r.calculators {
		for _, id := range calc.IDs() {
			calcByID[id] = calc
		}
	}

	visited := make(map[Calculator]bool)
	var ordered []Calculator

	var visit func(calc Calculator)
	visit = func(calc Calculator) {
		if visited[calc] {
			return
		}
		visited[calc] = true

		for _, dep := range calc.Dependencies() {
			if depCalc, ok := calcByID[dep]; ok {
				visit(depCalc)
			}
		}

		ordered = append(ordered, calc)
	}

	for _, calc := range r.calculators {
		visit(calc)
	}

	return lo.Uniq(ordered)
}
