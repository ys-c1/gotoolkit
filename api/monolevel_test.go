package api

import (
	"math"
	"testing"
)

// TestAnalyzeQuantTableThreeTiers 经唯一对外入口 api 复核三档：
// 修复后电平与整条映射逐对比较零逆序；误差不高于中点；预测与实测一致。
func TestAnalyzeQuantTableThreeTiers(t *testing.T) {
	tiers := DemoQuantTiers()
	if len(tiers) != 3 {
		t.Fatalf("档位数量 %d != 3", len(tiers))
	}
	for i, tier := range tiers {
		hist, err := DemoTierHistogram(i)
		if err != nil {
			t.Fatalf("%s 数据集: %v", tier.Name, err)
		}
		a, err := AnalyzeQuantTable(tier.Steps, hist, 0.42)
		if err != nil {
			t.Fatalf("%s 分析失败: %v", tier.Name, err)
		}
		if a.LevelInversions != 0 || a.RampInversions != 0 {
			t.Fatalf("%s 修复后仍有逆序: 电平 %d 映射 %d",
				tier.Name, a.LevelInversions, a.RampInversions)
		}
		// 逐对比较必须是全量规模，不是抽样。
		wantPairs := len(tier.Steps) * (len(tier.Steps) - 1) / 2
		if a.LevelPairs != wantPairs {
			t.Fatalf("%s 电平比较对数 %d != %d", tier.Name, a.LevelPairs, wantPairs)
		}
		if a.RampPairs != 256*255/2 {
			t.Fatalf("%s 映射比较对数 %d != 32640", tier.Name, a.RampPairs)
		}
		if a.JOptimal > a.JMidpoint+1e-9 {
			t.Fatalf("%s J 修复 %.4f > J 中点 %.4f", tier.Name, a.JOptimal, a.JMidpoint)
		}
		if a.MSEOptimal > a.MSEMidpoint+1e-9 {
			t.Fatalf("%s MSE 修复 %.4f > 中点 %.4f", tier.Name, a.MSEOptimal, a.MSEMidpoint)
		}
		if a.MSEOptimal >= a.MSENominal {
			t.Fatalf("%s 修复 %.4f 未优于名义表 %.4f", tier.Name, a.MSEOptimal, a.MSENominal)
		}
		// 同一份直方图上全方差恒等式必须与像素 MSE 一致。
		if math.Abs(a.PredictedMSEOptimal-a.MSEOptimal) > 1e-8 {
			t.Fatalf("%s 预测 %.6f != 实测 %.6f", tier.Name, a.PredictedMSEOptimal, a.MSEOptimal)
		}
		if a.Stationarity > 1e-8 || a.Complementarity > 1e-8 || a.PrimalViolation > 1e-12 {
			t.Fatalf("%s KKT 残差过大: primal %g station %g comp %g",
				tier.Name, a.PrimalViolation, a.Stationarity, a.Complementarity)
		}
		if a.MinLambda < -1e-9 {
			t.Fatalf("%s λmin %g 为负", tier.Name, a.MinLambda)
		}
		if a.Active+a.Inactive != len(tier.Steps)-1 {
			t.Fatalf("%s 约束计数 %d+%d != %d", tier.Name, a.Active, a.Inactive, len(tier.Steps)-1)
		}
	}
}

// TestMonotoneReconstructArbitrary 对外的 PAVA 门面在任意输入上都给非降解，
// 且中点表对任意正步长序列可行、单调。
func TestMonotoneReconstructArbitrary(t *testing.T) {
	steps := []int{1, 3, 250, 1, 1} // 非排序、任意形状，总和 256
	if _, err := QuantBoundaries(steps); err != nil {
		t.Fatal(err)
	}
	a := []float64{9, 1, 8, 0, 7}
	w := []float64{1, 1, 1, 1, 1}
	r, err := MonotoneReconstruct(a, w)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(r); i++ {
		if r[i] > r[i+1] {
			t.Fatalf("PAVA 输出非单调: %v", r)
		}
	}
	levels := RoundQuantLevels(r)
	if n, bad := CountLevelInversions(levels); bad != 0 {
		t.Fatalf("取整后 %d 对中有 %d 逆序", n, bad)
	}
	mid, err := MidpointQuantLevels(steps)
	if err != nil {
		t.Fatal(err)
	}
	if _, bad := CountLevelInversions(mid); bad != 0 {
		t.Fatalf("任意步长下中点表应单调: %v", mid)
	}
}
