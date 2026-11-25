// Package bitdepth models monotone bit-depth promotion as constrained maps.
//
// The canonical source depth in this task is 6 bits (64 codes, maximum code
// 63).  A delta-bit promotion has target depth 6+delta, M=2^(6+delta) codes and
// maximum target code J=M-1.
package bitdepth

import "math"

const (
	SourceBits   = 6
	SourceLevels = 1 << SourceBits  // 64
	MaxSource    = SourceLevels - 1 // 63
	DefaultPeak  = 32
)

// TargetMax returns J=2^(6+delta)-1.
func TargetMax(delta int) int {
	return 1<<(SourceBits+delta) - 1
}

// Scale is the full-range continuous slope alpha=J/63.
func Scale(delta int) float64 {
	return float64(TargetMax(delta)) / float64(MaxSource)
}

// GapAmplification is the shift-map pitch multiplier 2^delta.
func GapAmplification(delta int) int {
	return 1 << delta
}

// ShiftMap applies the conventional left-bit-shift promotion k << delta.
func ShiftMap(delta, k int) int {
	return k << delta
}

// PeakAnchor is the nearest target bin to the normalized full-range position
// of source peak p.
func PeakAnchor(delta, p int) int {
	return int(math.Floor(Scale(delta)*float64(p) + 0.5))
}

// OptimalMap is the nearest-integer full-range map
// round((2^(6+delta)-1)k/63).  It minimizes the coordinate RMSE to the
// continuous full-range line while remaining strictly monotone for delta>=1.
func OptimalMap(delta, k int) int {
	j := TargetMax(delta)
	y := int(math.Floor(Scale(delta)*float64(k) + 0.5))
	if k == 0 {
		y = 0
	}
	if k == MaxSource {
		y = j
	}
	return y
}

// ShiftEndpointShortfall is J-S(63)=2^delta-1.
func ShiftEndpointShortfall(delta int) int {
	return TargetMax(delta) - ShiftMap(delta, MaxSource)
}

// ShiftPeakOffset returns S(p)-PeakAnchor(delta,p).
func ShiftPeakOffset(delta, p int) int {
	return ShiftMap(delta, p) - PeakAnchor(delta, p)
}

// PointMapZeroRatio is the invariant fraction of empty target bins for an
// injective scalar map from K occupied source bins.
func PointMapZeroRatio(occupiedBins, delta int) float64 {
	targetLevels := 1 << (SourceBits + delta)
	m := float64(targetLevels)
	return (m - float64(occupiedBins)) / m
}

// CoordinateRMSE measures map deviation from the continuous full-range line.
func CoordinateRMSE(delta int, useOptimal bool) float64 {
	alpha := Scale(delta)
	var sum float64
	for k := 0; k < SourceLevels; k++ {
		var y int
		if useOptimal {
			y = OptimalMap(delta, k)
		} else {
			y = ShiftMap(delta, k)
		}
		e := float64(y) - alpha*float64(k)
		sum += e * e
	}
	return math.Sqrt(sum / SourceLevels)
}

// BalancedCumulative returns integer cumulative positions for n segments with
// total span total.  Consecutive differences are floor(total/n) or
// ceil(total/n), the convex-cost optimum for interpolation gaps.
func BalancedCumulative(total, n int) []int {
	if n <= 0 {
		return []int{total}
	}
	out := make([]int, n+1)
	for i := 0; i <= n; i++ {
		out[i] = total * i / n
	}
	return out
}

// NormalizedShiftedPeakBias returns the normalized-coordinate bias
// S(p)/J - p/63 = -p(2^delta-1)/(63J).
func NormalizedShiftedPeakBias(delta, p int) float64 {
	q := float64(GapAmplification(delta))
	j := float64(TargetMax(delta))
	return -float64(p) * (q - 1) / (float64(MaxSource) * j)
}
