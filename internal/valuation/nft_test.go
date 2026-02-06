package valuation

import "testing"

func TestIsNFT(t *testing.T) {
	tests := []struct {
		balance string
		want    bool
	}{
		{"0.0000001", true},
		{"0.0000002", false},
		{"1.0000000", false},
		{"0", false},
		{"100", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.balance, func(t *testing.T) {
			if got := IsNFT(tt.balance); got != tt.want {
				t.Errorf("IsNFT(%q) = %v, want %v", tt.balance, got, tt.want)
			}
		})
	}
}
