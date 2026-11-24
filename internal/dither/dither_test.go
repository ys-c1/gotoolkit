package dither

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

type vectors struct {
	FS   vector `json:"fs"`
	RB15 vector `json:"rb15"`
	RB25 vector `json:"rb25"`
	RB35 vector `json:"rb35"`
}

type vector struct {
	X []float64 `json:"x"`
	Y []float64 `json:"y"`
	E []float64 `json:"e"`
}

func loadVectors(t *testing.T) *vectors {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "vectors.json"))
	if err != nil {
		t.Fatalf("读取参考向量失败: %v", err)
	}
	var v vectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("解析参考向量失败: %v", err)
	}
	return &v
}

func TestMatchesPythonReference(t *testing.T) {
	v := loadVectors(t)
	const n = 64
	cases := []struct {
		name string
		k    Kernel
		ref  vector
	}{
		{"floyd-steinberg", FloydSteinberg, v.FS},
		{"feedback-rb15", FeedbackRB15, v.RB15},
		{"feedback-rb25", FeedbackRB25, v.RB25},
		{"feedback-rb35", FeedbackRB35, v.RB35},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			y, e, err := Diffuse(c.ref.X, n, n, 32, c.k)
			if err != nil {
				t.Fatalf("Diffuse 返回错误: %v", err)
			}
			for i := range y {
				if math.Abs(y[i]-c.ref.Y[i]) > 1e-12 {
					t.Fatalf("y[%d] = %.12f, 参考 %.12f", i, y[i], c.ref.Y[i])
				}
				if math.Abs(e[i]-c.ref.E[i]) > 1e-12 {
					t.Fatalf("e[%d] = %.12f, 参考 %.12f", i, e[i], c.ref.E[i])
				}
			}
		})
	}
}

func TestErrorBoundedByHalfStep(t *testing.T) {
	// 平坦输入取两个量化电平中点，是误差扩散最易发散的工况。
	const w, h, levels = 48, 48, 32
	q := 1.0 / (levels - 1)
	x := make([]float64, w*h)
	for i := range x {
		x[i] = 3.5 * q
	}
	for _, k := range Kernels() {
		_, e, err := Diffuse(x, w, h, levels, k)
		if err != nil {
			t.Fatalf("%s: %v", k.Name, err)
		}
		for i, v := range e {
			if math.Abs(v) > q/2+1e-12 {
				t.Fatalf("%s: |e[%d]|=%.4f 超过半步 q/2=%.4f", k.Name, i, math.Abs(v), q/2)
			}
		}
	}
}

func TestRejectsBadInput(t *testing.T) {
	if _, _, err := Diffuse([]float64{0.1}, 2, 2, 32, FeedbackRB15); err == nil {
		t.Fatal("长度不匹配应报错")
	}
	if _, _, err := Diffuse(make([]float64, 4), 2, 2, 1, FeedbackRB15); err == nil {
		t.Fatal("levels<2 应报错")
	}
}

func TestCoefficientsSumAndDCGain(t *testing.T) {
	// 现成核 Σh=1（DC 增益 2）；反馈核 Σh<0（DC 增益 <1）。
	if s := sumH(FloydSteinberg); math.Abs(s-1.0) > 1e-12 {
		t.Fatalf("FS Σh = %v, 期望 1", s)
	}
	for _, k := range []Kernel{FeedbackRB15, FeedbackRB25, FeedbackRB35} {
		if s := sumH(k); s >= 0 {
			t.Fatalf("%s Σh = %v, 期望负反馈", k.Name, s)
		}
		if g := math.Abs(1 + sumH(k)); g >= 1.0 {
			t.Fatalf("%s |NTF(0)| = %v, 期望 <1", k.Name, g)
		}
	}
}

func sumH(k Kernel) float64 {
	return k.HRight + k.HBelow + k.HBelowR + k.HBelowL
}
