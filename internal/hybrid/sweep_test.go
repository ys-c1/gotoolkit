package hybrid

import (
	"math"
	"testing"

	"gotoolkit/internal/testimg"
)

// TestSweepModelSingleMinimum 是“总失重在阈值扫描下只有一个极小点”的可执行证明：
// 对每张图，按块 σ 升序逐个并入平坦类，模型总失真关于 k 严格凸（差分单调增），
// 因此只有一个极小点；真实总失真的最小点也必须落在模型极小点上。
func TestSweepModelSingleMinimum(t *testing.T) {
	for name, pix := range testimg.All() {
		a := Analyze(testimg.Size, testimg.Size, pix)
		sw := a.Sweep
		// 模型总失真：S(k) = (1/N)Σ min(σ_k²+1/12, 24)。差分 Δk = (σ_k²+1/12 − 24)/N。
		// 由于 σ 升序，差分单调不减，故 S 只有一个极小点。
		// ε 取 1e-9 以吸收双精度累加误差。
		for i := 1; i < len(sw)-1; i++ {
			d1 := sw[i].TotalModel - sw[i-1].TotalModel
			d2 := sw[i+1].TotalModel - sw[i].TotalModel
			if d2 < d1-1e-9 {
				t.Fatalf("%s: 模型差分在 k=%d 不单调 (%.3e → %.3e)", name, i, d1, d2)
			}
		}
		// 模型极小点必须唯一。
		local := 0
		for i := 1; i < len(sw)-1; i++ {
			if sw[i].TotalModel < sw[i-1].TotalModel && sw[i].TotalModel <= sw[i+1].TotalModel {
				local++
			}
		}
		if local > 1 {
			t.Fatalf("%s: 模型极小点 %d 个", name, local)
		}
		// 模型阈值 τ* 落点与扫描最优点一致。
		if a.ModelK != a.MinK {
			t.Fatalf("%s: 模型阈值落点 k=%d，真实最优 k=%d", name, a.ModelK, a.MinK)
		}
		if math.Abs(sw[a.MinK].Total-a.Sweep[a.MinK].TotalModel) > 1.0 {
			t.Fatalf("%s: 真实与模型总失真相差过大", name)
		}
		if !(a.Lo < Tau && Tau <= a.Hi) {
			t.Fatalf("%s: τ* 未落在最优平台 [%.4f, %.4f] 内", name, a.Lo, a.Hi)
		}
	}
}

// TestThresholdIsModelCrossing 核验阈值就是两条路径单位失真相等的点，(τ²)+1/12 = 24。
func TestThresholdIsModelCrossing(t *testing.T) {
	if math.Abs(TauSq+FlatOff-TexMSE) > 1e-12 {
		t.Fatalf("τ²+1/12=%.15f ≠ 24", TauSq+FlatOff)
	}
	if math.Abs(Tau*Tau-TauSq) > 1e-12 {
		t.Fatalf("τ² 不一致")
	}
	// 以 τ 为界时，模型总失真与扫描最优值相等。
	for _, pix := range testimg.All() {
		a := Analyze(testimg.Size, testimg.Size, pix)
		best := a.Sweep[a.MinK].TotalModel
		atTau := a.TotalModel()
		if math.Abs(best-atTau) > 1e-9 {
			t.Fatalf("τ 落点模型总失真 %.9f ≠ 扫描最优 %.9f", atTau, best)
		}
	}
}
