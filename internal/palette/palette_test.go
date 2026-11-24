package palette

import (
	"errors"
	"math"
	"testing"
)

func TestDeriveThreshold(t *testing.T) {
	cases := []struct {
		k    int
		want float64
	}{
		{1, 256 * 256 / 12.0},
		{4, 64 * 64 / 12.0},
		{8, 32 * 32 / 12.0},
		{16, 16 * 16 / 12.0},
		{64, 4 * 4 / 12.0},
		{256, 1.0 / 12.0},
	}
	for _, c := range cases {
		got, err := DeriveThreshold(c.k)
		if err != nil {
			t.Fatalf("K=%d: %v", c.k, err)
		}
		if math.Abs(got-c.want) > 1e-12 {
			t.Fatalf("K=%d τ=%v, want %v", c.k, got, c.want)
		}
	}
	// τ 随 K 单调递减。
	prev := math.Inf(1)
	for k := 1; k <= MaxBuckets; k++ {
		v, err := DeriveThreshold(k)
		if err != nil {
			t.Fatal(err)
		}
		if v >= prev {
			t.Fatalf("τ 未随 K 递减: K=%d v=%v prev=%v", k, v, prev)
		}
		prev = v
	}
	for _, k := range []int{0, -1, 257, 1000} {
		if _, err := DeriveThreshold(k); !errors.Is(err, ErrBuckets) {
			t.Fatalf("K=%d 应返回 ErrBuckets, got %v", k, err)
		}
	}
}

func TestBucketErrors(t *testing.T) {
	pix := []uint8{0, 1, 2, 3}
	for _, k := range []int{0, -1, 257} {
		if _, err := Quantize(pix, Options{Buckets: k}); !errors.Is(err, ErrBuckets) {
			t.Fatalf("K=%d 应返回 ErrBuckets, got %v", k, err)
		}
	}
	for _, th := range []float64{-1, -0.001, math.NaN(), math.Inf(1), math.Inf(-1), VarianceCeiling + 1} {
		if _, err := Quantize(pix, Options{Buckets: 4, VarianceThreshold: th}); !errors.Is(err, ErrThreshold) {
			t.Fatalf("τ=%v 应返回 ErrThreshold, got %v", th, err)
		}
	}
	if _, err := Quantize(nil, Options{Buckets: 4}); err == nil {
		t.Fatal("空像素应报错")
	}
}

func TestConstantImage(t *testing.T) {
	res, err := Quantize([]uint8{7, 7, 7, 7, 7}, Options{Buckets: 8})
	if err != nil {
		t.Fatal(err)
	}
	if res.Actual != 1 || res.Stop != StopNoSplittable {
		t.Fatalf("纯色应为 1 桶且不可切: actual=%d stop=%v", res.Actual, res.Stop)
	}
	if res.MSE != 0 {
		t.Fatalf("纯色 MSE 应为 0, got %v", res.MSE)
	}
	if len(res.Palette) != 1 || res.Palette[0] != 7 {
		t.Fatalf("调色板应为 [7], got %v", res.Palette)
	}
}

// 量化后像素只能取调色板里的值。
func TestPixelsFromPalette(t *testing.T) {
	pix := make([]uint8, 256)
	for i := range pix {
		pix[i] = uint8(i)
	}
	res, err := Quantize(pix, Options{Buckets: 8})
	if err != nil {
		t.Fatal(err)
	}
	onPalette := make(map[uint8]bool)
	for _, p := range res.Palette {
		onPalette[p] = true
	}
	for i, v := range res.Pix {
		if !onPalette[v] {
			t.Fatalf("像素 %d=%d 不在调色板", i, v)
		}
	}
}

// MSE ≤ W+1/4 且 ≤ ParamBound，两档桶数各验一次。
func TestMSEBound(t *testing.T) {
	pix := make([]uint8, 4096)
	x := uint32(12345)
	for i := range pix {
		x = x*1664525 + 1013904223
		pix[i] = uint8(x >> 24)
	}
	for _, k := range []int{8, 32} {
		res, err := Quantize(pix, Options{Buckets: k})
		if err != nil {
			t.Fatal(err)
		}
		if res.MSE > res.MSEBound+1e-9 {
			t.Fatalf("K=%d MSE=%v 超过 W+1/4=%v", k, res.MSE, res.MSEBound)
		}
		if res.MSE > res.ParamBound+1e-9 {
			t.Fatalf("K=%d MSE=%v 超过参数上界=%v", k, res.MSE, res.ParamBound)
		}
		// τ 停止时参数上界应为 τ+1/4。
		if res.Stop == StopThreshold {
			want := res.Threshold + 0.25
			if math.Abs(res.ParamBound-want) > 1e-12 {
				t.Fatalf("K=%d 阈值停止上界=%v want %v", k, res.ParamBound, want)
			}
		}
	}
}

// 切分维判定可复算：每个桶的 Axis 等于其 Variance 的 argmax（并列取最小下标）。
func TestAxisRecomputable(t *testing.T) {
	pix := make([]uint8, 512)
	x := uint32(99)
	for i := range pix {
		x = x*1103515245 + 12345
		pix[i] = uint8(x >> 16)
	}
	res, err := Quantize(pix, Options{Buckets: 16})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range res.Buckets {
		if got := AxisOf(b.Variance); got != b.Axis {
			t.Fatalf("桶 Axis=%d 与 argmax=%d 不一致", b.Axis, got)
		}
		if b.Axis != 0 {
			t.Fatalf("灰度桶切分维应为 0, got %d", b.Axis)
		}
	}
	for _, s := range res.Splits {
		if s.Axis != 0 {
			t.Fatalf("灰度切分维应为 0, got %d", s.Axis)
		}
		if s.LeftN <= 0 || s.RightN <= 0 {
			t.Fatalf("切分后两侧必须非空: %+v", s)
		}
	}
}

func TestAxisOfTieTakesLowest(t *testing.T) {
	if got := AxisOf([]float64{4, 4, 1}); got != 0 {
		t.Fatalf("并列应取最小下标, got %d", got)
	}
	if got := AxisOf([]float64{1, 9, 9}); got != 1 {
		t.Fatalf("应取最大方差下标 1, got %d", got)
	}
}

// 同一输入两次量化逐像素一致（确定性）。
func TestDeterministic(t *testing.T) {
	pix := make([]uint8, 1000)
	x := uint32(7)
	for i := range pix {
		x = x*1664525 + 1013904223
		pix[i] = uint8(x >> 20)
	}
	a, err := Quantize(pix, Options{Buckets: 12})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Quantize(pix, Options{Buckets: 12})
	if err != nil {
		t.Fatal(err)
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			t.Fatalf("像素 %d 两次不一致 %d vs %d", i, a.Pix[i], b.Pix[i])
		}
	}
}

// 手动阈值覆盖：阈值足够大时首个桶即停止，实际桶数为 1。
func TestManualThresholdStopsEarly(t *testing.T) {
	pix := make([]uint8, 64)
	for i := range pix {
		pix[i] = uint8(i * 4)
	}
	res, err := Quantize(pix, Options{Buckets: 16, VarianceThreshold: VarianceCeiling})
	if err != nil {
		t.Fatal(err)
	}
	if res.Actual != 1 || res.Stop != StopThreshold {
		t.Fatalf("高阈值应停在 1 桶: actual=%d stop=%v", res.Actual, res.Stop)
	}
	if res.Threshold != VarianceCeiling {
		t.Fatalf("应使用手动阈值, got %v", res.Threshold)
	}
}

// 重复值退化：值域中大量重复时切分仍给出两侧非空。
func TestRepeatedValues(t *testing.T) {
	pix := make([]uint8, 100)
	for i := range pix {
		if i < 90 {
			pix[i] = 10
		} else {
			pix[i] = 200
		}
	}
	res, err := Quantize(pix, Options{Buckets: 8})
	if err != nil {
		t.Fatal(err)
	}
	if res.Actual != 2 {
		t.Fatalf("两组值应切成 2 桶, got %d", res.Actual)
	}
	if res.MSE != 0 {
		t.Fatalf("两组精确值应无误差, got %v", res.MSE)
	}
}
