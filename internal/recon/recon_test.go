package recon

import (
	"math"
	"testing"
)

// 容差：正规方程在 ~1e5 量级，高斯消元后约束应到 1e-8 以内。
const (
	tolConstraint = 1e-8
	tolKKT        = 1e-6
)

func solvedReport(t *testing.T) *Report {
	t.Helper()
	rep, err := Run()
	if err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	return rep
}

// 约束①：系数和必须为 1（单独一条，不与对称性合并）。
func TestConstraintSumToOne(t *testing.T) {
	s := solvedReport(t).Solved
	got := s.H[0] + s.H[1] + s.H[2] + s.H[3] + s.H[4]
	if math.Abs(got-1) > tolConstraint {
		t.Fatalf("Σh = %.12f，偏离 1 超过 %.0e", got, tolConstraint)
	}
}

// 约束②-a：中心对称，上 = 下（h0 = h4）。
func TestConstraintSymmetryUpDown(t *testing.T) {
	s := solvedReport(t).Solved
	if math.Abs(s.H[0]-s.H[4]) > tolConstraint {
		t.Fatalf("h0=%.10f != h4=%.10f", s.H[0], s.H[4])
	}
}

// 约束②-b：中心对称，左 = 右（h1 = h3）。
func TestConstraintSymmetryLeftRight(t *testing.T) {
	s := solvedReport(t).Solved
	if math.Abs(s.H[1]-s.H[3]) > tolConstraint {
		t.Fatalf("h1=%.10f != h3=%.10f", s.H[1], s.H[3])
	}
}

// KKT 增广正规方程必须真正被解出（左右两端相符）。
func TestKKTNormalEquationsSolved(t *testing.T) {
	s := solvedReport(t).Solved
	if r := s.KKTResidual(); r > tolKKT {
		t.Fatalf("KKT 最大残差 %.3e > %.0e", r, tolKKT)
	}
}

// 约束 LS 的解在训练平方误差上必须不劣于任一可行的固定双线性核（δ 与四邻域）。
func TestConstrainedLSIsOptimalOverFeasibleBilinear(t *testing.T) {
	imgs := Suite()
	A, y := BuildRegression(imgs)
	s, err := SolveConstrainedLS(A, y)
	if err != nil {
		t.Fatal(err)
	}
	sse := func(h [TapCount]float64) float64 {
		var e float64
		for i := range A {
			var p float64
			for k := 0; k < TapCount; k++ {
				p += A[i][k] * h[k]
			}
			d := p - y[i]
			e += d * d
		}
		return e
	}
	got := sse(s.H)
	if base := sse(BilinearCoSitedTaps()); got > base*(1+1e-9) {
		t.Fatalf("CLS SSE %.3f 意外劣于共址双线性 %.3f", got, base)
	}
	if nb := sse(BilinearNeighborTaps()); got > nb*(1+1e-9) {
		t.Fatalf("CLS SSE %.3f 意外劣于四邻域双线性 %.3f", got, nb)
	}
}

// 验收③：同一组图上，五抽头的平均与最大 Lab 色差都必须低于经典双线性路径。
func TestColorDifferenceBelowBilinear(t *testing.T) {
	r := solvedReport(t)
	if !(r.ClskLab.Mean < r.BilLab.Mean) {
		t.Fatalf("平均色差未降低: clsk %.4f >= bilinear %.4f", r.ClskLab.Mean, r.BilLab.Mean)
	}
	if !(r.ClskLab.Max < r.BilLab.Max) {
		t.Fatalf("最大色差未降低: clsk %.3f >= bilinear %.3f", r.ClskLab.Max, r.BilLab.Max)
	}
	// 同输入共址双线性 δ 上也必须改善，排除“输入不同”的口径争议。
	if !(r.ClskLab.Mean < r.CoLab.Mean && r.ClskLab.Max < r.CoLab.Max) {
		t.Fatalf("相对共址 δ 未全面胜出: clsk mean/max=%.4f/%.3f, δ=%.4f/%.3f",
			r.ClskLab.Mean, r.ClskLab.Max, r.CoLab.Mean, r.CoLab.Max)
	}
}

// 双线性路径不许删，且 bilinear -> clsk -> bilinear 切回后必须逐像素一致。
func TestSwitchBackIdentical(t *testing.T) {
	s := solvedReport(t).Solved
	for idx, im := range Suite() {
		observed := Soften(im)
		b1, err := Reconstruct(MethodBilinear, observed, s.H)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Reconstruct(MethodCLSK, observed, s.H); err != nil {
			t.Fatal(err)
		}
		b2, err := Reconstruct(MethodBilinear, observed, s.H)
		if err != nil {
			t.Fatal(err)
		}
		if len(b1.Pix) != len(b2.Pix) {
			t.Fatalf("图 %d 长度变化", idx)
		}
		for i := range b1.Pix {
			if b1.Pix[i] != b2.Pix[i] {
				t.Fatalf("图 %d 像素 %d 切回双线性后不一致: %+v vs %+v", idx, i, b1.Pix[i], b2.Pix[i])
			}
		}
	}
}

// 把“现在”的双线性平均/最大色差锁成基线；任何让切回结果漂移的改动都会被抓到。
func TestBilinearPinnedMetrics(t *testing.T) {
	r := solvedReport(t)
	const (
		pinMean = 0.7309968210702789
		pinMax  = 68.73607079663752
		eps     = 1e-6
	)
	if math.Abs(r.BilLab.Mean-pinMean) > eps {
		t.Fatalf("双线性平均色差漂移: got %.12f want %.12f", r.BilLab.Mean, pinMean)
	}
	if math.Abs(r.BilLab.Max-pinMax) > eps {
		t.Fatalf("双线性最大色差漂移: got %.12f want %.12f", r.BilLab.Max, pinMax)
	}
}

// 同尺寸下教科书双线性必须是恒等映射（像素中心映射 + 权重为 1 的单点）。
func TestBilinearScaleSameSizeIdentity(t *testing.T) {
	im := Suite()[0]
	got := BilinearScale(im, im.W, im.H)
	for i := range im.Pix {
		if math.Abs(got.Pix[i].R-im.Pix[i].R) > 1e-12 ||
			math.Abs(got.Pix[i].G-im.Pix[i].G) > 1e-12 ||
			math.Abs(got.Pix[i].B-im.Pix[i].B) > 1e-12 {
			t.Fatalf("同尺寸双线性非恒等 at %d: %+v vs %+v", i, got.Pix[i], im.Pix[i])
		}
	}
}

// 常数图经过 2x2 均值减半再放大必须保持常数。
func TestBilinearConstantImage(t *testing.T) {
	im := NewImage(16, 12)
	c := RGB{0.2, 0.5, 0.9}
	for i := range im.Pix {
		im.Pix[i] = c
	}
	rt := BilinearRoundTrip(im)
	for i, p := range rt.Pix {
		if math.Abs(p.R-c.R) > 1e-12 || math.Abs(p.G-c.G) > 1e-12 || math.Abs(p.B-c.B) > 1e-12 {
			t.Fatalf("常数图往返后变化 at %d: %+v", i, p)
		}
	}
}
