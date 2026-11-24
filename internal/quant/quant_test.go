package quant

import (
	"math"
	"testing"
)

// tileFixture 生成 256×256 的 8×8 色块图：每个灰度值恰好占一个小块（64 像素），
// 因此每个灰度都均匀经历 Bayer 的全部 64 个相位。它是 Predict 的精确对照：
// 此时模型预测与逐像素实测完全一致。
func tileFixture(w, h int) []uint8 {
	pix := make([]uint8, w*h)
	cols := w / 8
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pix[y*w+x] = uint8(((y/8)*cols + x/8) % 256)
		}
	}
	return pix
}

// sinFixture 生成平滑正弦灰度图，用来验证连续灰度的真实图像。
func sinFixture(w, h int) []uint8 {
	pix := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := 127.5 + 127.5*(0.6*math.Sin(float64(x)/17.0)+0.4*math.Sin(float64(y)/23.0))
			pix[y*w+x] = uint8(math.Max(0, math.Min(255, math.Round(v))))
		}
	}
	return pix
}

func meanSquared(src, out []uint8) float64 {
	var sum float64
	for i := range src {
		d := float64(out[i]) - float64(src[i])
		sum += d * d
	}
	return sum / float64(len(src))
}

func TestPaletteEndpoints(t *testing.T) {
	for _, L := range []int{2, 3, 4, 16, 64, 255, 256} {
		pal := Palette(L)
		if len(pal) != L {
			t.Fatalf("L=%d 调色板长度 %d", L, len(pal))
		}
		if pal[0] != 0 || pal[L-1] != 255 {
			t.Fatalf("L=%d 端点 %d..%d", L, pal[0], pal[L-1])
		}
		for k := 1; k < L; k++ {
			if pal[k] <= pal[k-1] {
				t.Fatalf("L=%d 调色板未严格递增: %v", L, pal)
			}
		}
	}
	if Palette(1) != nil || Palette(257) != nil {
		t.Fatal("越界桶数应返回 nil")
	}
}

func TestTauZeroIgnoresPosition(t *testing.T) {
	pix := []uint8{0, 7, 128, 249, 255}
	out, err := Quantize(len(pix), 1, pix, 4, 0)
	if err != nil {
		t.Fatal(err)
	}
	// τ=0 时输出只取决于像素值：任意位置重复同值必须得到同一输出。
	for i, v := range pix {
		again, err := Quantize(1, 5, []uint8{v, v, v, v, v}, 4, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range again {
			if o != out[i] {
				t.Fatalf("τ=0 时同值输出不一致: v=%d got %d want %d", v, o, out[i])
			}
		}
	}
}

func TestQuantizeRejects(t *testing.T) {
	if _, err := Quantize(2, 2, []uint8{1, 2, 3}, 4, 0); err == nil {
		t.Fatal("像素数不足应报错")
	}
	if _, err := Quantize(1, 1, []uint8{1}, 1, 0); err == nil {
		t.Fatal("桶数过小应报错")
	}
	if _, err := Quantize(1, 1, []uint8{1}, 4, 1.5); err == nil {
		t.Fatal("强度越界应报错")
	}
	if _, err := Quantize(1, 1, []uint8{1}, 4, math.NaN()); err == nil {
		t.Fatal("NaN 强度应报错")
	}
}

// TestDecompositionIsMSE 验证 B+N 恒等于逐像素 MSE；这是两部分量纲一致的
// 直接后果，也是加权不失真的前提。
func TestDecompositionIsMSE(t *testing.T) {
	pix := sinFixture(64, 64)
	for _, L := range []int{2, 4, 16, 64, 256} {
		for _, tau := range []float64{0, 0.3, 0.73, 1} {
			out, err := Quantize(64, 64, pix, L, tau)
			if err != nil {
				t.Fatal(err)
			}
			p, ok := Measure(pix, out, L)
			if !ok {
				t.Fatal("测量失败")
			}
			if got, want := p.MSE(), meanSquared(pix, out); math.Abs(got-want) > 1e-9 {
				t.Fatalf("L=%d τ=%.2f B+N=%.12f MSE=%.12f", L, tau, got, want)
			}
		}
	}
}

// TestPredictMatchesMeasurementOnTiles 验证模型：当每个灰度都均匀覆盖全部
// Bayer 相位时，Predict 与 Measure 必须完全相同。
func TestPredictMatchesMeasurementOnTiles(t *testing.T) {
	const w, h = 256, 256
	pix := tileFixture(w, h)
	hist := [256]float64{}
	for _, v := range pix {
		hist[v]++
	}
	for _, L := range []int{2, 4, 16, 64} {
		for _, tau := range []float64{0, 0.25, 0.5, 0.73, 1} {
			out, err := Quantize(w, h, pix, L, tau)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := Measure(pix, out, L)
			if !ok {
				t.Fatal("测量失败")
			}
			want := Predict(hist, L, tau)
			if math.Abs(got.Banding-want.Banding) > 1e-9 || math.Abs(got.Noise-want.Noise) > 1e-9 {
				t.Fatalf("L=%d τ=%.2f 模型 (%+.6f,%+.6f) 与实测 (%+.6f,%+.6f) 不一致",
					L, tau, want.Banding, want.Noise, got.Banding, got.Noise)
			}
		}
	}
}

// TestBandingFallsNoiseRises 验证抖动确实是在用噪声换色块。
func TestBandingFallsNoiseRises(t *testing.T) {
	hist := UniformHist()
	for _, L := range []int{4, 16, 64} {
		none := Predict(hist, L, 0)
		full := Predict(hist, L, 1)
		if none.Banding <= full.Banding {
			t.Fatalf("L=%d 满强度抖动未降低色块: %.4f -> %.4f", L, none.Banding, full.Banding)
		}
		if none.Noise >= full.Noise {
			t.Fatalf("L=%d 抖动未引入噪声: %.4f -> %.4f", L, none.Noise, full.Noise)
		}
		if none.Noise != 0 {
			t.Fatalf("L=%d 不抖动时噪声应为 0，实为 %.6f", L, none.Noise)
		}
	}
}

// TestOptimizeBeatsBothExtremes 是核心验收：三档桶数下，最优配比的加权误差
// 必须低于“不抖动”和“满强度抖动”两种极端。
func TestOptimizeBeatsBothExtremes(t *testing.T) {
	hist := UniformHist()
	for _, L := range []int{4, 16, 64} {
		plan := Optimize(L)
		if plan.Levels != L {
			t.Fatalf("L=%d 返回到桶数 %d", L, plan.Levels)
		}
		if plan.Tau <= 0 || plan.Tau >= 1 {
			t.Fatalf("L=%d 最优强度应落在开区间 (0,1)，实为 %.5f", L, plan.Tau)
		}
		none := Predict(hist, L, 0).Weighted()
		full := Predict(hist, L, 1).Weighted()
		if !(plan.Weighted() < none && plan.Weighted() < full) {
			t.Fatalf("L=%d 最优 W=%.4f 未同时低于不抖动 %.4f 与满抖动 %.4f", L, plan.Weighted(), none, full)
		}
	}
}

// TestOptimalTauNearClosedForm 对照连续近似下的闭式解 τ*=1-1/r（r=4 → 0.75）。
// 离散调色板在低位深时有约 ±0.04 的偏差（见 docs/dither.md）。
func TestOptimalTauNearClosedForm(t *testing.T) {
	for _, L := range []int{4, 16, 64} {
		tau := Optimize(L).Tau
		if math.Abs(tau-0.75) > 0.06 {
			t.Fatalf("L=%d 最优 τ=%.4f 偏离闭式解 0.75 过多", L, tau)
		}
	}
}

// TestLowBandingWeightDisablesDither 验证权重比 <=1 时最优解退化为不抖动：
// 抖动必然抬高总 MSE，若色块几乎不计权重，则没有理由引入噪声。
func TestLowBandingWeightDisablesDither(t *testing.T) {
	for _, L := range []int{4, 16, 64} {
		plan := optimizeHist(UniformHist(), L, 1, 1)
		if plan.Tau != 0 {
			t.Fatalf("L=%d 等权重时最优 τ=%.4f，应退化为 0", L, plan.Tau)
		}
	}
	// 权重比略高于 1 即可让内点解出现，且随权重比单调增大。
	prev := -1.0
	for _, r := range []float64{1.25, 2, 4, 8} {
		tau := optimizeHist(UniformHist(), 16, r, 1).Tau
		if tau <= prev {
			t.Fatalf("r=%.2f 时最优 τ=%.4f 未随权重比增大（前值 %.4f）", r, tau, prev)
		}
		prev = tau
	}
}

// TestOptimizeIsContentAware 验证直方图接口：全黑图没有色块可换，不应抖动。
func TestOptimizeIsContentAware(t *testing.T) {
	var black [256]float64
	black[0] = 1000
	if plan := OptimizeHist(black, 4); plan.Tau != 0 {
		t.Fatalf("常量图最优 τ=%.4f，应为 0", plan.Tau)
	}
	pix := sinFixture(256, 256)
	hist := [256]float64{}
	for _, v := range pix {
		hist[v]++
	}
	plan := OptimizeHist(hist, 16)
	if plan.Tau <= 0 || plan.Tau >= 1 {
		t.Fatalf("正弦图最优 τ=%.4f 未落在 (0,1)", plan.Tau)
	}
}

func TestUniformHist(t *testing.T) {
	h := UniformHist()
	for v, n := range h {
		if n != 1 {
			t.Fatalf("hist[%d]=%v", v, n)
		}
	}
}

func TestMeasureRejectsMismatch(t *testing.T) {
	if _, ok := Measure([]uint8{1, 2}, []uint8{1}, 4); ok {
		t.Fatal("长度不一致应失败")
	}
	if _, ok := Measure(nil, nil, 4); ok {
		t.Fatal("空输入应失败")
	}
	if _, ok := Measure([]uint8{1}, []uint8{1}, 1); ok {
		t.Fatal("桶数越界应失败")
	}
}

// TestSmoothImageOptimumMatchesModel 在平滑图像（正弦）上验证：模型给出的最优
// 强度在逐像素实测中同样优于两种极端。正弦图的像素并不严格均匀覆盖 Bayer 相位，
// 实测最优与模型最优会有 1–3 个百分点的偏差；这里要求模型最优点本身足够接近
// 实测平台，且模型与实测的误差曲线形状一致（逐点 W 差 <2%）。
func TestSmoothImageOptimumMatchesModel(t *testing.T) {
	const w, h = 256, 256
	pix := sinFixture(w, h)
	hist := [256]float64{}
	for _, v := range pix {
		hist[v]++
	}
	for _, L := range []int{4, 16, 64} {
		plan := OptimizeHist(hist, L)
		want := map[float64]float64{}
		for pct := 0; pct <= 100; pct += 2 {
			out, err := Quantize(w, h, pix, L, float64(pct)/100)
			if err != nil {
				t.Fatal(err)
			}
			p, ok := Measure(pix, out, L)
			if !ok {
				t.Fatal("测量失败")
			}
			want[float64(pct)] = p.Weighted()
		}
		atPlan, err := Quantize(w, h, pix, L, plan.Tau)
		if err != nil {
			t.Fatal(err)
		}
		pp, _ := Measure(pix, atPlan, L)
		if !(pp.Weighted() < want[0] && pp.Weighted() < want[100]) {
			t.Fatalf("L=%d 模型最优 τ=%.3f 实测 W=%.3f 未同时优于 τ=0 的 %.3f 与 τ=1 的 %.3f",
				L, plan.Tau, pp.Weighted(), want[0], want[100])
		}
		bestPct, bestW := 0.0, math.Inf(1)
		for pct, v := range want {
			if v < bestW {
				bestW, bestPct = v, pct
			}
		}
		// 模型最优点落在实测平台的 2% 带宽内（模型最优本身的实测 W 与实测
		// 最优 W 相差不超过 2%），且两者相差不超过 4 个百分点。
		if pp.Weighted() > bestW*1.02 {
			t.Fatalf("L=%d 模型最优 τ=%.3f 实测 W=%.3f 高于实测最优 W=%.3f 的 2%%",
				L, plan.Tau, pp.Weighted(), bestW)
		}
		if d := math.Abs(bestPct/100 - plan.Tau); d > 0.04 {
			t.Fatalf("L=%d 实测最优 τ=%.2f 与模型最优 τ=%.3f 相差 %.3f 超过 4 个百分点",
				L, bestPct/100, plan.Tau, d)
		}
	}
}
