package domain

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestSafeParse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"valid integer", "100", "100"},
		{"valid decimal", "3.14", "3.14"},
		{"zero", "0", "0"},
		{"negative", "-5.5", "-5.5"},
		{"empty string", "", "0"},
		{"invalid string", "abc", "0"},
		{"whitespace", "  ", "0"},
		{"large number", "999999999999.1234567", "999999999999.1234567"},
		{"small fraction", "0.0000001", "0.0000001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SafeParse(tt.input)
			want, _ := decimal.NewFromString(tt.want)
			if !got.Equal(want) {
				t.Errorf("SafeParse(%q) = %s, want %s", tt.input, got, want)
			}
		})
	}
}

func TestSafeMultiply(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want string
	}{
		{"normal", "10", "5", "50"},
		{"decimal", "3.5", "2", "7"},
		{"zero a", "0", "100", "0"},
		{"zero b", "100", "0", "0"},
		{"invalid a", "abc", "5", "0"},
		{"invalid b", "5", "abc", "0"},
		{"both invalid", "abc", "def", "0"},
		{"empty a", "", "5", "0"},
		{"high precision", "1.2345678", "1", "1.2345678"},
		{"negative", "-3", "4", "-12"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SafeMultiply(tt.a, tt.b)
			want, _ := decimal.NewFromString(tt.want)
			if !got.Equal(want) {
				t.Errorf("SafeMultiply(%q, %q) = %s, want %s", tt.a, tt.b, got, want)
			}
		})
	}
}

func TestSafeSum(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want string
	}{
		{"normal", "10", "5", "15"},
		{"zero", "0", "0", "0"},
		{"negative", "-3", "5", "2"},
		{"decimal", "1.5", "2.3", "3.8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _ := decimal.NewFromString(tt.a)
			b, _ := decimal.NewFromString(tt.b)
			got := SafeSum(a, b)
			want, _ := decimal.NewFromString(tt.want)
			if !got.Equal(want) {
				t.Errorf("SafeSum(%s, %s) = %s, want %s", tt.a, tt.b, got, want)
			}
		})
	}
}

func TestMultiplyWithPrecision(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want string
	}{
		{"normal", "10", "5", "50"},
		{"decimal precision", "1.23456789", "1", "1.2345679"},
		{"zero", "0", "100", "0"},
		{"invalid input", "abc", "5", "0"},
		{"trailing zeros stripped", "1.1000000", "1", "1.1"},
		{"exact seven places", "0.0000001", "1", "0.0000001"},
		{"high precision rounds", "0.00000005", "1", "0.0000001"},
		{"integer result", "2", "3", "6"},
		{"negative", "-3.5", "2", "-7"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MultiplyWithPrecision(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("MultiplyWithPrecision(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestDivideWithPrecision(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want string
	}{
		{"normal", "10", "5", "2"},
		{"decimal result", "10", "3", "3.3333333"},
		{"division by zero", "10", "0", "0"},
		{"zero numerator", "0", "5", "0"},
		{"invalid a", "abc", "5", "0"},
		{"invalid b", "5", "abc", "0"},
		{"trailing zeros stripped", "10", "4", "2.5"},
		{"high precision", "1", "7", "0.1428571"},
		{"negative", "-10", "3", "-3.3333333"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DivideWithPrecision(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("DivideWithPrecision(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
