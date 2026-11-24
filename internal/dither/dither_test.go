package dither

import (
	_ "embed"
	"encoding/json"
	"math"
	"testing"
)

// 参考值由 Go 权威实现通过 DUMP_REF=1 导出（见 git 历史中的生成命令），
// analysis/dither_prototype.py 用 numpy 独立验证算法与量级。
// 矩阵值表面的 LF 在两实现间有 ~1e-13 的浮点路径差（朴素 DFT vs FFT），
// 故频谱指标按相对容差比较，不要求逐位相等。
//
//go:embed testdata/reference_values.json
var referenceJSON []byte

type refMetrics struct {
	Matrix   []int   `json:"matrix"`
	LF       float64 `json:"lf"`
	LFWide   float64 `json:"lf_wide"`
	Total    float64 `json:"total"`
	PAR      float64 `json:"par"`
	Anis     float64 `json:"anis"`
	BinaryLF float64 `json:"binary_lf"`
	GoMSE    float64 `json:"go_mse"`
	GoMAE    float64 `json:"go_mae"`
}

func loadReferences(t *testing.T) map[string]refMetrics {
	t.Helper()
	var refs map[string]refMetrics
	if err := json.Unmarshal(referenceJSON, &refs); err != nil {
		t.Fatalf("解析参考值失败: %v", err)
	}
	return refs
}

func approx(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// relApprox 用相对容差，兼容大数值（~1e5）与小数值（~1e1）两类指标。
func relApprox(a, b, rel float64) bool {
	return math.Abs(a-b) <= rel*math.Max(1.0, math.Max(math.Abs(a), math.Abs(b)))
}

func TestMatricesMatchReference(t *testing.T) {
	refs := loadReferences(t)
	cases := []struct {
		key, name string
	}{
		{"bayer", NameBayer},
		{"vac", NameVoidCluster},
		{"spectral", NameSpectral},
	}
	for _, c := range cases {
		m, ok := Matrix(c.name)
		if !ok {
			t.Fatalf("未知矩阵 %q", c.name)
		}
		ref := refs[c.key]
		if len(m) != len(ref.Matrix) {
			t.Fatalf("%s 长度不一致", c.name)
		}
		for i, v := range m {
			if v != ref.Matrix[i] {
				t.Fatalf("%s 位置 %d: Go=%d Python=%d", c.name, i, v, ref.Matrix[i])
			}
		}
		// 必须是 0..63 的排列。
		seen := make(map[int]bool)
		for _, v := range m {
			if v < 0 || v >= Size*Size || seen[v] {
				t.Fatalf("%s 不是 0..63 的排列（值 %d）", c.name, v)
			}
			seen[v] = true
		}
	}
}

func TestSpectralMetricsMatchReference(t *testing.T) {
	refs := loadReferences(t)
	suite, w, h := BuildTestSuite()
	cases := []struct {
		key, name string
	}{
		{"bayer", NameBayer},
		{"vac", NameVoidCluster},
		{"spectral", NameSpectral},
	}
	for _, c := range cases {
		m, _ := Matrix(c.name)
		got := Evaluate(m, suite, w, h)
		ref := refs[c.key]
		checks := []struct {
			label string
			a, b  float64
			rel   float64
		}{
			{"低频泄漏", got.LowFreqLeak, ref.LF, 1e-12},
			{"宽低频泄漏", got.LowFreqWide, ref.LFWide, 1e-12},
			{"总功率", got.TotalPower, ref.Total, 1e-12},
			{"PAR", got.PAR, ref.PAR, 1e-12},
			{"各向异性", got.Anisotropy, ref.Anis, 1e-12},
			{"二值低频", got.BinaryLF, ref.BinaryLF, 1e-12},
			{"MSE", got.MSE, ref.GoMSE, 1e-12},
			{"MAE", got.MAE, ref.GoMAE, 1e-12},
		}
		for _, ck := range checks {
			if !relApprox(ck.a, ck.b, ck.rel) {
				t.Errorf("%s %s: Go=%.10f ref=%.10f", c.name, ck.label, ck.a, ck.b)
			}
		}
		// Parseval：去均值信号的总功率应等于 N²*Σx²。
		var sumSq float64
		mean := float64(Size*Size-1) / 2 // 0..63 的均值
		for _, v := range m {
			d := float64(v) - mean
			sumSq += d * d
		}
		if !approx(got.TotalPower, float64(Size*Size)*sumSq, 1e-6) {
			t.Errorf("%s Parseval 校验失败: %.4f vs %.4f", c.name, got.TotalPower, float64(Size*Size)*sumSq)
		}
	}
}

func TestSpectralBeatsBayerOnPatternAndError(t *testing.T) {
	refs := loadReferences(t)
	bayer := refs["bayer"]
	spec := refs["spectral"]

	// 硬指标 1：低频泄漏显著下降（至少降两个数量级）。
	if !(spec.LF < bayer.LF/100) {
		t.Errorf("spectral 低频泄漏未显著下降: %f vs %f", spec.LF, bayer.LF)
	}
	// 硬指标 2：规则纹对比度下降（PAR 降一半以上、各向异性下降）。
	if !(spec.PAR < bayer.PAR*0.5) {
		t.Errorf("spectral PAR 未下降一半: %f vs %f", spec.PAR, bayer.PAR)
	}
	if !(spec.Anis < bayer.Anis*0.6) {
		t.Errorf("spectral 各向异性未下降: %f vs %f", spec.Anis, bayer.Anis)
	}
	// 硬指标 3：量化误差不上升。三矩阵同为 0..63 排列，灰场 MSE 严格
	// 相等（见 TestFlatFieldMSEEqual）；全集 MSE 的差异仅来自特定图像
	// 内容的采样波动，限定在 0.1% 内即视为无系统性上升。
	rel := (spec.GoMSE - bayer.GoMSE) / bayer.GoMSE
	if rel > 1e-3 {
		t.Errorf("spectral 全集 MSE 系统性上升 %.4f%%", 100*rel)
	}
}

// TestFlatFieldMSEEqual 验证排列矩阵的理论性质：在无内容偏置的均匀
// 灰场上，三种矩阵的均方量化误差严格相等（噪声同分布、同方差）。
func TestFlatFieldMSEEqual(t *testing.T) {
	suite, w, h := BuildTestSuite()
	var first float64
	for k, name := range []string{NameBayer, NameVoidCluster, NameSpectral} {
		m, _ := Matrix(name)
		var se float64
		n := 0
		for img := 0; img < 7; img++ { // 前 7 张为均匀灰场
			out, _ := Dither(w, h, suite[img], m)
			for i, v := range suite[img] {
				d := float64(out[i]) - float64(v)
				se += d * d
				n++
			}
		}
		mse := se / float64(n)
		if k == 0 {
			first = mse
		} else if mse != first {
			t.Errorf("%s 灰场 MSE=%f 与 bayer=%f 不相等", name, mse, first)
		}
	}
}

func TestDitherIsDeterministicAndBinary(t *testing.T) {
	m, _ := Matrix(NameSpectral)
	suite, w, h := BuildTestSuite()
	img := suite[0]
	out1, err := Dither(w, h, img, m)
	if err != nil {
		t.Fatal(err)
	}
	out2, _ := Dither(w, h, img, m)
	for i := range out1 {
		if out1[i] != out2[i] {
			t.Fatal("抖动结果不确定")
		}
		if out1[i] != 0 && out1[i] != 255 {
			t.Fatalf("输出不是二值: %d", out1[i])
		}
	}
}

func TestDitherGray128Density(t *testing.T) {
	// 灰场 128（scaled=32）下，每个 8×8 周期应有恰好 32 个白点。
	m, _ := Matrix(NameBayer)
	img := make([]uint8, Size*Size)
	for i := range img {
		img[i] = grayLevel
	}
	out, err := Dither(Size, Size, img, m)
	if err != nil {
		t.Fatal(err)
	}
	white := 0
	for _, v := range out {
		if v == 255 {
			white++
		}
	}
	if white != halfDensity {
		t.Fatalf("128 灰场白点数=%d，期望 %d", white, halfDensity)
	}
}

func TestDitherRejectsBadInput(t *testing.T) {
	m, _ := Matrix(NameBayer)
	if _, err := Dither(0, 4, make([]uint8, 0), m); err == nil {
		t.Fatal("零画幅应报错")
	}
	if _, err := Dither(2, 2, make([]uint8, 3), m); err == nil {
		t.Fatal("像素数不匹配应报错")
	}
}

func TestUnknownMatrix(t *testing.T) {
	if _, ok := Matrix("not-a-matrix"); ok {
		t.Fatal("未知矩阵名应返回 ok=false")
	}
}

func TestLCGStream(t *testing.T) {
	// 与 Python 原型的 LCG 前 5 个输出对齐。
	rng := NewLCG(SeedVAC)
	want := []float64{
		float64((lcgA*SeedVAC+lcgC)&lcgMask) / lcgM,
	}
	for i := 1; i < 5; i++ {
		s := uint32(0)
		// 重新独立推一遍，避免共用状态。
		s = SeedVAC & lcgMask
		for k := 0; k <= i; k++ {
			s = (lcgA*s + lcgC) & lcgMask
		}
		want = append(want, float64(s)/lcgM)
	}
	for i, w := range want {
		if got := rng.Rand(); !approx(got, w, 1e-15) {
			t.Errorf("LCG 第 %d 个值: got=%v want=%v", i, got, w)
		}
	}
}
