package subband

import (
	"math"
	"testing"
)

func TestDWTInvertible(t *testing.T) {
	w, h, pix := SyntheticImage(64)
	for _, levels := range []int{1, 2, 3} {
		bands, err := ForwardDWT(pix, w, h, levels)
		if err != nil {
			t.Fatalf("levels=%d 正变换失败: %v", levels, err)
		}
		if len(bands) != 1+3*levels {
			t.Fatalf("levels=%d 子带数错误: %d", levels, len(bands))
		}
		got, err := InverseDWT(bands, w, h, levels)
		if err != nil {
			t.Fatalf("levels=%d 逆变换失败: %v", levels, err)
		}
		// 正交 Haar 能量守恒，重构误差只来自最后取整，应逐像素一致。
		for i := range pix {
			if d := int(got[i]) - int(pix[i]); d > 1 || d < -1 {
				t.Fatalf("levels=%d 像素 %d 重构偏差 %d", levels, i, d)
			}
		}
	}
}

func TestBudgetAndWaterfillWins(t *testing.T) {
	w, h, pix := SyntheticImage(64)
	levels := 2
	budgets := []int{w * h * 1, w * h * 2, w * h * 3} // 1/2/3 bpp
	rep, err := Analyze(pix, w, h, levels, budgets)
	if err != nil {
		t.Fatal(err)
	}
	for _, br := range rep.Budgets {
		// 系数比特不得超预算。
		if br.Water.Used > br.Budget {
			t.Fatalf("预算 %d 被超出: used=%d", br.Budget, br.Water.Used)
		}
		if br.Equal.Used > br.Budget {
			t.Fatalf("平分预算 %d 被超出: used=%d", br.Budget, br.Equal.Used)
		}
		// 连续解预算残差应在数值精度内为 0。
		if br.Water.BudgetResidual > 1e-9 {
			t.Fatalf("预算 %d 连续解残差过大: %g", br.Budget, br.Water.BudgetResidual)
		}
		// KKT 残差应很小（活动带共水位）。
		if br.Water.KKTResidual > 1e-9 {
			t.Fatalf("预算 %d KKT 残差过大: %g", br.Budget, br.Water.KKTResidual)
		}
		// 同体积下注水加权误差必须不高于平分。
		if br.WaterMSE > br.EqualMSE+1e-12 {
			t.Fatalf("预算 %d 注水 MSE %g 高于平分 %g", br.Budget, br.WaterMSE, br.EqualMSE)
		}
		// 反比关系检查：指数律高 R²、负秩相关。
		if br.Inverse.LogR2 < 0.9 {
			t.Fatalf("预算 %d 指数律 R² 过低: %g", br.Budget, br.Inverse.LogR2)
		}
		if br.Inverse.Spearman >= 0 {
			t.Fatalf("预算 %d 比特与失真应负相关, Spearman=%g", br.Budget, br.Inverse.Spearman)
		}
	}
}

func TestTruncationPath(t *testing.T) {
	// 构造 3 个等方差单系数带 + 1 个零方差带，预算 2：
	// 连续解各活动带 2/3≈0.667，就近取整后 1+1+1=3 > 2，必须截断 1 比特。
	stats := []Stats{
		{Name: "LL", Level: 2, N: 1, Weight: 0.25, Var: 100},
		{Name: "LH", Level: 2, N: 1, Weight: 0.25, Var: 100},
		{Name: "HL", Level: 2, N: 1, Weight: 0.25, Var: 100},
		{Name: "HH", Level: 2, N: 1, Weight: 0.25, Var: 0},
	}
	res := Waterfill(stats, 2)
	if res.TruncatedBits != 1 {
		t.Fatalf("应截断 1 比特, 实际 %d", res.TruncatedBits)
	}
	if res.Used != 2 {
		t.Fatalf("截断后用量应为 2, 实际 %d", res.Used)
	}
	if len(res.Active) != 3 {
		t.Fatalf("活动带应为 3, 实际 %d", len(res.Active))
	}
	// 零方差带必须不活动且零比特。
	if res.Details[3].Bits != 0 {
		t.Fatalf("零方差带不应分到比特")
	}
}

func TestZeroBudget(t *testing.T) {
	w, h, pix := SyntheticImage(64)
	rep, err := Analyze(pix, w, h, 2, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	br := rep.Budgets[0]
	if br.Water.Used != 0 {
		t.Fatalf("零预算用量应为 0, 实际 %d", br.Water.Used)
	}
	// 零比特时每带用均值重构，整图 MSE 应等于各带方差加权和。
	var want float64
	for _, st := range rep.Stats {
		want += st.Weight * st.Var
	}
	if math.Abs(br.WaterMSE-want) > 1e-9 {
		t.Fatalf("零预算 MSE %g 不等于方差加权和 %g", br.WaterMSE, want)
	}
}

func TestStreamRoundTrip(t *testing.T) {
	w, h, pix := SyntheticImage(64)
	levels, budget := 2, w*h*2
	bands, _ := ForwardDWT(pix, w, h, levels)
	stats := make([]Stats, len(bands))
	for i, b := range bands {
		stats[i] = ComputeStats(b)
	}
	res := Waterfill(stats, budget)
	bits := make([]int, len(bands))
	for i, d := range res.Details {
		bits[i] = d.Bits
	}
	stream, err := EncodeStream(pix, w, h, levels, budget, ModeWaterfill, bits)
	if err != nil {
		t.Fatal(err)
	}
	rw, rh, rl, rb, mode, got, err := DecodeStream(stream)
	if err != nil {
		t.Fatal(err)
	}
	if rw != w || rh != h || rl != levels || rb != budget || mode != ModeWaterfill {
		t.Fatal("码头信息不一致")
	}
	// 2bpp 预算紧张，高方差带每系数仅 2~6 级量化，逆变换后单点最大偏差
	// 可达约 10 个灰级；这里只验证码流往返结构正确，失真大小由 Analyze 的 MSE 考核。
	maxDev := 0
	for i := range pix {
		d := int(got[i]) - int(pix[i])
		if d < 0 {
			d = -d
		}
		if d > maxDev {
			maxDev = d
		}
	}
	if maxDev > 10 {
		t.Fatalf("像素往返最大偏差 %d 过大", maxDev)
	}

	// 平分路径必须产出与注水完全相同的文件体积（同体积对比前提）。
	eq := EqualAllocation(stats, budget)
	eqBits := make([]int, len(bands))
	for i, d := range eq.Details {
		eqBits[i] = d.Bits
	}
	eqStream, err := EncodeStream(pix, w, h, levels, budget, ModeEqual, eqBits)
	if err != nil {
		t.Fatal(err)
	}
	if len(eqStream) != len(stream) {
		t.Fatalf("两路径体积不一致: 注水 %d vs 平分 %d", len(stream), len(eqStream))
	}
}
