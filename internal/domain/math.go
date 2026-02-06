package domain

import (
	"strings"

	"github.com/shopspring/decimal"
)

const stellarPrecision = 7

// SafeParse parses a string into a decimal, returning zero for invalid or empty input.
func SafeParse(value string) decimal.Decimal {
	if value == "" {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(value)
	if err != nil {
		return decimal.Zero
	}
	return d
}

// SafeMultiply multiplies two string values, returning zero if either is invalid.
func SafeMultiply(a, b string) decimal.Decimal {
	da := SafeParse(a)
	db := SafeParse(b)
	return da.Mul(db)
}

// SafeSum adds two decimals.
func SafeSum(a, b decimal.Decimal) decimal.Decimal {
	return a.Add(b)
}

// MultiplyWithPrecision multiplies two string values with Stellar precision (7 decimal places),
// stripping trailing zeros. Returns "0" for invalid input.
func MultiplyWithPrecision(a, b string) string {
	da := SafeParse(a)
	db := SafeParse(b)
	result := da.Mul(db)
	return formatStellar(result)
}

// DivideWithPrecision divides two string values with Stellar precision (7 decimal places),
// stripping trailing zeros. Returns "0" for division by zero or invalid input.
func DivideWithPrecision(a, b string) string {
	da := SafeParse(a)
	db := SafeParse(b)
	if db.IsZero() {
		return "0"
	}
	result := da.Div(db)
	return formatStellar(result)
}

// formatStellar rounds to 7 decimal places and strips trailing zeros.
func formatStellar(d decimal.Decimal) string {
	rounded := d.Round(stellarPrecision)
	s := rounded.StringFixed(stellarPrecision)
	if !strings.Contains(s, ".") {
		return s
	}
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}
