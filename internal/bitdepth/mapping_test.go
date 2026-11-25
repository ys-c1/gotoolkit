package bitdepth

import (
	"math"
	"testing"
)

func TestShiftFailsFullRangeAndAmplifiesGaps(t *testing.T) {
	cases := []struct {
		delta       int
		wantJ       int
		wantShort   int
		wantPeakOff int
	}{
		{1, 127, 1, -1},
		{2, 255, 3, -2},
		{3, 511, 7, -4},
	}
	for _, tc := range cases {
		if got := TargetMax(tc.delta); got != tc.wantJ {
			t.Fatalf("delta=%d J=%d, want %d", tc.delta, got, tc.wantJ)
		}
		if got := GapAmplification(tc.delta); got != 1<<tc.delta {
			t.Fatalf("bad gap amplification: %d", got)
		}
		if got := ShiftMap(tc.delta, 1); got != 1<<tc.delta {
			t.Fatalf("shift pitch = %d, want %d", got, 1<<tc.delta)
		}
		if got := ShiftEndpointShortfall(tc.delta); got != tc.wantShort {
			t.Fatalf("delta=%d shortfall=%d, want %d", tc.delta, got, tc.wantShort)
		}
		if got := ShiftPeakOffset(tc.delta, DefaultPeak); got != tc.wantPeakOff {
			t.Fatalf("delta=%d peak offset=%d, want %d", tc.delta, got, tc.wantPeakOff)
		}
	}
}

func TestOptimalMapIsFullRangeMonotoneAndPeakLocked(t *testing.T) {
	for delta := 1; delta <= 3; delta++ {
		j := TargetMax(delta)
		if got := OptimalMap(delta, 0); got != 0 {
			t.Fatalf("delta=%d f(0)=%d", delta, got)
		}
		if got := OptimalMap(delta, MaxSource); got != j {
			t.Fatalf("delta=%d f(63)=%d, want %d", delta, got, j)
		}
		prev := -1
		for k := 0; k <= MaxSource; k++ {
			got := OptimalMap(delta, k)
			if got <= prev {
				t.Fatalf("delta=%d map not strictly monotone at k=%d: %d <= %d", delta, k, got, prev)
			}
			if got < 0 || got > j {
				t.Fatalf("delta=%d map out of range: %d", delta, got)
			}
			prev = got
		}
		anchor := PeakAnchor(delta, DefaultPeak)
		if got := OptimalMap(delta, DefaultPeak); got != anchor {
			t.Fatalf("delta=%d peak=%d, anchor=%d", delta, got, anchor)
		}
		if ShiftPeakOffset(delta, DefaultPeak) == 0 && delta != 1 {
			t.Fatalf("test expectation changed: shift should move delta=%d peak", delta)
		}
		if off := ShiftPeakOffset(delta, DefaultPeak); off >= 0 {
			t.Fatalf("delta=%d shift peak offset=%d, want negative", delta, off)
		}
	}

	if got := OptimalMap(2, DefaultPeak); got != 130 {
		t.Fatalf("6->8 peak map = %d, want 130", got)
	}
}

func TestOptimalCoordinateRMSEBeatsShift(t *testing.T) {
	for delta := 1; delta <= 3; delta++ {
		s := CoordinateRMSE(delta, false)
		o := CoordinateRMSE(delta, true)
		if !(o < s) {
			t.Fatalf("delta=%d RMSE opt %.4f not below shift %.4f", delta, o, s)
		}
		if o > 0.31 { // nearest-integer RMS for nearly uniform fractional phases
			t.Fatalf("delta=%d optimal RMSE unexpectedly large: %.4f", delta, o)
		}
	}
}

func TestPointMapZeroRatioInvariant(t *testing.T) {
	if got := PointMapZeroRatio(SourceLevels, 2); math.Abs(got-0.75) > 1e-12 {
		t.Fatalf("K=64 delta=2 zero ratio=%v, want .75", got)
	}
	if got := PointMapZeroRatio(19, 2); math.Abs(got-237.0/256.0) > 1e-12 {
		t.Fatalf("K=19 delta=2 zero ratio=%v", got)
	}
}

func TestBalancedCumulativeSpansTotal(t *testing.T) {
	got := BalancedCumulative(255, 63)
	if len(got) != 64 || got[0] != 0 || got[63] != 255 {
		t.Fatalf("bad balanced cumulative endpoints/length: %v", got)
	}
	for i := 1; i < len(got); i++ {
		d := got[i] - got[i-1]
		if d < 4 || d > 5 {
			t.Fatalf("6->8 gap %d out of balanced {4,5}", d)
		}
	}
}
