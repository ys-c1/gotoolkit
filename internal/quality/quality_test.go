package quality

import "testing"

func TestShiftForThreshold(t *testing.T) {
	cases := [][2]int{
		{0, 0},
		{1, 1},
		{2, 2}, {3, 2},
		{4, 3}, {7, 3},
		{8, 4}, {15, 4},
		{16, 5}, {31, 5},
		{32, 6}, {63, 6},
		{64, 7}, {127, 7}, {255, 7},
	}
	for _, c := range cases {
		if got := ShiftForThreshold(c[0]); got != c[1] {
			t.Errorf("ShiftForThreshold(%d)=%d, 想 %d", c[0], got, c[1])
		}
	}
}

func TestErrorBoundAndBitDepth(t *testing.T) {
	for s := 1; s <= 7; s++ {
		if got, want := ErrorBound(s), 1<<(s-1); got != want {
			t.Fatalf("ErrorBound(%d)=%d want %d", s, got, want)
		}
		if got, want := BitDepth(s), 8-s; got != want {
			t.Fatalf("BitDepth(%d)=%d want %d", s, got, want)
		}
	}
	if ErrorBound(0) != 0 {
		t.Fatal("s=0 必须无损、上界 0")
	}
}

func TestQuantizeMeetsBound(t *testing.T) {
	vals := make([]uint8, 256)
	for i := range vals {
		vals[i] = uint8(i)
	}
	for s := 0; s <= 7; s++ {
		q := Quantize(vals, s)
		d := Compare(vals, q)
		if d.MaxAbs > ErrorBound(s) {
			t.Fatalf("s=%d 实测最大误差 %d 超过上界 %d", s, d.MaxAbs, ErrorBound(s))
		}
		if s == 0 && d.MaxAbs != 0 {
			t.Fatal("s=0 必须逐值恒等")
		}
	}
}

func TestFormulaSizes(t *testing.T) {
	w, h := 64, 48
	pix := Pattern(w, h)
	if got := FormulaPGMSize(w, h); got != len("P5\n64 48\n255\n")+w*h {
		t.Fatalf("pgm 公式体积错误: %d", got)
	}
	if got, want := FormulaBMPSize(3, 2), 54+((3*3+3)&^3)*2; got != want {
		t.Fatalf("bmp 公式体积 %d want %d", got, want)
	}
	if FormulaRLESize(pix) != 8+2*CountRLEChunks(pix) {
		t.Fatal("rle 公式体积不一致")
	}
}

func TestAnalyzeTierSelectionAndTightness(t *testing.T) {
	for _, tau := range []int{0, 2, 4, 8, 16, 32} {
		for i, sz := range DefaultSizes {
			w, h := sz[0], sz[1]
			src := Pattern(w, h)
			tr, err := AnalyzeTier(w, h, src, tau, "档")
			if err != nil {
				t.Fatalf("tau=%d size=%v: %v", tau, sz, err)
			}
			if !tr.BoundHolds {
				t.Fatalf("tau=%d %s 有档位实测误差超过闭式上界", tau, tr.Dims())
			}
			if !tr.AllPlannedEligible {
				t.Fatalf("tau=%d %s 反解点不在阈值内", tau, tr.Dims())
			}
			if !tr.ReconIdentical {
				t.Fatalf("tau=%d %s 三格式重建像素不一致", tau, tr.Dims())
			}
			// 规范样本含纯 0 像素，上界必须严格取到。
			if tr.PlanBound != tr.Formats[0].Planned.Measured.MaxAbs {
				t.Fatalf("tau=%d %s 误差未取到上界: bound=%d measured=%d",
					tau, tr.Dims(), tr.PlanBound, tr.Formats[0].Planned.Measured.MaxAbs)
			}
			// 每个格式：公式精确、选中点合格、且体积等于全档最小值。
			minAll := map[string]int{}
			for _, f := range tr.Formats {
				if !f.Planned.FormulaExact {
					t.Fatalf("tau=%d %s %s 公式体积不精确", tau, tr.Dims(), f.Format)
				}
				if !f.Chosen.Eligible || f.Chosen.Measured.MaxAbs > tau {
					t.Fatalf("tau=%d %s %s 选中点不合格", tau, tr.Dims(), f.Format)
				}
				if f.MinBytesConfirmed != true {
					t.Fatalf("tau=%d %s %s 最小性未确认", tau, tr.Dims(), f.Format)
				}
				minAll[f.Format] = f.Chosen.Bytes
			}
			// 选中格式必须是三者中体积最小者。
			wantMin := 1 << 30
			for _, b := range minAll {
				if b < wantMin {
					wantMin = b
				}
			}
			if tr.SelectedBytes != wantMin {
				t.Fatalf("tau=%d 档%d 选中体积 %d 不是全局最小 %d", tau, i+1, tr.SelectedBytes, wantMin)
			}
			if minAll[tr.SelectedFormat] != tr.SelectedBytes {
				t.Fatal("选中格式与体积未成对对应")
			}
		}
	}
}

func TestCanonicalTwoThresholdsConsistent(t *testing.T) {
	rep, err := RunCanonical([]int{4, 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Sizes) != 5 {
		t.Fatalf("应当有五档尺寸，实际 %d", len(rep.Sizes))
	}
	if rep.Consistency == nil || len(rep.Consistency.Items) != 5 {
		t.Fatal("一致性对照必须覆盖五档")
	}
	if !rep.Consistency.FormatsAgree || !rep.Consistency.PairsAgree {
		t.Fatalf("τ=4 与 τ=7 必须得到完全一致的格式+体积对: %+v", rep.Consistency)
	}
	// 跨离散档（4 vs 8）格式结论也应一致（本样本的稳健区间）。
	r4, _ := RunThresholdOn(CanonicalImages(DefaultSizes), 4)
	r8, _ := RunThresholdOn(CanonicalImages(DefaultSizes), 8)
	c := CompareRuns(r4, r8)
	if !c.FormatsAgree {
		t.Fatal("τ=4 与 τ=8 的选中格式应当一致")
	}
}

func TestAnalyzeTierRejectsBadInput(t *testing.T) {
	if _, err := AnalyzeTier(0, 1, nil, 4, "x"); err == nil {
		t.Fatal("零宽应报错")
	}
	if _, err := AnalyzeTier(2, 2, make([]uint8, 3), 4, "x"); err == nil {
		t.Fatal("像素数不匹配应报错")
	}
	if _, err := AnalyzeTier(2, 2, make([]uint8, 4), 999, "x"); err == nil {
		t.Fatal("阈值越界应报错")
	}
}
