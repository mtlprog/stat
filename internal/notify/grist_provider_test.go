package notify

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestFormatDecimal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"0", "0"},
		{"1", "1"},
		{"999", "999"},
		{"1000", "1 000"},
		{"1000.5", "1 000.5"},
		{"1827956", "1 827 956"},
		{"1827956.42", "1 827 956.42"},
		{"-1827956.42", "-1 827 956.42"},
		{"0.0000001", "0.0000001"},
		{"1234567890", "1 234 567 890"},
	}

	for _, tc := range tests {
		d, _ := decimal.NewFromString(tc.input)
		got := formatDecimal(d)
		if got != tc.want {
			t.Errorf("formatDecimal(%s) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
