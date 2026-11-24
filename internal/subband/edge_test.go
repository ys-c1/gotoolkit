package subband

import "testing"

func TestWaterfillMonotonicAndRealloc(t *testing.T) {
	w, h, pix := SyntheticImage(64)
	rep, err := Analyze(pix, w, h, 2, []int{2048, 4096, 8192, 12288, 20000})
	if err != nil {
		t.Fatal(err)
	}
	prev := mathInf()
	for _, br := range rep.Budgets {
		// 预算越大，整图注水 MSE 应单调不增，且始终不劣于平分。
		if br.WaterMSE > prev+1e-9 {
			t.Fatalf("预算 %d 注水 MSE 未单调下降", br.Budget)
		}
		if br.WaterMSE > br.EqualMSE+1e-9 {
			t.Fatalf("预算 %d 注水劣于平分", br.Budget)
		}
		if br.Water.Used > br.Budget {
			t.Fatalf("预算 %d 超支", br.Budget)
		}
		prev = br.WaterMSE
	}
	// 高预算触顶：每系数不超过 MaxBitsPerCoef，且仍不超预算。
	last := rep.Budgets[len(rep.Budgets)-1]
	for _, d := range last.Water.Details {
		if d.Bits > MaxBitsPerCoef {
			t.Fatalf("带 %s 比特触顶失效: %d", d.Name, d.Bits)
		}
	}
}

func mathInf() float64 { return 1e300 }

func TestForcedTruncationLarge(t *testing.T) {
	// 7 个等方差带、每带 1 系数、预算 3：连续解各 3/7≈0.43，
	// 就近取整全为 0，不截断；改用预算 5（连续 0.71 全入为 1 → 7>5）强制截断 2。
	stats := make([]Stats, 7)
	for i := range stats {
		stats[i] = Stats{Name: "B", Level: 1, N: 1, Weight: 1.0 / 7, Var: 50}
	}
	res := Waterfill(stats, 5)
	if res.TruncatedBits != 2 {
		t.Fatalf("应截断 2 比特, 实际 %d", res.TruncatedBits)
	}
	if res.Used != 5 {
		t.Fatalf("截断后应用满 5, 实际 %d", res.Used)
	}
}
