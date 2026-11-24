package lowbit

import (
	"errors"
	"math"
	"testing"
)

func TestStepInequality(t *testing.T) {
	// 反解不等式：ceil(255/(2^d-1)) ≤ q ≤ 2E，且 q 覆盖满量程。
	for d := 1; d <= MaxBits; d++ {
		qMin := MinStep(d)
		if (1<<uint(d)-1)*qMin < 255 {
			t.Fatalf("d=%d: q_min=%d 覆盖不了 255", d, qMin)
		}
		for e := 0; e <= 255; e++ {
			q := Step(e, d)
			if qMin > 2*e && q < qMin {
				t.Fatalf("d=%d E=%d: q=%d 低于覆盖下界 %d", d, e, q, qMin)
			}
			if qMin <= 2*e && q > 2*e {
				t.Fatalf("d=%d E=%d: q=%d 超出误差上界 2E=%d", d, e, q, 2*e)
			}
			if q < 1 || q > 255 {
				t.Fatalf("d=%d E=%d: q=%d 越界", d, e, q)
			}
			// 最近电平重建的实际误差上界就是 ceil(q/2)。
			p, err := NewParams(d, e, 0)
			if err != nil {
				t.Fatal(err)
			}
			if p.EffErr != (q+1)/2 {
				t.Fatalf("d=%d E=%d: EffErr=%d", d, e, p.EffErr)
			}
		}
	}
}

func TestMaxReconstructError(t *testing.T) {
	// 纯量化（不抖动）时，全 0..255 范围内的重建误差不得超过 EffErr。
	for d := 1; d <= MaxBits; d++ {
		for _, e := range []int{0, 1, 4, 9, 32, 43, 128, 255} {
			p, err := NewParams(d, e, 0)
			if err != nil {
				t.Fatal(err)
			}
			for v := 0; v <= 255; v++ {
				got := p.Reconstruct(v)
				if diff := abs(got - v); diff > p.EffErr {
					t.Fatalf("d=%d E=%d: v=%d -> %d，误差 %d > EffErr %d", d, e, v, got, diff, p.EffErr)
				}
			}
		}
	}
}

func TestFullRangeCovered(t *testing.T) {
	// 最高码必须能重建出 255，最低码重建出 0。
	for d := 1; d <= MaxBits; d++ {
		p, _ := NewParams(d, 0, 0)
		if p.ReconstructCode(p.MaxCode) != 255 {
			t.Fatalf("d=%d: 最高码重建 %d != 255", d, p.ReconstructCode(p.MaxCode))
		}
		if p.ReconstructCode(0) != 0 {
			t.Fatalf("d=%d: 码 0 重建 %d != 0", d, p.ReconstructCode(0))
		}
	}
}

func TestKernelNormalizedToOne(t *testing.T) {
	for name, k := range Builtins() {
		if err := k.Validate(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		sum := 0.0
		for _, v := range k.Normalized() {
			sum += v
		}
		if math.Abs(sum-1) > 1e-12 {
			t.Fatalf("%s: 归一和 %v != 1", name, sum)
		}
		q8, err := k.Q8()
		if err != nil {
			t.Fatal(err)
		}
		qs := 0
		for _, v := range q8 {
			qs += v
		}
		if qs != Q8One {
			t.Fatalf("%s: Q8 和 %d != %d", name, qs, Q8One)
		}
	}
}

func TestZeroWeightKernelRejected(t *testing.T) {
	zero := &Kernel{Name: "zero", N: 3, W: make([]int, 9)}
	if err := zero.Validate(); !errors.Is(err, ErrZeroSum) {
		t.Fatalf("全零核应报 ErrZeroSum，得到 %v", err)
	}
	if _, err := zero.Q8(); !errors.Is(err, ErrZeroSum) {
		t.Fatalf("全零核 Q8 应报 ErrZeroSum，得到 %v", err)
	}
	// 自定义解析路径同样要报。
	if _, err := ParseKernel("0,0,0;0,0,0;0,0,0"); !errors.Is(err, ErrZeroSum) {
		t.Fatalf("解析全零核应报 ErrZeroSum，得到 %v", err)
	}
}

func TestParseKernelRejectsCausal(t *testing.T) {
	if _, err := ParseKernel("7,0,0;0,0,0;0,0,0"); !errors.Is(err, ErrBadKernel) {
		t.Fatalf("引用已处理像素应报 ErrBadKernel，得到 %v", err)
	}
	if _, err := ParseKernel("0,0,7;3,7,5;1,0,0"); !errors.Is(err, ErrBadKernel) {
		t.Fatalf("中心非零应报 ErrBadKernel，得到 %v", err)
	}
}

func TestDepthOutOfRange(t *testing.T) {
	for _, d := range []int{-1, 0, 9, 100} {
		if _, err := NewParams(d, 0, 0); !errors.Is(err, ErrBadBits) {
			t.Fatalf("d=%d 应报 ErrBadBits，得到 %v", d, err)
		}
	}
}

func TestDitherPreservesMean(t *testing.T) {
	// 误差扩散把误差推给邻居，总亮度基本守恒：
	// 只有越界被夹住/出边的误差会丢失，故允许每行最多 q 的总偏移。
	img := &Image{W: 8, H: 8, Pix: make([]uint8, 64)}
	for i := range img.Pix {
		img.Pix[i] = uint8((i * 37) % 256)
	}
	p, _ := NewParams(2, 0, 0)
	k, _ := Builtin("floyd-steinberg")
	plain, _ := quantize(img, p)
	dithered, _ := diffuse(img, p, k, mustQ8(t, k))
	sum := func(b []uint8) int {
		s := 0
		for _, v := range b {
			s += int(v)
		}
		return s
	}
	limit := p.Q * img.H
	if d := abs(sum(dithered) - sum(plain)); d > limit {
		t.Fatalf("抖动后总亮度偏移 %d 超过上限 %d", d, limit)
	}
}

func TestPipelineThumbAndIndices(t *testing.T) {
	img := &Image{W: 5, H: 3, Pix: make([]uint8, 15)}
	for i := range img.Pix {
		img.Pix[i] = uint8(i * 16)
	}
	p, _ := NewParams(3, 0, 0)
	st := Pipeline(img, p, nil, false)
	if st.ThumbW != 2 || st.ThumbH != 1 {
		t.Fatalf("缩略尺寸 %dx%d，期望 2x1", st.ThumbW, st.ThumbH)
	}
	if len(st.IndicesAfter) != 2 {
		t.Fatalf("缩略后索引数 %d，期望 2", len(st.IndicesAfter))
	}
	if len(st.IndicesBefore) != 15 {
		t.Fatalf("缩略前索引数 %d，期望 15", len(st.IndicesBefore))
	}
	for _, ix := range st.IndicesAfter {
		if int(ix) >= len(st.PaletteAfter) {
			t.Fatalf("索引 %d 超出调色板 %d", ix, len(st.PaletteAfter))
		}
	}
	for _, ix := range st.IndicesBefore {
		if int(ix) >= len(st.PaletteBefore) {
			t.Fatalf("索引 %d 超出调色板 %d", ix, len(st.PaletteBefore))
		}
	}
}

func mustQ8(t *testing.T, k *Kernel) []int {
	t.Helper()
	q8, err := k.Q8()
	if err != nil {
		t.Fatal(err)
	}
	return q8
}
