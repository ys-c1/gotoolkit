package sharpen

import (
	"math"
	"testing"
)

func TestGaussianNormalizedAndSymmetric(t *testing.T) {
	for _, k := range CandidateWidths {
		w, err := Gaussian(k)
		if err != nil {
			t.Fatal(err)
		}
		if len(w) != k {
			t.Fatalf("k=%d 长度=%d", k, len(w))
		}
		var s float64
		for i := range w {
			s += w[i]
			if math.Abs(w[i]-w[k-1-i]) > 1e-15 {
				t.Fatalf("k=%d 核不对称: %v", k, w)
			}
		}
		if math.Abs(s-1) > 1e-12 {
			t.Fatalf("k=%d 核和=%.15f", k, s)
		}
		if _, err := Gaussian(4); err == nil {
			t.Fatal("偶核应报错")
		}
		if _, err := Gaussian(0); err == nil {
			t.Fatal("零核应报错")
		}
	}
}

func TestStepDMatchesClosedForm(t *testing.T) {
	// 数值阶跃响应峰值必须与闭式解 D=(1-w_c)/2 一致到 1e-12。
	for _, k := range CandidateWidths {
		w, _ := Gaussian(k)
		cf := (1 - w[k/2]) / 2
		if d := StepD(w); math.Abs(d-cf) > 1e-12 {
			t.Fatalf("k=%d 数值 D=%.15f 闭式 D=%.15f", k, d, cf)
		}
	}
}

func TestClosedFormMatchesSearch(t *testing.T) {
	// 逐像素反解必须与二分搜索一致。
	for _, e := range DefaultEdgeCases() {
		pix := e.Render()
		for _, k := range CandidateWidths {
			w, _ := Gaussian(k)
			cf, _ := ClosedForm(e.Width, e.Height, pix, w)
			got, iters := Search(e.Width, e.Height, pix, w)
			if math.Abs(cf-got) > 1e-9*math.Max(1, cf) {
				t.Fatalf("%s k=%d 闭式=%.12f 搜索=%.12f (iters=%d)", e.Name(), k, cf, got, iters)
			}
			if iters > 200 {
				t.Fatalf("%s k=%d 二分未收敛", e.Name(), k)
			}
		}
	}
}

func TestStepEdgeClosedForm(t *testing.T) {
	// 无纹理的硬台阶上，逐像素闭式解必须等于 H/(C*D_k)：
	// H=min(255-hi,lo)，D_k=(1-w_c)/2。允许 2% 的相对误差（量化与邻域采样）。
	for _, tc := range []struct{ lo, hi int }{{96, 160}, {64, 192}, {32, 224}} {
		pix := make([]uint8, 64*8)
		for y := 0; y < 8; y++ {
			for x := 0; x < 64; x++ {
				v := uint8(tc.lo)
				if x >= 32 {
					v = uint8(tc.hi)
				}
				pix[y*64+x] = v
			}
		}
		for _, k := range CandidateWidths {
			w, _ := Gaussian(k)
			got, _ := ClosedForm(64, 8, pix, w)
			want := ClosedFormStepGain(tc.lo, tc.hi, w)
			if math.Abs(got-want) > 0.02*want {
				t.Fatalf("lo=%d hi=%d k=%d 实测=%.6f 闭式=%.6f", tc.lo, tc.hi, k, got, want)
			}
		}
	}
}

func TestEdgeBoostAtLimit(t *testing.T) {
	// 在最大增益下，最亮边缘像素恰好被推到 255：边缘增幅 = headroom。
	for _, e := range DefaultEdgeCases() {
		pix := e.Render()
		sol, err := Solve(e.Width, e.Height, pix, nil)
		if err != nil {
			t.Fatal(err)
		}
		vals := Unclamped(e.Width, e.Height, pix, sol.W, sol.Gain)
		var mx float64
		for _, v := range vals {
			if v > mx {
				mx = v
			}
		}
		headroom := math.Min(255-float64(e.Hi), float64(e.Lo))
		if math.Abs(sol.Boost-headroom) > 0.5 || math.Abs(mx-255) > 0.5 {
			t.Fatalf("%s k=%d 增幅=%.3f headroom=%.1f max=%.3f", e.Name(), sol.K, sol.Boost, headroom, mx)
		}
	}
}

func TestZeroOvershootAtSolvedGain(t *testing.T) {
	// 解出的增益必须实测零过冲；核宽必须与实际使用的核一致。
	for _, e := range DefaultEdgeCases() {
		pix := e.Render()
		sol, err := Solve(e.Width, e.Height, pix, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(sol.W) != sol.K {
			t.Fatalf("%s 解出 k=%d 但核长=%d", e.Name(), sol.K, len(sol.W))
		}
		reKern, _ := Gaussian(sol.K)
		for i := range reKern {
			if reKern[i] != sol.W[i] {
				t.Fatalf("%s 解出核与按 k 重建的核不一致", e.Name())
			}
		}
		if sol.Clipped != 0 || sol.PeakG > eps {
			t.Fatalf("%s 解出增益 %.6f 下过冲 %d 像素 / %.6g 灰阶", e.Name(), sol.Gain, sol.Clipped, sol.PeakG)
		}
		// 钳位后的实际产物与未钳位输出逐点一致（证明没有截平）。
		out := Apply(e.Width, e.Height, pix, sol.W, sol.Gain)
		vals := Unclamped(e.Width, e.Height, pix, sol.W, sol.Gain)
		for i := range out {
			if math.Abs(float64(out[i])-vals[i]) > 0.5+eps {
				t.Fatalf("%s 像素 %d 被截平: %d vs %.3f", e.Name(), i, out[i], vals[i])
			}
		}
	}
}

func TestOverLimitProducesOvershoot(t *testing.T) {
	// 超过解出的增益 25% 必须出现过冲，且实测过冲像素数为正。
	for _, e := range DefaultEdgeCases() {
		pix := e.Render()
		sol, err := Solve(e.Width, e.Height, pix, nil)
		if err != nil {
			t.Fatal(err)
		}
		vals := Unclamped(e.Width, e.Height, pix, sol.W, sol.Gain*1.25)
		total, count := Clipping(vals)
		if count == 0 || total <= 0 {
			t.Fatalf("%s 1.25x 增益 %.6f 下应过冲，实测 %d 像素 / %.3f", e.Name(), sol.Gain*1.25, count, total)
		}
	}
}

func TestUnsharpenedIsByteIdentical(t *testing.T) {
	// 未锐化（gain=0 或 k=1）必须逐字节保持原样。
	for _, e := range DefaultEdgeCases() {
		pix := e.Render()
		kern, _ := Gaussian(7)
		for _, out := range [][]uint8{
			Apply(e.Width, e.Height, pix, kern, 0),
			Apply(e.Width, e.Height, pix, []float64{1}, 3.5),
		} {
			for i := range pix {
				if out[i] != pix[i] {
					t.Fatalf("%s 像素 %d: %d != %d", e.Name(), i, out[i], pix[i])
				}
			}
		}
	}
}
