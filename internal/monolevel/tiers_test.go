package monolevel

import (
	"math"
	"testing"
)

// 三档的实测数字由 cmd/levelreport 产出，这里逐项锁定，防止报告与实现漂移。
var tierWant = []struct {
	name           string
	nomLevelBad    int
	nomRampBad     int
	lsRep, lsMid   float64
	mseRep, mseMid float64
	predRep        float64
	active         int
}{
	{"uniform-8", 4, 4096, 41.9967, 506.5005, 97.6857, 120.2819, 97.4465, 4},
	{"geometric-16", 8, 2541, 11.9341, 146.3969, 27.5255, 36.4885, 27.2917, 8},
	{"perceptual-32", 16, 1104, 3.2870, 32.9896, 5.8954, 8.3114, 5.8719, 16},
}

type tierRun struct {
	steps                  []int
	bounds                 []int
	nominal, repaired, mid []int
	rFloat                 []float64
	cells                  Cells
	train, test            []uint64
	lsRep, lsMid           float64
	mseNom, mseRep, mseMid float64
	predRep, predMid       float64
	kkt                    KKTReport
	nomLevel, nomRamp      int
	repLevel, repRamp      int
	midLevel, midRamp      int
	levelPairs, rampPairs  int
	blocks                 []Block
}

func runTier(t *testing.T, tierIdx int) tierRun {
	t.Helper()
	tier := Tiers[tierIdx]
	var out tierRun
	out.steps = tier.Steps

	hist, err := TierHistogram(tierIdx)
	if err != nil {
		t.Fatalf("%s 直方图: %v", tier.Name, err)
	}
	out.train, out.test = SplitCounts(hist)
	cells, err := Stats(tier.Steps, out.train)
	if err != nil {
		t.Fatalf("%s 统计: %v", tier.Name, err)
	}
	out.cells = cells
	out.bounds = cells.Bounds
	out.nominal = NominalLevels(cells, 0.42)
	out.rFloat, out.blocks, err = PAVA(f64(out.nominal), cells.Weight)
	if err != nil {
		t.Fatalf("%s PAVA: %v", tier.Name, err)
	}
	out.repaired = RoundLevels(out.rFloat)
	out.mid, err = MidpointLevels(tier.Steps)
	if err != nil {
		t.Fatalf("%s 中点: %v", tier.Name, err)
	}
	out.kkt = KKT(f64(out.nominal), cells.Weight, out.rFloat)

	out.levelPairs, _ = AllPairs(out.nominal)
	_, nomBad := AllPairs(out.nominal)
	out.nomLevel = len(nomBad)
	_, repBad := AllPairs(out.repaired)
	out.repLevel = len(repBad)
	_, midBad := AllPairs(out.mid)
	out.midLevel = len(midBad)

	var nomRamp []Pair
	out.rampPairs, nomRamp, err = RampAllPairs(tier.Steps, out.nominal)
	if err != nil {
		t.Fatalf("%s 展开: %v", tier.Name, err)
	}
	_, repRamp, err := RampAllPairs(tier.Steps, out.repaired)
	if err != nil {
		t.Fatal(err)
	}
	_, midRamp, err := RampAllPairs(tier.Steps, out.mid)
	if err != nil {
		t.Fatal(err)
	}
	out.nomRamp, out.repRamp, out.midRamp = len(nomRamp), len(repRamp), len(midRamp)

	out.lsRep, _ = LSError(f64(out.nominal), cells.Weight, f64(out.repaired))
	out.lsMid, _ = LSError(f64(out.nominal), cells.Weight, f64(out.mid))
	out.mseNom, _ = PixelMSE(out.test, cells.Bounds, out.nominal)
	out.mseRep, _ = PixelMSE(out.test, cells.Bounds, out.repaired)
	out.mseMid, _ = PixelMSE(out.test, cells.Bounds, out.mid)
	out.predRep, _ = PredictMSE(cells, out.repaired)
	out.predMid, _ = PredictMSE(cells, out.mid)
	return out
}

func f64(v []int) []float64 {
	out := make([]float64, len(v))
	for i := range v {
		out[i] = float64(v[i])
	}
	return out
}

// TestTierDatasets 数据集必须是长度 256、总和 DatasetN、每格非空的整数直方图。
func TestTierDatasets(t *testing.T) {
	for i, tier := range Tiers {
		hist, err := TierHistogram(i)
		if err != nil {
			t.Fatalf("%s: %v", tier.Name, err)
		}
		if len(hist) != DomainN {
			t.Fatalf("%s 长度 %d", tier.Name, len(hist))
		}
		var sum uint64
		for _, c := range hist {
			if c == 0 {
				t.Fatalf("%s 存在空灰度", tier.Name)
			}
			sum += c
		}
		if sum != DatasetN {
			t.Fatalf("%s 总和 %d != %d", tier.Name, sum, DatasetN)
		}
		bounds, _ := Partition(tier.Steps)
		for k := 0; k+1 < len(bounds); k++ {
			var cell uint64
			for x := bounds[k]; x < bounds[k+1]; x++ {
				cell += hist[x]
			}
			if cell == 0 {
				t.Fatalf("%s 区间 [%d,%d) 为空", tier.Name, bounds[k], bounds[k+1])
			}
		}
	}
}

// TestNominalTableIsNonMonotone 凑出来的名义表确实非单调：三档都有逆序对，
// 且整条映射上也有逆序对。这是“需要修复”的前提。
func TestNominalTableIsNonMonotone(t *testing.T) {
	for i := range Tiers {
		r := runTier(t, i)
		if r.nomLevel == 0 {
			t.Fatalf("%s 名义电平表不该单调", Tiers[i].Name)
		}
		if r.nomRamp == 0 {
			t.Fatalf("%s 名义整条映射不该单调", Tiers[i].Name)
		}
	}
}

// TestRepairedPointwiseMonotone 改完三档重建电平单调：电平表与整条 256 点映射
// 都做全量逐对比较（不抽样），逆序对数必须为零。
func TestRepairedPointwiseMonotone(t *testing.T) {
	for i := range Tiers {
		r := runTier(t, i)
		if r.levelPairs != len(r.steps)*(len(r.steps)-1)/2 {
			t.Fatalf("%s 电平逐对比较规模 %d", Tiers[i].Name, r.levelPairs)
		}
		if r.rampPairs != DomainN*(DomainN-1)/2 {
			t.Fatalf("%s 映射逐对比较规模 %d，应为 %d", Tiers[i].Name, r.rampPairs, DomainN*(DomainN-1)/2)
		}
		if r.repLevel != 0 {
			t.Fatalf("%s 修复后电平仍有 %d 对逆序", Tiers[i].Name, r.repLevel)
		}
		if r.repRamp != 0 {
			t.Fatalf("%s 修复后映射仍有 %d 对逆序", Tiers[i].Name, r.repRamp)
		}
		// 中点表本身单调，作为参照。
		if r.midLevel != 0 || r.midRamp != 0 {
			t.Fatalf("%s 中点表不单调（对照路径异常）", Tiers[i].Name)
		}
	}
}

// TestRepairedBeatsMidpointLS 最小二乘意义下修复不劣于中点：中点表是单调锥的
// 可行点，PAVA 在锥上最小化同一目标，故 J(rep) ≤ J(mid)。三档逐档验。
func TestRepairedBeatsMidpointLS(t *testing.T) {
	for i := range Tiers {
		r := runTier(t, i)
		if r.lsRep > r.lsMid+1e-9 {
			t.Fatalf("%s J(rep) %.6f > J(mid) %.6f", Tiers[i].Name, r.lsRep, r.lsMid)
		}
		if r.lsMid <= 0 {
			t.Fatalf("%s 对照中点表 J 应为正", Tiers[i].Name)
		}
	}
}

// TestRepairedPixelErrorNotAboveMidpoint “误差不高于中点重建”按测试集像素 MSE 验。
// 这是与 J 不同的量（J 对名义表，MSE 对原始灰度），故单列一条。
func TestRepairedPixelErrorNotAboveMidpoint(t *testing.T) {
	for i := range Tiers {
		r := runTier(t, i)
		if r.mseRep > r.mseMid+1e-9 {
			t.Fatalf("%s 像素 MSE 修复 %.6f > 中点 %.6f", Tiers[i].Name, r.mseRep, r.mseMid)
		}
		if r.mseRep >= r.mseNom {
			t.Fatalf("%s 修复应优于名义表: 修复 %.6f 名义 %.6f", Tiers[i].Name, r.mseRep, r.mseNom)
		}
	}
}

// TestPredictionMatchesTestError 预测式 Σw[σ²+(r-μ)²] 在训练集统计下应贴近测试集实测，
// 这是全方差公式给出的无偏预测。训练/测试是同一直方图的确定性对半切分（每灰度
// 至多差一个样本），二者分布只是近似相同，故容差取实测的 10%。
func TestPredictionMatchesTestError(t *testing.T) {
	for i := range Tiers {
		r := runTier(t, i)
		for _, p := range []struct {
			label string
			pred  float64
			meas  float64
		}{
			{"修复", r.predRep, r.mseRep},
			{"中点", r.predMid, r.mseMid},
		} {
			if math.Abs(p.pred-p.meas) > 0.10*p.meas {
				t.Fatalf("%s %s 预测 %.4f 与实测 %.4f 相差超过 10%%", Tiers[i].Name, p.label, p.pred, p.meas)
			}
		}
		// 预测也应保持与实测相同的排序：修复优于中点。
		if r.predRep >= r.predMid {
			t.Fatalf("%s 预测排序错误: 修复 %.4f 未优于中点 %.4f", Tiers[i].Name, r.predRep, r.predMid)
		}
	}
}

// TestKKTOnTiers 三档 PAVA 解都满足 KKT：稳定性与互补松弛残差在浮点噪声内，
// 对偶乘子非负，且约束起作用/不起作用的计数与合并块一致。
func TestKKTOnTiers(t *testing.T) {
	for i := range Tiers {
		r := runTier(t, i)
		if r.kkt.Stationarity > 1e-8 {
			t.Fatalf("%s 稳定性残差 %g", Tiers[i].Name, r.kkt.Stationarity)
		}
		if r.kkt.Complementarity > 1e-8 {
			t.Fatalf("%s 互补松弛残差 %g", Tiers[i].Name, r.kkt.Complementarity)
		}
		if r.kkt.MinLambda < -1e-9 {
			t.Fatalf("%s λmin %g 为负", Tiers[i].Name, r.kkt.MinLambda)
		}
		// 每个合并块内部有 Hi-Lo-1 条起作用约束。
		wantActive := 0
		for _, b := range r.blocks {
			wantActive += b.Hi - b.Lo - 1
		}
		if r.kkt.Active != wantActive {
			t.Fatalf("%s 起作用约束 %d != 合并块内边界数 %d", Tiers[i].Name, r.kkt.Active, wantActive)
		}
	}
}

// TestTwoStepConstructionEndToEnd 两步串联：任意步长 → 边界 → 名义表 → PAVA →
// 取整，端点落在合法范围内，块内值等于加权均值。
func TestTwoStepConstructionEndToEnd(t *testing.T) {
	for i := range Tiers {
		r := runTier(t, i)
		for k, v := range r.repaired {
			if v < 0 || v > 255 {
				t.Fatalf("%s 电平越界 at %d: %d", Tiers[i].Name, k, v)
			}
		}
		if r.blocks[0].Lo != 0 || r.blocks[len(r.blocks)-1].Hi != len(r.steps) {
			t.Fatalf("%s 块未覆盖全部下标", Tiers[i].Name)
		}
		for _, b := range r.blocks {
			var W, S float64
			for k := b.Lo; k < b.Hi; k++ {
				W += r.cells.Weight[k]
				S += r.cells.Weight[k] * float64(r.nominal[k])
			}
			if math.Abs(S/W-b.Value) > 1e-9 {
				t.Fatalf("%s 块 [%d,%d) 值不符", Tiers[i].Name, b.Lo, b.Hi)
			}
		}
	}
}

// TestLockedTierNumbers 锁定报告里的数字（四位小数），防止实现漂移。
func TestLockedTierNumbers(t *testing.T) {
	for i := range Tiers {
		r := runTier(t, i)
		w := tierWant[i]
		if r.nomLevel != w.nomLevelBad || r.nomRamp != w.nomRampBad {
			t.Fatalf("%s 名义逆序 %d/%d，期望 %d/%d", w.name, r.nomLevel, r.nomRamp, w.nomLevelBad, w.nomRampBad)
		}
		if math.Abs(r.lsRep-w.lsRep) > 5e-4 || math.Abs(r.lsMid-w.lsMid) > 5e-4 {
			t.Fatalf("%s J 修复 %.4f 中点 %.4f，期望 %.4f/%.4f", w.name, r.lsRep, r.lsMid, w.lsRep, w.lsMid)
		}
		if math.Abs(r.mseRep-w.mseRep) > 5e-4 || math.Abs(r.mseMid-w.mseMid) > 5e-4 {
			t.Fatalf("%s MSE 修复 %.4f 中点 %.4f，期望 %.4f/%.4f", w.name, r.mseRep, r.mseMid, w.mseRep, w.mseMid)
		}
		if math.Abs(r.predRep-w.predRep) > 5e-4 {
			t.Fatalf("%s 预测 %.4f，期望 %.4f", w.name, r.predRep, w.predRep)
		}
		if r.kkt.Active != w.active {
			t.Fatalf("%s 起作用约束 %d，期望 %d", w.name, r.kkt.Active, w.active)
		}
	}
}
