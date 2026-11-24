package quantbias

import (
	"math"
	"testing"
)

// TestW2OffsetMatchesBiasTerm 验证闭式偏置解与 W₂² 散度数值最小点一致。
func TestW2OffsetMatchesBiasTerm(t *testing.T) {
	imgs := SampleImages(DefaultSize, DefaultSeed)
	for _, class := range ClassNames {
		for _, bits := range DefaultBits {
			c := Analyze(imgs[class], DefaultSize, DefaultSize, bits, class)
			if c.SolverAgreement > 1e-6 {
				t.Fatalf("%s b=%d: W2 最小点 %.6f 与偏置项 β=%.6f 偏离 %.2e",
					class, bits, c.OffsetW2/c.Step, c.PhaseMean, c.SolverAgreement)
			}
			if math.Abs(c.ResidualBiasContinuous) > 1e-6 {
				t.Fatalf("%s b=%d: 连续偏移残余偏置 %.2e 不为零",
					class, bits, c.ResidualBiasContinuous)
			}
		}
	}
}

// TestOriginalQuantizerIsOneSided 坐实“误差都挤在区间一侧”：
// 原始零偏移量化器的误差 100% 非负，且平均偏置约为半步长。
func TestOriginalQuantizerIsOneSided(t *testing.T) {
	imgs := SampleImages(DefaultSize, DefaultSeed)
	for _, class := range ClassNames {
		c := Analyze(imgs[class], DefaultSize, DefaultSize, 3, class)
		if c.PositiveFraction != 1.0 {
			t.Fatalf("%s: 原始量化器正误差比例应为 1，实得 %.3f", class, c.PositiveFraction)
		}
		if c.PhaseMean < 0.35 || c.PhaseMean > 0.65 {
			t.Fatalf("%s: 相位均值 β=%.3f 不在半步长附近，偏置前提异常", class, c.PhaseMean)
		}
		if c.OutsideHalfFrac < 0.2 {
			t.Fatalf("%s: 应有大量误差越出标准对称半格，实得 %.3f", class, c.OutsideHalfFrac)
		}
	}
}

// TestFourClassesThreshold005 是题目要求的总验收：四类图各测一次（并加严到
// 三档位深逐档），整数可落地偏移后的归一化残余偏置不得超过 0.05。
func TestFourClassesThreshold005(t *testing.T) {
	imgs := SampleImages(DefaultSize, DefaultSeed)
	rep := BuildReport(imgs, DefaultSize, DefaultSize, DefaultBits, DefaultSeed)
	if math.Abs(rep.Threshold-0.05) > 1e-12 {
		t.Fatalf("阈值必须是 0.05，实得 %v", rep.Threshold)
	}
	for _, v := range rep.ClassVerdicts {
		t.Logf("四类图判定 %-9s: 最差残余偏置=%.4f (b=%d) 阈值=%.2f pass=%v 散度全降=%v",
			v.Class, v.MaxResidualBias, v.WorstDepthBits, BiasTolerance,
			v.BiasPass, v.AllDivergenceDown)
		if !v.BiasPass {
			t.Errorf("%s: 偏差 %.4f 超过阈值 %.2f —— 偏置没有解对",
				v.Class, v.MaxResidualBias, BiasTolerance)
		}
	}
}

// TestDivergenceDecreasesEveryTier 验证每一类、每一档位深，校正后的
// JS/KL/χ² 三种散度相对原始参照量化器都严格下降。
func TestDivergenceDecreasesEveryTier(t *testing.T) {
	imgs := SampleImages(DefaultSize, DefaultSeed)
	for _, class := range ClassNames {
		for _, bits := range DefaultBits {
			c := Analyze(imgs[class], DefaultSize, DefaultSize, bits, class)
			if !(c.JSInteger < c.JSOriginal) {
				t.Errorf("%s b=%d: JS 未下降 %.5f -> %.5f", class, bits, c.JSOriginal, c.JSInteger)
			}
			if !(c.KLInteger < c.KLOriginal) {
				t.Errorf("%s b=%d: KL 未下降 %.5f -> %.5f", class, bits, c.KLOriginal, c.KLInteger)
			}
			if !(c.Chi2Integer < c.Chi2Original) {
				t.Errorf("%s b=%d: chi2 未下降 %.5f -> %.5f", class, bits, c.Chi2Original, c.Chi2Integer)
			}
			if !c.DivergenceDown {
				t.Errorf("%s b=%d: DivergenceDown 标志未置位", class, bits)
			}
		}
	}
}

// TestSyntheticUniformPhases 用 0..255 全灰阶循环构造相位严格均匀的输入，
// 检查 β=(Δ-1)/(2Δ) 的离散结论与取整残余上界 ≤1/(2Δ)。
func TestSyntheticUniformPhases(t *testing.T) {
	for _, bits := range DefaultBits {
		levels := 1 << bits
		step := 256 / levels
		pix := make([]uint8, 0, 256*16)
		for rep := 0; rep < 16; rep++ {
			for v := 0; v < 256; v++ {
				pix = append(pix, uint8(v))
			}
		}
		c := Analyze(pix, len(pix), 1, bits, "uniform-phases")
		wantBeta := float64(step-1) / (2 * float64(step))
		if math.Abs(c.PhaseMean-wantBeta) > 1e-9 {
			t.Fatalf("b=%d: 均匀相位 β=%.6f，期望 %.6f", bits, c.PhaseMean, wantBeta)
		}
		if math.Abs(c.ResidualBiasInteger) > 0.5/float64(step)+1e-9 {
			t.Fatalf("b=%d: 取整残余 %.4f 超过 1/(2Δ)=%.4f",
				bits, math.Abs(c.ResidualBiasInteger), 0.5/float64(step))
		}
		if math.Abs(c.ResidualBiasInteger) > BiasTolerance {
			t.Fatalf("b=%d: 均匀相位残余 %.4f 超过 0.05", bits, c.ResidualBiasInteger)
		}
	}
}

func TestReportAllPass(t *testing.T) {
	imgs := SampleImages(DefaultSize, DefaultSeed)
	rep := BuildReport(imgs, DefaultSize, DefaultSize, DefaultBits, DefaultSeed)
	if !rep.AllPass {
		t.Fatalf("存在未通过单元: %+v", rep.ClassVerdicts)
	}
	if len(rep.Cells) != len(ClassNames)*len(DefaultBits) {
		t.Fatalf("应有 %d 个测量单元，实得 %d", len(ClassNames)*len(DefaultBits), len(rep.Cells))
	}
}

// TestCorrectedJSUnderFiveHundredths 是加严的“均匀度”质量线：四类图 × 三档，
// 整数偏移后的 JS 散度本身也都低于 0.05。注意题目规定的 0.05 阈值作用在
// “残余偏置”上（见 TestFourClassesThreshold005），JS<0.05 只是本数据集
// 额外达到的效果，并非同一判据。
func TestCorrectedJSUnderFiveHundredths(t *testing.T) {
	imgs := SampleImages(DefaultSize, DefaultSeed)
	for _, class := range ClassNames {
		for _, bits := range DefaultBits {
			c := Analyze(imgs[class], DefaultSize, DefaultSize, bits, class)
			if c.JSInteger >= 0.05 {
				t.Errorf("%s b=%d: 校正后 JS=%.4f 未低于 0.05", class, bits, c.JSInteger)
			}
		}
	}
}

// TestBimodalStressBiasStillSolved 用极端双峰图说明 0.05 判据针对的是“偏置”
// 而非“形状均匀”：全局偏移可以把均值偏置清零，却不能把两个相位峰变成均匀分布，
// 因此该例残余偏置仍过阈值、JS 仍然高——这是方法的诚实边界。
func TestBimodalStressBiasStillSolved(t *testing.T) {
	const n = 256
	pix := make([]uint8, n*n)
	for i := range pix {
		if i%2 == 0 {
			pix[i] = 40
		} else {
			pix[i] = 200
		}
	}
	c := Analyze(pix, n, n, 2, "bimodal")
	if !c.BiasPass {
		t.Fatalf("双峰图的偏置仍应被偏移解对，残余 %.4f", c.ResidualBiasInteger)
	}
	if c.JSInteger < 0.05 {
		t.Fatalf("双峰形状不可能靠平移变均匀，JS=%.4f 异常偏低", c.JSInteger)
	}
}
