package indicator

import (
	"math"
	"testing"

	"github.com/shopspring/decimal"
)

func TestMean(t *testing.T) {
	values := []decimal.Decimal{
		decimal.NewFromInt(1),
		decimal.NewFromInt(2),
		decimal.NewFromInt(3),
		decimal.NewFromInt(4),
		decimal.NewFromInt(5),
	}

	result := Mean(values)
	if !result.Equal(decimal.NewFromInt(3)) {
		t.Errorf("Mean = %s, want 3", result)
	}
}

func TestMeanEmpty(t *testing.T) {
	result := Mean(nil)
	if !result.Equal(decimal.Zero) {
		t.Errorf("Mean(nil) = %s, want 0", result)
	}
}

func TestVariance(t *testing.T) {
	values := []decimal.Decimal{
		decimal.NewFromInt(2),
		decimal.NewFromInt(4),
		decimal.NewFromInt(4),
		decimal.NewFromInt(4),
		decimal.NewFromInt(5),
		decimal.NewFromInt(5),
		decimal.NewFromInt(7),
		decimal.NewFromInt(9),
	}

	result := Variance(values)
	f, _ := result.Float64()
	if math.Abs(f-4.571429) > 0.001 {
		t.Errorf("Variance = %f, want ~4.571429", f)
	}
}

func TestStdDev(t *testing.T) {
	values := []decimal.Decimal{
		decimal.NewFromInt(2),
		decimal.NewFromInt(4),
		decimal.NewFromInt(4),
		decimal.NewFromInt(4),
		decimal.NewFromInt(5),
		decimal.NewFromInt(5),
		decimal.NewFromInt(7),
		decimal.NewFromInt(9),
	}

	result := StdDev(values)
	f, _ := result.Float64()
	if math.Abs(f-2.138) > 0.01 {
		t.Errorf("StdDev = %f, want ~2.138", f)
	}
}

func TestMedian(t *testing.T) {
	tests := []struct {
		name   string
		values []decimal.Decimal
		want   string
	}{
		{"odd count", []decimal.Decimal{
			decimal.NewFromInt(1), decimal.NewFromInt(3), decimal.NewFromInt(5),
		}, "3"},
		{"even count", []decimal.Decimal{
			decimal.NewFromInt(1), decimal.NewFromInt(2), decimal.NewFromInt(3), decimal.NewFromInt(4),
		}, "2.5"},
		{"single", []decimal.Decimal{decimal.NewFromInt(42)}, "42"},
		{"empty", nil, "0"},
		{"unsorted", []decimal.Decimal{
			decimal.NewFromInt(5), decimal.NewFromInt(1), decimal.NewFromInt(3),
		}, "3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Median(tt.values)
			want, _ := decimal.NewFromString(tt.want)
			if !result.Equal(want) {
				t.Errorf("Median = %s, want %s", result, tt.want)
			}
		})
	}
}

func TestCovariance(t *testing.T) {
	a := []decimal.Decimal{
		decimal.NewFromInt(1), decimal.NewFromInt(2), decimal.NewFromInt(3), decimal.NewFromInt(4), decimal.NewFromInt(5),
	}
	b := []decimal.Decimal{
		decimal.NewFromInt(2), decimal.NewFromInt(4), decimal.NewFromInt(6), decimal.NewFromInt(8), decimal.NewFromInt(10),
	}

	result := Covariance(a, b)
	if !result.Equal(decimal.NewFromInt(5)) {
		t.Errorf("Covariance = %s, want 5", result)
	}
}

func TestNormalQuantile(t *testing.T) {
	// q(0.05) ≈ -1.645
	q := NormalQuantile(0.05)
	if math.Abs(q-(-1.645)) > 0.01 {
		t.Errorf("NormalQuantile(0.05) = %f, want ~-1.645", q)
	}

	// q(0.5) = 0
	q50 := NormalQuantile(0.5)
	if math.Abs(q50) > 0.01 {
		t.Errorf("NormalQuantile(0.5) = %f, want ~0", q50)
	}
}
