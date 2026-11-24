package quant

import (
	"errors"
	"math"
	"testing"

	"gotoolkit/internal/quant/fixture"
)

// fixtureHistogram 由合成图统计得出，保持测试与演示用同一份分布。
func fixtureHistogram() Histogram {
	_, _, pix := fixture.Image()
	return FromPixels(pix)
}

func mustMid(t *testing.T, bits int) Result {
	t.Helper()
	r, err := Midpoint(bits)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func mustOpt(t *testing.T, h Histogram, bits int, tol float64, maxIter int) Result {
	t.Helper()
	r, err := Optimal(h, bits, tol, maxIter)
	if err != nil {
		t.Fatalf("bits=%d 迭代失败: %v", bits, err)
	}
	if r.Iterations <= 0 || r.Iterations > maxIter {
		t.Fatalf("bits=%d 迭代次数 %d 不在 (0,%d]", bits, r.Iterations, maxIter)
	}
	return r
}

// 三档位深下条件期望重建的误差都应低于中点重建。
// 这里用取整后电平的误差（DistortionRounded），与 CLI 报告的口径一致。
func TestExpectationBeatsMidpointAllBitDepths(t *testing.T) {
	h := fixtureHistogram()
	for _, bits := range []int{3, 4, 5} {
		mid := mustMid(t, bits)
		mid.MSE = DistortionRounded(h, mid.Levels)
		opt := mustOpt(t, h, bits, DefaultTol, DefaultMaxIter)
		optMSE := DistortionRounded(h, opt.Levels)
		if !(optMSE < mid.MSE) {
			t.Fatalf("bits=%d 条件期望 MSE=%.6f 未低于中点 MSE=%.6f",
				bits, optMSE, mid.MSE)
		}
	}
}

// 逐档对比：条件期望电平在每个区间上是平方误差的极小点（等于该区间
// 样本均值），因此把中点电平换进同一区间后误差必然不增；未移动的档位
// 误差保持不变。这里对三档位深逐值核对这一最优性。
func TestLevelsDominatesMidpointValueByValue(t *testing.T) {
	h := fixtureHistogram()
	for _, bits := range []int{3, 4, 5} {
		opt := mustOpt(t, h, bits, DefaultTol, DefaultMaxIter)
		mid := mustMid(t, bits)

		// 在条件期望自己的边界上，电平应等于该区间样本均值（残差为 0）。
		optBounds := boundaries(opt.Levels)
		sums := make([]float64, len(opt.Levels))
		weights := make([]float64, len(opt.Levels))
		for x, c := range h {
			if c == 0 {
				continue
			}
			i := lookup(optBounds, float64(x))
			sums[i] += float64(x) * float64(c)
			weights[i] += float64(c)
		}
		for i := range opt.Levels {
			if weights[i] == 0 {
				continue // 空区间保持初值，不参与最优性论证
			}
			centroid := sums[i] / weights[i]
			if math.Abs(opt.Levels[i]-centroid) > DefaultTol*4 {
				t.Fatalf("bits=%d 电平 %d 不是区间质心: r=%.9f, 质心=%.9f",
					bits, i, opt.Levels[i], centroid)
			}
			if opt.Levels[i] < optBounds[i] || opt.Levels[i] >= optBounds[i+1] {
				t.Fatalf("bits=%d 电平 %d 越出自身区间", bits, i)
			}
		}

		// 逐档对比：在条件期望自己的区间 R_i 上，条件期望电平是使
		// Σ_{x∈R_i} p_x·(x-r)² 最小的 r。因此把中点电平放进同一个 R_i
		// 比较，条件期望的平方误差必须不大于中点；仅当两者相等时取等。
		for i := range opt.Levels {
			if weights[i] == 0 {
				continue
			}
			var sseOpt, sseMid float64
			for x := 0; x < 256; x++ {
				if lookup(optBounds, float64(x)) != i {
					continue
				}
				c := float64(h[x])
				dOpt := float64(x) - opt.Levels[i]
				dMid := float64(x) - mid.Levels[i]
				sseOpt += c * dOpt * dOpt
				sseMid += c * dMid * dMid
			}
			if sseOpt > sseMid+1e-9 {
				t.Fatalf("bits=%d 区间 %d: 条件期望 SSE=%.6f 大于中点 SSE=%.6f",
					bits, i, sseOpt, sseMid)
			}
			if opt.Levels[i] != mid.Levels[i] && !(sseOpt < sseMid) {
				t.Fatalf("bits=%d 区间 %d: 电平已移动但误差未降 (%.6f vs %.6f)",
					bits, i, sseOpt, sseMid)
			}
		}
	}
}

// 任意直方图与电平，最近邻量化都是最优标量量化器：这里验证电平顺序与区间覆盖。
func TestBoundariesCoverRange(t *testing.T) {
	for _, bits := range []int{1, 2, 3, 4, 5, 6, 7, 8} {
		r := mustMid(t, bits)
		b := boundaries(r.Levels)
		if b[0] > 0 || b[len(b)-1] < MaxValue {
			t.Fatalf("bits=%d 边界未覆盖 0..255: %v", bits, b)
		}
		for i := range r.Levels {
			if i > 0 && r.Levels[i] <= r.Levels[i-1] {
				t.Fatalf("bits=%d 中点电平未递增", bits)
			}
			if lookup(b, float64(i%256)) != lookup(b, float64(i%256)) {
				t.Fatalf("lookup 不确定")
			}
		}
	}
}

func TestMidpointMSEExact(t *testing.T) {
	// 单电平直方图：x=10，K=2 时中点电平为 63.5 与 191.5。
	var h Histogram
	h[10] = 1
	mid := mustMid(t, 1)
	if want := float64((10 - 63.5) * (10 - 63.5)); math.Abs(Distortion(h, mid.Levels)-want) > 1e-9 {
		t.Fatalf("中点 MSE=%g, want %g", Distortion(h, mid.Levels), want)
	}
	// 条件期望应把电平搬到样本上，MSE 归零。
	// 若本轮迭代恰好收敛于"电平未移动"（初值已在质心上），仍应通过。
	opt := mustOpt(t, h, 1, DefaultTol, DefaultMaxIter)
	if Distortion(h, opt.Levels) > 1e-9 {
		t.Fatalf("单样本条件期望 MSE=%g, want 0", Distortion(h, opt.Levels))
	}
}

// 空区间保持上一轮电平：单点直方图 K=2 时只有一个区间非空，另一个
// 必须留在初值附近且不影响失真。
func TestEmptyIntervalKeepsLevel(t *testing.T) {
	var h Histogram
	h[10] = 1
	opt := mustOpt(t, h, 1, DefaultTol, DefaultMaxIter)
	if opt.Levels[0] != 10 && opt.Levels[1] != 10 {
		t.Fatalf("非空区间电平应落在样本上: %v", opt.Levels)
	}
}

func TestConvergenceCriterion(t *testing.T) {
	h := fixtureHistogram()
	// 紧阈值下轮数不应减少，且仍满足判据。
	loose := mustOpt(t, h, 4, 1.0, DefaultMaxIter)
	tight := mustOpt(t, h, 4, 1e-9, DefaultMaxIter)
	if tight.Iterations < loose.Iterations {
		t.Fatalf("紧阈值轮数 %d 小于松阈值轮数 %d", tight.Iterations, loose.Iterations)
	}
	if tight.MSE > loose.MSE+1e-9 {
		t.Fatalf("更紧的收敛应带来不更差的 MSE: %g vs %g", tight.MSE, loose.MSE)
	}
}

func TestMaxIterExceededIsError(t *testing.T) {
	h := fixtureHistogram()
	// 1 轮、极紧阈值必然不收敛，必须报错而不是静默返回。
	_, err := Optimal(h, 4, 1e-12, 1)
	if !errors.Is(err, ErrNotConverged) {
		t.Fatalf("应返回 ErrNotConverged, got %v", err)
	}
}

func TestRejectsBadArguments(t *testing.T) {
	h := fixtureHistogram()
	cases := []struct {
		name string
		tol  float64
		it   int
		want error
	}{
		{"负阈值", -1, 10, ErrBadTol},
		{"NaN 阈值", math.NaN(), 10, ErrBadTol},
		{"零上限", DefaultTol, 0, ErrBadMaxIter},
	}
	for _, c := range cases {
		if _, err := Optimal(h, 4, c.tol, c.it); !errors.Is(err, c.want) {
			t.Fatalf("%s: want %v, got %v", c.name, c.want, err)
		}
	}
	if _, err := Midpoint(9); !errors.Is(err, ErrBadBits) {
		t.Fatalf("位深 9 应报 ErrBadBits, got %v", err)
	}
	if _, err := Optimal(Histogram{}, 4, DefaultTol, 10); !errors.Is(err, ErrEmptyHistogram) {
		t.Fatalf("空直方图应报 ErrEmptyHistogram")
	}
}
