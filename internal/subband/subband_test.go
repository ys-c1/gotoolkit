package subband

import (
	"math"
	"testing"
)

func testSetup(t *testing.T) *Setup {
	t.Helper()
	w, h, pix := SourceImage(128)
	s, err := NewSetup(pix, w, h, 3)
	if err != nil {
		t.Fatalf("NewSetup: %v", err)
	}
	return s
}

// 正交归一 Haar 必须近似完全重构：原始系数正/逆变换回像素的误差接近机器精度。
func TestHaarPerfectReconstruction(t *testing.T) {
	s := testSetup(t)
	lin, err := Reconstruct(s.Bands, s.W, s.H, s.Levels)
	if err != nil {
		t.Fatal(err)
	}
	var maxErr float64
	for i, v := range lin {
		if e := math.Abs(v - float64(s.Pix[i])); e > maxErr {
			maxErr = e
		}
	}
	if maxErr > 1e-6 {
		t.Fatalf("完全重构失败，最大误差 %g", maxErr)
	}
	back := ClampPixels(lin)
	diff := 0
	for i := range back {
		if back[i] != s.Pix[i] {
			diff++
		}
	}
	if diff != 0 {
		t.Fatalf("取整后仍有 %d/%d 像素与原图不同", diff, len(back))
	}
}

// Parseval：按 n_i/N 加权的子带 MSE 之和必须等于浮点像素域 MSE。
func TestParsevalWeightedMSEEqualsPixel(t *testing.T) {
	s := testSetup(t)
	res, err := s.RunBits(EqualBits(len(s.Bands), 3), 5)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(res.WeightedCoefMSE-res.LinMSE) > 1e-6*math.Max(1, res.LinMSE) {
		t.Fatalf("Parseval 不成立: 加权系数MSE=%g 像素线性MSE=%g",
			res.WeightedCoefMSE, res.LinMSE)
	}
}

// 连续反向注水必须把预算用满（闭式水位），且活动带满足 σ²>θ。
func TestWaterfillUsesBudgetAndKKT(t *testing.T) {
	s := testSetup(t)
	const B = 16384
	wr := Waterfill(s.Stats, B)
	if math.Abs(wr.ContinuousSum-float64(B)) > 1e-6 {
		t.Fatalf("连续解总比特 %g != 预算 %d", wr.ContinuousSum, B)
	}
	active := map[int]bool{}
	for _, i := range wr.Active {
		active[i] = true
		if s.Stats[i].Var <= wr.Theta {
			t.Fatalf("活动带 %s 方差 %g 未超过水位 %g", s.Stats[i].Label, s.Stats[i].Var, wr.Theta)
		}
	}
	for i, st := range s.Stats {
		if !active[i] && wr.Theta > 0 && st.Var > wr.Theta*1.0001 {
			t.Fatalf("非活动带 %s 方差 %g 高于水位 %g 却 b*=0", st.Label, st.Var, wr.Theta)
		}
	}
}

// 整数分配必须严格不超预算；exact 且预算可行时必须用满。
func TestIntegerAllocationBudget(t *testing.T) {
	s := testSetup(t)
	for _, B := range []int{1024, 2048, 4096, 16384, 32768} {
		dp := s.BestIntegerMeasured(B, false)
		if dp.Used > B {
			t.Fatalf("B=%d 用了 %d，超预算", B, dp.Used)
		}
		for _, b := range dp.Bits {
			if b < 0 || b > MaxBitsPerCoef {
				t.Fatalf("非法比特数 %d", b)
			}
		}
	}
	dp := s.BestIntegerMeasured(16384, true)
	if dp.Used != 16384 {
		t.Fatalf("exact B=N 应用满，实际 %d", dp.Used)
	}
}

// 核心结论：同体积（B=N，文件字节数相同）下，注水整数解的实测加权/像素误差
// 必须显著低于平分（每系数 1bit）。留足余量防止数值抖动。
func TestWaterfillBeatsEqualSameVolume(t *testing.T) {
	s := testSetup(t)
	const B = 16384
	wf := s.BestIntegerMeasured(B, true)
	eq := EqualBits(len(s.Bands), 1)
	if TotalBits(s.NS, wf.Bits) != TotalBits(s.NS, eq) {
		t.Fatalf("体积不同: %d vs %d", TotalBits(s.NS, wf.Bits), TotalBits(s.NS, eq))
	}
	rw, err := s.RunBits(wf.Bits, 5)
	if err != nil {
		t.Fatal(err)
	}
	re, err := s.RunBits(eq, 5)
	if err != nil {
		t.Fatal(err)
	}
	if StreamBytes(len(s.Bands), rw.Used) != StreamBytes(len(s.Bands), re.Used) {
		t.Fatal("两条流文件字节数不同")
	}
	if !(rw.MSE < 0.5*re.MSE) {
		t.Fatalf("注水误差未显著更低: wf=%.1f eq=%.1f", rw.MSE, re.MSE)
	}
	if !(rw.PSNR > re.PSNR+3) {
		t.Fatalf("PSNR 优势不足 3dB: wf=%.2f eq=%.2f", rw.PSNR, re.PSNR)
	}
}

// 纯方差模型整数反向注水（题面方法的直接落地）同样必须在同体积下显著优于平分。
func TestVarianceIntegerWaterfillBeatsEqual(t *testing.T) {
	s := testSetup(t)
	const B = 16384
	mv := s.BestInteger(B, true)
	if mv.Used != B {
		t.Fatalf("方差整数解应用满预算，实际 %d", mv.Used)
	}
	eq := EqualBits(len(s.Bands), 1)
	rm, err := s.RunBits(mv.Bits, 5)
	if err != nil {
		t.Fatal(err)
	}
	re, err := s.RunBits(eq, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !(rm.MSE < 0.8*re.MSE) {
		t.Fatalf("方差注水解未稳定优于平分: mv=%.1f eq=%.1f", rm.MSE, re.MSE)
	}
}

// 紧预算下最低方差的 HH1 必须被整带截断（0bit），且结果确定。
func TestTightBudgetTruncatesQuietestBand(t *testing.T) {
	s := testSetup(t)
	dp := s.BestIntegerMeasured(1024, false)
	if dp.Used > 1024 {
		t.Fatalf("超预算 %d>1024", dp.Used)
	}
	hh1 := len(s.Bands) - 1 // 提取顺序最后一个是最细 HH1
	if s.Bands[hh1].Label != "HH1" {
		t.Fatalf("测试假设错误，最后一带是 %s", s.Bands[hh1].Label)
	}
	if dp.Bits[hh1] != 0 {
		t.Fatalf("极紧预算下 HH1 应被截断，实际 %d bit", dp.Bits[hh1])
	}
	z1 := TruncatedBands(dp.Bits)
	dp2 := s.BestIntegerMeasured(1024, false)
	if len(z1) != len(TruncatedBands(dp2.Bits)) {
		t.Fatal("分配不确定")
	}
}

// 字节上限截断必须满足 cap 且按优先级末端丢带。
func TestTruncateForCap(t *testing.T) {
	s := testSetup(t)
	bits := s.BestIntegerMeasured(16384, true).Bits
	prio := PriorityByMarginalValue(s)
	nb, dropped, size, err := TruncateForCap(s, bits, prio, 1500)
	if err != nil {
		t.Fatal(err)
	}
	if size > 1500 {
		t.Fatalf("截断后 %d 字节仍超 1500", size)
	}
	if len(dropped) == 0 {
		t.Fatal("应当截掉至少一个带")
	}
	// 总比特必须下降，且每个被点名的带确实被置 0。
	for _, i := range dropped {
		if nb[i] != 0 {
			t.Fatalf("被截带 %d 仍有 %d bit", i, nb[i])
		}
	}
	if TotalBits(s.NS, nb) >= TotalBits(s.NS, bits) {
		t.Fatal("截断后比特未下降")
	}
}

// 两类失真的能量占比必须合法且合计为 1。
func TestArtifactSharesValid(t *testing.T) {
	s := testSetup(t)
	res, err := s.RunBits(EqualBits(len(s.Bands), 2), 5)
	if err != nil {
		t.Fatal(err)
	}
	a := res.Art
	if a.EnergyLowShare < 0 || a.EnergyLowShare > 1 || a.EnergyHighShare < 0 || a.EnergyHighShare > 1 {
		t.Fatalf("能量占比越界: low=%g high=%g", a.EnergyLowShare, a.EnergyHighShare)
	}
	if math.Abs(a.EnergyLowShare+a.EnergyHighShare-1) > 1e-9 {
		t.Fatalf("两类能量占比之和=%g !=1", a.EnergyLowShare+a.EnergyHighShare)
	}
	if a.SeamHitPct < 0 || a.SeamHitPct > 100 || a.BurrPrevalence < 0 || a.BurrPrevalence > 100 {
		t.Fatal("检出率越界")
	}
}

// 打包长度必须与 StreamBytes 一致，且同体积两方案字节数相同。
func TestPackLength(t *testing.T) {
	s := testSetup(t)
	wfBits := s.BestIntegerMeasured(16384, true).Bits
	r, err := s.RunBits(wfBits, 5)
	if err != nil {
		t.Fatal(err)
	}
	p := Pack(s, r, PriorityByMarginalValue(s))
	if len(p) != StreamBytes(len(s.Bands), r.Used) {
		t.Fatalf("打包长度 %d != 预计 %d", len(p), StreamBytes(len(s.Bands), r.Used))
	}
}
