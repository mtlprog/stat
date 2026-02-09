package indicator

import (
	"log/slog"
	"math"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Variance calculates the variance of a decimal slice.
func Variance(values []decimal.Decimal) decimal.Decimal {
	if len(values) < 2 {
		return decimal.Zero
	}

	mean := Mean(values)
	sumSqDiff := lo.Reduce(values, func(acc decimal.Decimal, v decimal.Decimal, _ int) decimal.Decimal {
		diff := v.Sub(mean)
		return acc.Add(diff.Mul(diff))
	}, decimal.Zero)

	return sumSqDiff.Div(decimal.NewFromInt(int64(len(values) - 1)))
}

// StdDev calculates the standard deviation of a decimal slice.
func StdDev(values []decimal.Decimal) decimal.Decimal {
	v := Variance(values)
	f, exact := v.Float64()
	if !exact {
		slog.Warn("precision loss in StdDev float64 conversion", "variance", v.String())
	}
	return decimal.NewFromFloat(math.Sqrt(f))
}

// Mean calculates the arithmetic mean of a decimal slice.
func Mean(values []decimal.Decimal) decimal.Decimal {
	if len(values) == 0 {
		return decimal.Zero
	}
	sum := lo.Reduce(values, func(acc decimal.Decimal, v decimal.Decimal, _ int) decimal.Decimal {
		return acc.Add(v)
	}, decimal.Zero)
	return sum.Div(decimal.NewFromInt(int64(len(values))))
}

// DownsideStdDev calculates the downside deviation (only negative returns).
func DownsideStdDev(returns []decimal.Decimal, threshold decimal.Decimal) decimal.Decimal {
	downside := lo.Filter(returns, func(r decimal.Decimal, _ int) bool {
		return r.LessThan(threshold)
	})
	if len(downside) == 0 {
		return decimal.Zero
	}

	sumSqDiff := lo.Reduce(downside, func(acc decimal.Decimal, v decimal.Decimal, _ int) decimal.Decimal {
		diff := v.Sub(threshold)
		return acc.Add(diff.Mul(diff))
	}, decimal.Zero)

	variance := sumSqDiff.Div(decimal.NewFromInt(int64(len(downside))))
	f, exact := variance.Float64()
	if !exact {
		slog.Warn("precision loss in DownsideStdDev float64 conversion", "variance", variance.String())
	}
	return decimal.NewFromFloat(math.Sqrt(f))
}

// Covariance calculates the covariance between two decimal slices.
func Covariance(a, b []decimal.Decimal) decimal.Decimal {
	n := min(len(a), len(b))
	if n < 2 {
		return decimal.Zero
	}

	meanA := Mean(a[:n])
	meanB := Mean(b[:n])

	sum := decimal.Zero
	for i := range n {
		sum = sum.Add(a[i].Sub(meanA).Mul(b[i].Sub(meanB)))
	}

	return sum.Div(decimal.NewFromInt(int64(n - 1)))
}

// Median calculates the median of a decimal slice.
func Median(values []decimal.Decimal) decimal.Decimal {
	if len(values) == 0 {
		return decimal.Zero
	}

	sorted := make([]decimal.Decimal, len(values))
	copy(sorted, values)

	// Simple insertion sort for small slices
	for i := 1; i < len(sorted); i++ {
		key := sorted[i]
		j := i - 1
		for j >= 0 && sorted[j].GreaterThan(key) {
			sorted[j+1] = sorted[j]
			j--
		}
		sorted[j+1] = key
	}

	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return sorted[mid-1].Add(sorted[mid]).Div(decimal.NewFromInt(2))
	}
	return sorted[mid]
}

// NormalQuantile approximates the normal distribution quantile (inverse CDF).
func NormalQuantile(p float64) float64 {
	// Rational approximation (Abramowitz and Stegun 26.2.23)
	if p <= 0 || p >= 1 {
		return 0
	}
	if p < 0.5 {
		return -rationalApprox(math.Sqrt(-2.0 * math.Log(p)))
	}
	return rationalApprox(math.Sqrt(-2.0 * math.Log(1-p)))
}

func rationalApprox(t float64) float64 {
	c := []float64{2.515517, 0.802853, 0.010328}
	d := []float64{1.432788, 0.189269, 0.001308}
	return t - (c[0]+c[1]*t+c[2]*t*t)/(1.0+d[0]*t+d[1]*t*t+d[2]*t*t*t)
}
