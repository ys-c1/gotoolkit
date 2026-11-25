package grayquant

import "testing"

// TestCanonicalTierTable 锁定三档位深、M=256 时题目要求的全部数字。
func TestCanonicalTierTable(t *testing.T) {
	want := map[int]struct {
		n, maxRun, zeroSteps, plateauRuns int
		injective                         bool
		mismatch                          int
	}{
		8:  {256, 1, 0, 0, true, 0},
		10: {1024, 4, 768, 256, false, 3072},
		12: {4096, 16, 3840, 256, false, 3840},
	}
	const w, h = 64, 64
	for _, bits := range BitsTiers {
		e := want[bits]
		n := Levels(bits)
		if n != e.n {
			t.Fatalf("bits=%d N=%d, want %d", bits, n, e.n)
		}
		lut := LUT(n, SharedIntervals)
		if zs := len(ZeroDiffSteps(lut)); zs != e.zeroSteps {
			t.Errorf("bits=%d zeroSteps=%d want %d", bits, zs, e.zeroSteps)
		}
		if mx := MaxRunLength(lut); mx != e.maxRun {
			t.Errorf("bits=%d maxRun=%d want %d", bits, mx, e.maxRun)
		}
		if pr := len(PlateauRuns(lut)); pr != e.plateauRuns {
			t.Errorf("bits=%d plateauRuns=%d want %d", bits, pr, e.plateauRuns)
		}
		if got := IsInjective(lut); got != e.injective {
			t.Errorf("bits=%d injective=%v want %v", bits, got, e.injective)
		}
		if MinIntervals(n) != n {
			t.Errorf("bits=%d Mmin must equal N=%d", bits, n)
		}
		src := FullRangeRamp(w, h, n)
		if got := RoundtripCount(src, n, SharedIntervals); got != e.mismatch {
			t.Errorf("bits=%d mismatch=%d want %d", bits, got, e.mismatch)
		}
	}
}

// TestReversibleTierZeroOnAnyBytes 强调 8bit/M=256 是恒等映射：
// 任意输入（不只是梯度）往返差异都必须为 0。
func TestReversibleTierZeroOnAnyBytes(t *testing.T) {
	lut := LUT(256, 256)
	for x := 0; x < 256; x++ {
		if lut[x] != x || Repr(lut[x], 256, 256) != x {
			t.Fatalf("M=N 必须恒等，x=%d lut=%d", x, lut[x])
		}
	}
	// 确定性构造的任意字节序列。
	xs := make([]int, 4096)
	seed := uint32(1)
	for i := range xs {
		seed = seed*1664525 + 1013904223
		xs[i] = int(seed >> 16 & 0xff)
	}
	if got := RoundtripCount(xs, 256, 256); got != 0 {
		t.Fatalf("可逆档任意图像差异应为 0，实际 %d", got)
	}
}

// TestMGeNAlwaysInjective 验证定理：M>=N 时 T 严格递增。
func TestMGeNAlwaysInjective(t *testing.T) {
	for n := 2; n <= 200; n++ {
		for _, m := range []int{n, n + 1, 2 * n, 3*n + 1} {
			if !IsInjective(LUT(n, m)) {
				t.Fatalf("N=%d M=%d 应可逆", n, m)
			}
			if zs := ZeroDiffSteps(LUT(n, m)); len(zs) != 0 {
				t.Fatalf("N=%d M=%d 不应有零差边", n, m)
			}
		}
	}
	// 三档在 M=N/N+1 边界同样成立。
	for _, bits := range BitsTiers {
		n := Levels(bits)
		for _, m := range []int{n - 1, n, n + 1} {
			got := IsInjective(LUT(n, m))
			want := m >= n
			if got != want {
				t.Fatalf("bits=%d M=%d injective=%v want %v", bits, m, got, want)
			}
		}
	}
}

// TestMLtNAlwaysHasPlateau 验证必要性：M<N 时必有至少一条零差边。
func TestMLtNAlwaysHasPlateau(t *testing.T) {
	for n := 2; n <= 120; n++ {
		for m := 1; m < n; m++ {
			lut := LUT(n, m)
			if len(ZeroDiffSteps(lut)) == 0 {
				t.Fatalf("N=%d M=%d<N 必存在平台", n, m)
			}
			if IsInjective(lut) {
				t.Fatalf("N=%d M=%d<N 必不可逆", n, m)
			}
		}
	}
}

// TestMinIntervalsExhaustive 对小 N 穷举：首个无零差边的 M 恰为 N。
func TestMinIntervalsExhaustive(t *testing.T) {
	for n := 2; n <= 64; n++ {
		first := -1
		for m := 1; m <= n; m++ {
			if IsInjective(LUT(n, m)) {
				first = m
				break
			}
		}
		if first != n {
			t.Fatalf("N=%d 最小区间数=%d，应为 %d", n, first, n)
		}
	}
}

// TestAdjacentZeroIsPlateauCriterion 直接锁定题目指定判据：
// 平台段 ⇔ 相邻映射值之差为零；并交叉核验模运算充要条件。
func TestAdjacentZeroIsPlateauCriterion(t *testing.T) {
	for _, bits := range BitsTiers {
		n := Levels(bits)
		lut := LUT(n, SharedIntervals)
		d := AdjacentDiff(lut)
		if len(d) != n-1 {
			t.Fatalf("bits=%d diff 长度 %d", bits, len(d))
		}
		for i := 1; i < n; i++ {
			zeroByDiff := d[i-1] == 0
			zeroByEq := lut[i]-lut[i-1] == 0
			if zeroByDiff != zeroByEq {
				t.Fatalf("bits=%d i=%d 差值判据与相等判据不一致", bits, i)
			}
			if SharedIntervals < n {
				if pred := ModularZeroCondition(i, n, SharedIntervals); pred != zeroByDiff {
					t.Fatalf("bits=%d i=%d 模条件=%v 差值判据=%v",
						bits, i, pred, zeroByDiff)
				}
			}
		}
	}
	// 小 N,M 全组合交叉核验模条件。
	for n := 2; n <= 80; n++ {
		for m := 1; m < n; m++ {
			lut := LUT(n, m)
			for i := 1; i < n; i++ {
				got := lut[i] == lut[i-1]
				if pred := ModularZeroCondition(i, n, m); pred != got {
					t.Fatalf("N=%d M=%d i=%d 模条件=%v 实际零差=%v",
						n, m, i, pred, got)
				}
			}
		}
	}
}

// TestExactPowerRuns 三档 M=256 整除 N：平台段必须是宽度 w=N/M 的整块。
func TestExactPowerRuns(t *testing.T) {
	for _, bits := range BitsTiers {
		n := Levels(bits)
		w := n / SharedIntervals
		lut := LUT(n, SharedIntervals)
		runs := EqualRuns(lut)
		if len(runs) != SharedIntervals {
			t.Fatalf("bits=%d run 数=%d want %d", bits, len(runs), SharedIntervals)
		}
		for j, r := range runs {
			if r.Start != j*w || r.End != (j+1)*w-1 || r.Length != w {
				t.Fatalf("bits=%d run %d = [%d,%d] len %d, 期望块 [%d,%d] w=%d",
					bits, j, r.Start, r.End, r.Length, j*w, (j+1)*w-1, w)
			}
			if r.Value != j {
				t.Fatalf("bits=%d run %d value=%d want %d", bits, j, r.Value, j)
			}
		}
	}
}
