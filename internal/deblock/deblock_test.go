package deblock

import (
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

func approx(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Fatalf("%s = %.6f, want %.6f ± %.2e", name, got, want, tol)
	}
}

func TestOptimalAlphaClosedForm(t *testing.T) {
	// 若干组合：闭式解必须与 C(a)=Vn(1-a)^2+Vs a^2 的数值最优一致。
	for _, tc := range []struct{ vn, vs, want float64 }{
		{42, 318, 42.0 / 360.0},
		{340, 314, 340.0 / 654.0},
		{425, 288, 425.0 / 713.0},
		{0, 100, 0},
		{100, 0, 1},
	} {
		got := OptimalAlpha(tc.vn, tc.vs)
		approx(t, "alpha", got, tc.want, 1e-12)

		st := Stats{Vn: tc.vn, Vs: tc.vs}
		aGrid, cGrid := GridOptimal(st, 20001)
		if math.Abs(aGrid-got) > 1e-3 {
			t.Fatalf("闭式 %.4f 与数值最优 %.4f 不一致", got, aGrid)
		}
		// 闭式点代价不应高于网格最优（网格只可能因离散化略高）。
		if Cost(st, got) > cGrid+1e-9 {
			t.Fatalf("闭式代价未取到最小值: %.6f > grid %.6f", Cost(st, got), cGrid)
		}
		// 导数在最优点为零。
		d := -2*tc.vn*(1-got) + 2*tc.vs*got
		approx(t, "C'(alpha*)", d, 0, 1e-9)
	}
}

func TestTriangularBoundaryErrorVariance(t *testing.T) {
	// e1,e2 独立 ~ U[-Q/2,Q/2]，d=e1-e2 为三角分布，E[d^2]=Q^2/6。
	const Q = 16.0
	rng := rand.New(rand.NewSource(20261125))
	var sum float64
	const N = 400000
	for i := 0; i < N; i++ {
		e1 := (rng.Float64() - 0.5) * Q
		e2 := (rng.Float64() - 0.5) * Q
		d := e1 - e2
		sum += d * d
	}
	got := sum / N
	want := Q * Q / 6
	approx(t, "E[(e1-e2)^2]", got, want, 0.6) // 蒙特卡洛容差
}

func TestBlendScalesBoundaryDifference(t *testing.T) {
	// 4x1，B=2：唯一边界对是下标 1|2，差为 10；a=0.5 后差应为 5。
	g := &Gray{W: 4, H: 1, Pix: []uint8{0, 0, 10, 10}}
	out := Deblock(g, 2, 0.5)
	if d := int(out.Pix[2]) - int(out.Pix[1]); d != 5 {
		t.Fatalf("混合后边界差应为 5，实际 %d (%v)", d, out.Pix)
	}
	// 均值保持：(0+10)/2 == (3+8)/2（取整误差内）。
	if math.Abs(float64(out.Pix[1])+float64(out.Pix[2])-10) > 1 {
		t.Fatalf("像素对和应近似不变: %v", out.Pix)
	}
}

func TestOffIdentity(t *testing.T) {
	src := SampleImage(64, 64)
	for _, B := range []int{4, 8, 16} {
		comp := BlockCompress(src, B, 16)
		out := Deblock(comp, B, 0)
		if !equalPix(out, comp) {
			t.Fatalf("B=%d 滤波关闭时产物发生变化", B)
		}
		// 关闭路径返回拷贝而非改入参也可，但内容必须一致。
		out.Pix[0]++
		if comp.Pix[0] == out.Pix[0] {
			t.Fatal("关闭路径应返回独立拷贝")
		}
	}
}

func TestStudyClaims(t *testing.T) {
	rep, err := Study("")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Rows) != 3 {
		t.Fatalf("应有三档，实际 %d", len(rep.Rows))
	}
	if !rep.OffSameAll {
		t.Fatal("存在档位在 alpha=0 时不恒等")
	}
	wantB := []int{4, 8, 16}
	var prev float64 = -1
	for i, r := range rep.Rows {
		if r.B != wantB[i] {
			t.Fatalf("档位顺序错误: %d", r.B)
		}
		if !r.OffSame {
			t.Fatalf("B=%d off 不恒等", r.B)
		}
		approx(t, "闭式/数值一致", r.AlphaStar, r.AlphaGrid, 5e-3)
		if r.AlphaStar <= prev {
			t.Fatalf("最优强度应随块长单调上升: %v", []float64{prev, r.AlphaStar})
		}
		prev = r.AlphaStar
	}
	// 题面核心对照：三档等权平均下，边界跳变更低且细节损失不升（留安全余量）。
	if !(rep.Agg.JRatio < 0.95) {
		t.Fatalf("边界跳变未明显下降: JRatio=%.4f", rep.Agg.JRatio)
	}
	if !(rep.Agg.DRatio <= 1.0) {
		t.Fatalf("细节损失上升: DRatio=%.4f", rep.Agg.DRatio)
	}
	if rep.Agg.DRatio > 0.97 {
		t.Fatalf("细节损失应至少不升并留有复算余量: DRatio=%.4f", rep.Agg.DRatio)
	}
}

func TestStudyWritesArtifacts(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "deblock")
	if _, err := Study(out); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"source.pgm", "metrics.json", "metrics.csv", "table.md"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatalf("缺少产物 %s: %v", name, err)
		}
	}
	matches, _ := filepath.Glob(filepath.Join(out, "B*_optimal_a*.pgm"))
	if len(matches) != 3 {
		t.Fatalf("应有 3 张最优产物，实际 %d", len(matches))
	}
	off, err := os.ReadFile(filepath.Join(out, "B8_compressed_off.pgm"))
	if err != nil {
		t.Fatal(err)
	}
	if len(off) == 0 || string(off[:2]) != "P5" {
		t.Fatal("off 产物不是 PGM")
	}
}
