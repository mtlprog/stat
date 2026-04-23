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

// IndicatorMeta holds the canonical name, unit, and description for an indicator.
type IndicatorMeta struct {
	Name        string
	Unit        string
	Description string
}

// indicatorRegistry maps indicator IDs to their canonical metadata.
// All calculators MUST use NewIndicator() to construct indicators from this registry.
// Descriptions are sourced from описание_и_формулы_параметров.xlsx.
var indicatorRegistry = map[int]IndicatorMeta{
	1:  {Name: "Market Cap EUR", Unit: "EURMTL", Description: "Рыночная капитализация в евро"},
	2:  {Name: "Market Cap BTC", Unit: "BTC", Description: "Рыночная капитализация в биткоинах"},
	3:  {Name: "Assets Value MTLF", Unit: "EURMTL", Description: "Совокупная стоимость активов"},
	4:  {Name: "Operating Balance", Unit: "EURMTL", Description: "Кэш и его эквивалент"},
	5:  {Name: "Total Shares", Unit: "shares", Description: "Количество всех акций фонда на рынке"},
	6:  {Name: "MTL in Circulation", Unit: "MTL", Description: "Количество акций MTL на рынке"},
	7:  {Name: "MTLRECT in Circulation", Unit: "MTLRECT", Description: "Количество акций MTLRECT на рынке"},
	8:  {Name: "Share Book Value", Unit: "EURMTL", Description: "Балансовая стоимость MTL акции"},
	10: {Name: "Share Market Price", Unit: "EURMTL", Description: "Рыночная цена MTL акции"},
	11: {Name: "Monthly Dividends", Unit: "EURMTL", Description: "Объём дивидендов, начисленных за последний месяц"},
	15: {Name: "Dividends Per Share", Unit: "EURMTL", Description: "Объём месячных дивидендов на 1 акцию"},
	16: {Name: "Annual Dividend Yield 1", Unit: "%", Description: "Прогноз годовой доходности 1 акции по медиане"},
	17: {Name: "Annual Dividend Yield 2", Unit: "%", Description: "Скорректированная прогнозируемая доходность акции по медиане к цене акции год назад"},
	18: {Name: "Shareholders by EURMTL", Unit: "accounts", Description: "Полное кол-во аккаунтов, получивших дивиденды в EURMTL в последнем месяце"},
	21: {Name: "Average Shareholding", Unit: "shares", Description: "Средний объём акционерного пакета"},
	22: {Name: "Average Value per Shareholder", Unit: "EURMTL", Description: "Средняя цена акционерного пакета"},
	23: {Name: "Median Shareholding", Unit: "shares", Description: "Медианное количество акций в акционерном пакете"},
	24: {Name: "EURMTL Participants", Unit: "accounts", Description: "Число Stellar-аккаунтов с ненулевым балансом EURMTL"},
	25: {Name: "EURMTL Daily Volume", Unit: "EURMTL", Description: "Оборот токеномики за прошлые сутки"},
	26: {Name: "EURMTL 30d Volume", Unit: "EURMTL", Description: "Совокупный оборот токеномики за последние 30 дней"},
	27: {Name: "MTL Shareholders (>=1)", Unit: "accounts", Description: "Число Stellar-аккаунтов, на которых более 1 MTL или MTLRECT"},
	30: {Name: "Price/Book Ratio", Unit: "ratio", Description: "Ценность акции от её балансовой стоимости"},
	33: {Name: "Earnings Per Share", Unit: "EURMTL", Description: "Доход на акцию"},
	34: {Name: "Price/Earnings Ratio", Unit: "ratio", Description: "Относительная ценность акции по дивиденду"},
	40: {Name: "Association Participants", Unit: "accounts", Description: "Число участников Ассоциации Монтелиберо, держателей MTLAP"},
	43: {Name: "Total ROI", Unit: "%", Description: "Общая рентабельность инвестиций"},
	44: {Name: "Beta", Unit: "ratio", Description: "Чувствительность акции к движению рынка (биткоину)"},
	45: {Name: "Sharpe Ratio", Unit: "ratio", Description: "Доходность на единицу риска"},
	46: {Name: "Sortino Ratio", Unit: "ratio", Description: "Доходность на единицу риска при негативной волатильности"},
	47: {Name: "Value at Risk", Unit: "%", Description: "Оценка потенциального убытка с заданной вероятностью"},
	48: {Name: "Dividend/Book Value", Unit: "ratio", Description: "Эффективность управления балансом"},
	49: {Name: "MTLRECT Market Price", Unit: "EURMTL", Description: "Рыночная цена MTLRECT"},
	51: {Name: "DEFI Total Value", Unit: "EURMTL", Description: "Стоимость активов субфонда DEFI"},
	52: {Name: "MCITY Total Value", Unit: "EURMTL", Description: "Стоимость активов субфонда MCITY"},
	53: {Name: "MABIZ Total Value", Unit: "EURMTL", Description: "Стоимость активов субфонда MABIZ"},
	54: {Name: "Annual DPS", Unit: "EURMTL", Description: "Годовые дивиденды на акцию"},
	55: {Name: "Price Year Ago", Unit: "EURMTL", Description: "Рыночная цена MTL акции год назад"},
	56: {Name: "MFApart Total Value", Unit: "EURMTL", Description: "Стоимость активов ПИФ MFApart"},
	57: {Name: "MFBond Total Value", Unit: "EURMTL", Description: "Стоимость активов ПИФ MFBond"},
	58: {Name: "Issuer Free Assets", Unit: "EURMTL", Description: "Свободные активы эмитента"},
	59: {Name: "BOSS Total Value", Unit: "EURMTL", Description: "Стоимость активов субфонда BOSS"},
	60: {Name: "ADMIN Total Value", Unit: "EURMTL", Description: "Стоимость активов счёта ADMIN"},
	61: {Name: "BTC Rate", Unit: "EUR", Description: "Курс BTC в EUR"},
}

// Indicator represents a calculated statistical indicator.
type Indicator struct {
	ID          int             `json:"id"`
	Name        string          `json:"name"`
	Value       decimal.Decimal `json:"value"`
	Unit        string          `json:"unit"`
	Description string          `json:"description,omitempty"`
}

// NewIndicator creates an indicator using the canonical metadata from the registry.
// Falls back to the provided name and unit if the ID is not registered.
func NewIndicator(id int, value decimal.Decimal, name, unit string) Indicator {
	if meta, ok := indicatorRegistry[id]; ok {
		return Indicator{ID: id, Name: meta.Name, Value: value, Unit: meta.Unit, Description: meta.Description}
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
