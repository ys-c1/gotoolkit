package quantfb

import (
	"math"
	"testing"
)

// TestOriginalKernelPreserved 锁定 A 组原核底稿，防止“改完把原核删了/改值”。
func TestOriginalKernelPreserved(t *testing.T) {
	k := OriginalFloydSteinberg()
	if k.Div != 16 {
		t.Fatalf("FS 归一化分母必须是 16，得到 %v", k.Div)
	}
	want := map[[2]int]float64{
		{1, 0}: 7, {-1, 1}: 3, {0, 1}: 5, {1, 1}: 1,
	}
	if len(k.Taps) != 4 {
		t.Fatalf("FS 必须保持 4 个抽头，得到 %d", len(k.Taps))
	}
	var sum float64
	for _, tap := range k.Taps {
		w, ok := want[[2]int{tap.DX, tap.DY}]
		if !ok || w != tap.W {
			t.Fatalf("FS 抽头被改动: (%d,%d)=%v", tap.DX, tap.DY, tap.W)
		}
		sum += tap.W
	}
	if sum != 16 {
		t.Fatalf("FS 权重和应为 16，得到 %v", sum)
	}
}

// TestCoefficientClosedForm 锁定两个系数的高精度数值与 DC 置零性质。
func TestCoefficientClosedForm(t *testing.T) {
	gotT := TStar(Cutoff)
	if math.Abs(gotT-0.8226665268767702) > 1e-12 {
		t.Fatalf("t* = %.15f 偏离冻结值", gotT)
	}
	c1, c2 := JointCoeffs()
	if math.Abs(c1-(-1.8226665268767702)) > 1e-12 {
		t.Fatalf("c1 = %.15f", c1)
	}
	if math.Abs(c2-0.8226665268767702) > 1e-12 {
		t.Fatalf("c2 = %.15f", c2)
	}
	if math.Abs(1+c1+c2) > 1e-12 {
		t.Fatalf("NTF 必须在 DC 置零，1+c1+c2=%e", 1+c1+c2)
	}
	// 与数值积分版带能目标一致：t* 应是 DC 零射线上的极小点。
	j0 := bandOnRay(gotT)
	for _, dt := range []float64{-0.05, -0.01, 0.01, 0.05} {
		if bandOnRay(gotT+dt) < j0-1e-12 {
			t.Fatalf("t* 不是带能极小点: J(t*%.4f) 更低", dt)
		}
	}
}

func bandOnRay(t float64) float64 {
	return BandPower(-(1 + t), t, Cutoff)
}

// TestAlternativeCoeffConvention 锁定无约束 Gram 解，并验证两种口径
// 在 π/4 下 L1 相同 ⇒ 同一个 q*=10 与同一条误差界。
func TestAlternativeCoeffConvention(t *testing.T) {
	h1, h2 := UnconstrainedCoeffs(Cutoff)
	if math.Abs(h1-(-1.727056068622307)) > 1e-9 {
		t.Fatalf("h1=%.12f", h1)
	}
	if math.Abs(h2-0.918276985131228) > 1e-9 {
		t.Fatalf("h2=%.12f", h2)
	}
	c1, c2 := JointCoeffs()
	if math.Abs(CoeffL1(h1, h2)-CoeffL1(c1, c2)) > 1e-12 {
		t.Fatalf("两解 L1 应相同: %.15f vs %.15f", CoeffL1(h1, h2), CoeffL1(c1, c2))
	}
	if StepForCoeffs(h1, h2) != 10 {
		t.Fatalf("无约束解对应步长也应为 10，得到 %d", StepForCoeffs(h1, h2))
	}
}

// TestAlternationFixedPoint 验证交替迭代收敛到 (10, c*) 且两轮不变。
func TestAlternationFixedPoint(t *testing.T) {
	_, final := FixedPoint()
	if final.Q != 10 {
		t.Fatalf("整数不动点步长应为 10，得到 %d", final.Q)
	}
	c1, c2 := JointCoeffs()
	if math.Abs(final.C1-c1) > 1e-9 || math.Abs(final.C2-c2) > 1e-9 {
		t.Fatalf("不动点系数偏离 c*: %+v", final)
	}
	if final.QNext != 10 {
		t.Fatalf("步长解必须自洽，q'=%d", final.QNext)
	}
	if final.Bound > BudgetD+1e-9 {
		t.Fatalf("不动点界 %.6f 超过预算 %.1f", final.Bound, BudgetD)
	}
	// 从多个初始步长出发，B 系数播种的主轨迹与小步长起点都应到 10/c*。
	for _, q0 := range []int{1, 5, 9, 10} {
		tr := Alternate(q0)
		last := tr[len(tr)-1]
		if last.Q != 10 || math.Abs(last.C1-c1) > 1e-9 {
			t.Fatalf("q0=%d 未收敛到主不动点: %s", q0, last.String())
		}
	}
	// 连续不动点：预算半径恰好切于无约束最优点。
	delta, _, _, _, l1, acap := ContinuousFixedPoint()
	if math.Abs(delta-10.149964202009404) > 1e-9 {
		t.Fatalf("连续 Δ* 偏差: %.12f", delta)
	}
	if math.Abs(acap-l1) > 1e-9 {
		t.Fatalf("连续不动点处 Acap %.9f != L1* %.9f", acap, l1)
	}
}

// TestQuantizerAndOutputBounds 在多种片与随机图上核对 |e|≤q/2 与输出误差界。
func TestQuantizerAndOutputBounds(t *testing.T) {
	c1, c2 := JointCoeffs()
	const q = 10.0
	B := BoundAt(q, c1, c2)
	imgs := []*Gray{Chart(), GammaGradient(), LinearRamp(), FlatField(128), PseudoRandom(97, 61, 7)}
	for ii, img := range imgs {
		res := FeedbackQuantize(img, q, c1, c2, true)
		st := Analyze(res, B)
		if st.QuantMax > q/2+1e-9 {
			t.Fatalf("img%d 量化误差 %.6f 超出 q/2", ii, st.QuantMax)
		}
		if !st.BoundHolds {
			t.Fatalf("img%d 输出 max|d|=%.4f 超出 B=%.6f", ii, st.OutMax, B)
		}
	}
}

// TestErrorIdentity 以数值精度验证未夹取恒等式
// level−x = e + c1 e_{n−1} + c2 e_{n−2}（逐行清零）。
func TestErrorIdentity(t *testing.T) {
	img := Chart()
	c1, c2 := JointCoeffs()
	res := FeedbackQuantize(img, 10, c1, c2, true)
	for y := 0; y < img.H; y++ {
		for x := 0; x < img.W; x++ {
			i := y*img.W + x
			var e1, e2 float64
			if x >= 1 {
				e1 = res.QErr[i-1]
			}
			if x >= 2 {
				e2 = res.QErr[i-2]
			}
			want := res.QErr[i] + c1*e1 + c2*e2
			got := res.Level[i] - res.Xf[i]
			if math.Abs(got-want) > 1e-8 {
				t.Fatalf("恒等式在 (%d,%d) 失效: %.12f vs %.12f", x, y, got, want)
			}
		}
	}
}

// TestMetricsImprove 是题面两条硬指标的回归锁：
// C 的低频占比必须显著低于 A，量化误差 RMSE/max 都不得高于 A。
func TestMetricsImprove(t *testing.T) {
	c1, c2 := JointCoeffs()
	imgs := map[string]*Gray{
		"chart":  Chart(),
		"gamma":  GammaGradient(),
		"linear": LinearRamp(),
		"flat":   FlatField(128),
	}
	for name, img := range imgs {
		a := Analyze(FloydSteinberg(img, nil), -1)
		c := Analyze(FeedbackQuantize(img, 10, c1, c2, true), BoundAt(10, c1, c2))
		if c.LFSharePi4 >= a.LFSharePi4*0.5 {
			t.Fatalf("%s 低频占比下降不足: A=%.4f C=%.5f", name, a.LFSharePi4, c.LFSharePi4)
		}
		if c.QuantRMSE > a.QuantRMSE+1e-9 {
			t.Fatalf("%s 量化 RMSE 上升: A=%.3f C=%.3f", name, a.QuantRMSE, c.QuantRMSE)
		}
		if c.QuantMax > a.QuantMax+1e-9 {
			t.Fatalf("%s 最大量化误差上升: A=%.2f C=%.2f", name, a.QuantMax, c.QuantMax)
		}
		if !c.BoundHolds {
			t.Fatalf("%s C 误差界不成立", name)
		}
	}
	// 主试片要求“明显”：至少 5 倍。
	ch := Chart()
	a := Analyze(FloydSteinberg(ch, nil), -1)
	c := Analyze(FeedbackQuantize(ch, 10, c1, c2, true), BoundAt(10, c1, c2))
	if ratio := a.LFSharePi4 / c.LFSharePi4; ratio < 5 {
		t.Fatalf("chart 低频占比仅下降 %.2f 倍，要求 ≥5", ratio)
	}
}

// TestRowLFShareKnownSignal 用已知正弦核对 DFT 低频占比的量级。
func TestRowLFShareKnownSignal(t *testing.T) {
	const W, H = 256, 4
	d := make([]float64, W*H)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			// k=4 的低频正弦，能量应几乎全部落在 π/4 带内
			d[y*W+x] = math.Sin(2 * math.Pi * 4 * float64(x) / W)
		}
	}
	s := RowLFShare(d, W, H, 1.0/8.0)
	if s < 0.99 {
		t.Fatalf("k=4 正弦的低频占比应≈1，得到 %.4f", s)
	}
	d2 := make([]float64, W*H)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			// k=100 的高频正弦，低频占比应≈0
			d2[y*W+x] = math.Sin(2 * math.Pi * 100 * float64(x) / W)
		}
	}
	if s2 := RowLFShare(d2, W, H, 1.0/8.0); s2 > 1e-6 {
		t.Fatalf("k=100 正弦低频占比应≈0，得到 %.6f", s2)
	}
}
