package palette

import (
	"errors"
	"math"
	"testing"
)

func ramp() []uint8 {
	pix := make([]uint8, 256*16)
	for y := 0; y < 16; y++ {
		for x := 0; x < 256; x++ {
			pix[y*256+x] = uint8(x)
		}
	}
	return pix
}

func TestQuantizeRampThreeLevels(t *testing.T) {
	pix := ramp()
	cases := []struct {
		k       int
		mse     float64
		mae     float64
		maxAbs  int
		palSize int
	}{
		{4, 341.5, 16, 32, 4},
		{16, 21.5, 4, 8, 16},
		{64, 1.5, 1, 2, 64},
	}
	for _, tc := range cases {
		res, err := Quantize(pix, tc.k)
		if err != nil {
			t.Fatalf("K=%d: %v", tc.k, err)
		}
		if res.Actual != tc.k || len(res.Palette) != tc.palSize {
			t.Fatalf("K=%d actual=%d palette=%d", tc.k, res.Actual, len(res.Palette))
		}
		if math.Abs(res.MSE-tc.mse) > 1e-9 {
			t.Fatalf("K=%d MSE=%v want %v", tc.k, res.MSE, tc.mse)
		}
		if math.Abs(res.MAE-tc.mae) > 1e-9 {
			t.Fatalf("K=%d MAE=%v want %v", tc.k, res.MAE, tc.mae)
		}
		if res.MaxAbs != tc.maxAbs {
			t.Fatalf("K=%d MaxAbs=%d want %d", tc.k, res.MaxAbs, tc.maxAbs)
		}
	}
}

func TestIntermediateSplitOrderPrediction(t *testing.T) {
	pix := ramp()
	for k := 1; k <= 64; k++ {
		res, err := Quantize(pix, k)
		if err != nil {
			t.Fatal(err)
		}
		want, err := UniformRampMSE(k)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(res.MSE-want) > 1e-8 {
			t.Fatalf("K=%d MSE=%v prediction=%v", k, res.MSE, want)
		}
	}
	targetK, err := UniformRampBucketsForMSE(2)
	if err != nil {
		t.Fatal(err)
	}
	if targetK != 60 {
		t.Fatalf("K for MSE<=2 = %d, want 60", targetK)
	}
}

func TestLeftmostOrderChangesCurve(t *testing.T) {
	pix := ramp()
	got, err := QuantizeOrdered(pix, Options{Buckets: 4, Order: OrderLeftmost})
	if err != nil {
		t.Fatal(err)
	}
	want := 789.5
	if math.Abs(got.MSE-want) > 1e-9 {
		t.Fatalf("leftmost K=4 MSE=%v want %v", got.MSE, want)
	}
	variance, err := Quantize(pix, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !(got.MSE > variance.MSE) {
		t.Fatalf("错误的深度优先顺序应抬高误差: left=%v variance=%v", got.MSE, variance.MSE)
	}
}

func TestConstantAndValidation(t *testing.T) {
	res, err := Quantize([]uint8{7, 7, 7, 7}, 8)
	if err != nil {
		t.Fatal(err)
	}
	if res.Actual != 1 || res.MSE != 0 || len(res.Palette) != 1 || res.Palette[0] != 7 {
		t.Fatalf("纯色量化错误: %+v", res)
	}
	for _, k := range []int{0, -1, 257} {
		if _, err := Quantize(ramp(), k); !errors.Is(err, ErrBuckets) {
			t.Fatalf("K=%d should wrap ErrBuckets, got %v", k, err)
		}
	}
	if _, err := Quantize(nil, 4); !errors.Is(err, ErrBuckets) {
		t.Fatalf("empty pixels should wrap ErrBuckets, got %v", err)
	}
}

func TestDeterministicAndPixelsOnPalette(t *testing.T) {
	pix := ramp()
	a, err := Quantize(pix, 16)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Quantize(pix, 16)
	if err != nil {
		t.Fatal(err)
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			t.Fatalf("non-deterministic at %d", i)
		}
	}
	pal := map[uint8]bool{}
	for _, v := range a.Palette {
		pal[v] = true
	}
	for i, v := range a.Pix {
		if !pal[v] {
			t.Fatalf("pixel %d value %d not in palette", i, v)
		}
	}
}
