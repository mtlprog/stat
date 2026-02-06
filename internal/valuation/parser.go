package valuation

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/mtlprog/stat/internal/domain"
)

var externalSymbols = []string{"BTC", "ETH", "XLM", "Sats", "USD", "AU"}

var compoundRegex = regexp.MustCompile(`^(\w+)\s+([\d.]+)(g|oz)$`)

// ParseDataEntryValue parses a decoded DATA entry value into a ValuationValue.
func ParseDataEntryValue(raw string) (domain.ValuationValue, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return domain.ValuationValue{}, fmt.Errorf("empty value")
	}

	// Check compound format: "AU 1g", "AU 2.5oz"
	if matches := compoundRegex.FindStringSubmatch(raw); matches != nil {
		symbol := matches[1]
		if !lo.Contains(externalSymbols, symbol) {
			return domain.ValuationValue{}, fmt.Errorf("unknown symbol in compound value: %s", symbol)
		}
		quantity, err := strconv.ParseFloat(matches[2], 64)
		if err != nil || quantity <= 0 {
			return domain.ValuationValue{}, fmt.Errorf("invalid quantity in compound value: %s", matches[2])
		}
		return domain.ValuationValue{
			Type:     domain.ValuationValueExternal,
			Symbol:   symbol,
			Quantity: &quantity,
			Unit:     matches[3],
		}, nil
	}

	// Check if it's an external symbol
	if lo.Contains(externalSymbols, raw) {
		return domain.ValuationValue{
			Type:   domain.ValuationValueExternal,
			Symbol: raw,
		}, nil
	}

	// Try parsing as numeric with European decimal normalization
	normalized := normalizeEuropeanDecimal(raw)
	d, err := decimal.NewFromString(normalized)
	if err != nil {
		return domain.ValuationValue{}, fmt.Errorf("invalid value: %q", raw)
	}

	if d.LessThanOrEqual(decimal.Zero) {
		return domain.ValuationValue{}, fmt.Errorf("value must be positive, got: %s", d.String())
	}

	return domain.ValuationValue{
		Type:  domain.ValuationValueEURMTL,
		Value: d.String(),
	}, nil
}

// normalizeEuropeanDecimal converts European decimal format to standard format.
// "0,8" → "0.8", "1.234,56" → "1234.56", "1.5" → "1.5"
func normalizeEuropeanDecimal(s string) string {
	hasComma := strings.Contains(s, ",")
	hasDot := strings.Contains(s, ".")

	if hasComma && hasDot {
		// European: dot is thousands, comma is decimal
		s = strings.ReplaceAll(s, ".", "")
		s = strings.Replace(s, ",", ".", 1)
	} else if hasComma {
		// Comma-only: treat as decimal separator
		s = strings.Replace(s, ",", ".", 1)
	}
	// Dot-only or no separator: standard format, no change

	return s
}
