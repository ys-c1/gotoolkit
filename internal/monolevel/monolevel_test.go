package monolevel

import (
	"math"
	"math/rand"
	"testing"
)

// TestPartitionAnyStepSequence 第一步对任意正步长序列都成立：
// 边界从 0 起严格递增，末项恰为 256。
func TestPartitionAnyStepSequence(t *testing.T) {
	cases := [][]int{
		{256},
		{1, 255},
		{1, 1, 254},
		{128, 128},
		{32, 32, 32, 32, 32, 32, 32, 32},
		{3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10},
		{6, 7, 8, 8, 9, 11, 12, 13, 15, 17, 19, 21, 23, 26, 29, 32},
	}
	for _, steps := range cases {
		bounds, err := Partition(steps)
		if err != nil {
			t.Fatalf("steps=%v: %v", steps, err)
		}
		if bounds[0] != 0 || bounds[len(bounds)-1] != DomainN {
			t.Fatalf("steps=%v 端点错误: %v", steps, bounds)
		}
		for k := 0; k+1 < len(bounds); k++ {
			if bounds[k] >= bounds[k+1] {
				t.Fatalf("steps=%v 边界非严格递增 at %d: %v", steps, k, bounds)
			}
		}
	}
}

func TestPartitionRejects(t *testing.T) {
	bad := [][]int{
		nil,
		{},
		{0},
		{-1, 257},
		{128, 127},
		{300},
	}
	for _, steps := range bad {
		if _, err := Partition(steps); err == nil {
			t.Fatalf("steps=%v 应被拒绝", steps)
		}
	}
}

// TestPAVAMonotoneForAnyInput 第二步对任意实数表都给出非降解：逐对比较，不抽样。
func TestPAVAMonotoneForAnyInput(t *testing.T) {
	rng := rand.New(rand.NewSource(20260101))
	for trial := 0; trial < 2000; trial++ {
		n := 1 + rng.Intn(40)
		a := make([]float64, n)
		w := make([]float64, n)
		for k := range a {
			a[k] = rng.Float64()*500 - 250
			w[k] = 0.01 + rng.Float64()*4
		}
		r, blocks, err := PAVA(a, w)
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		for k := 0; k+1 < n; k++ {
			if r[k] > r[k+1] {
				t.Fatalf("n=%d 第 %d 对逆序: r=%v", n, k, r)
			}
		}
		// 块划分覆盖全部下标，块值=块内加权平均。
		if blocks[0].Lo != 0 || blocks[len(blocks)-1].Hi != n {
			t.Fatalf("块划分未覆盖全部下标: %+v", blocks)
		}
		for i, b := range blocks {
			if i > 0 && blocks[i-1].Hi != b.Lo {
				t.Fatalf("块不连续: %+v", blocks)
			}
			lo, hi := b.Lo, b.Hi
			var wsum, asum float64
			for k := lo; k < hi; k++ {
				wsum += w[k]
				asum += w[k] * a[k]
			}
			want := asum / wsum
			if math.Abs(want-b.Value) > 1e-9 {
				t.Fatalf("块 [%d,%d) 值 %.12f != 加权平均 %.12f", lo, hi, b.Value, want)
			}
		}
	}
}

func TestPAVARejectsBadWeights(t *testing.T) {
	a := []float64{1, 2, 3}
	cases := [][]float64{
		{1, 0, 1},
		{1, -1, 1},
		{1, math.NaN(), 1},
		{1, math.Inf(1), 1},
		{1, 2},
	}
	for _, w := range cases {
		if _, _, err := PAVA(a, w); err == nil {
			t.Fatalf("w=%v 应被拒绝", w)
		}
	}
	if _, _, err := PAVA(nil, nil); err == nil {
		t.Fatal("空输入应被拒绝")
	}
}

// TestPAVAOptimalityByBruteForce 在小规模随机实例上，用随机可行点与
// 穷举相邻格合并（Catalan 数意义下的全部块划分）两种方式核对 PAVA 解的最优性。
func TestPAVAOptimalityByBruteForce(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for trial := 0; trial < 200; trial++ {
		n := 2 + rng.Intn(7)
		a := make([]float64, n)
		w := make([]float64, n)
		for k := range a {
			a[k] = rng.Float64()*200 - 100
			w[k] = 0.05 + rng.Float64()*3
		}
		r, _, err := PAVA(a, w)
		if err != nil {
			t.Fatal(err)
		}
		got := weightedSSE(a, w, r)

		// 全部单调可行表中的最优值：最优单调阶梯函数必为相邻合并且块常数的形式，
		// 故只需枚举 2^(n-1) 种相邻合并划分，再筛掉块均值不非降的（不可行）划分。
		var best float64 = math.Inf(1)
		for mask := 0; mask < 1<<(n-1); mask++ {
			cand := blocksFromMask(a, w, mask)
			if !isNondecreasing(cand) {
				continue
			}
			if v := weightedSSE(a, w, cand); v < best {
				best = v
			}
		}
		if math.Abs(got-best) > 1e-7*math.Max(1, best) {
			t.Fatalf("n=%d a=%v: PAVA %.10f 与穷举最优 %.10f 不符", n, a, got, best)
		}
		// 随机单调表不应击败 PAVA。
		for r := 0; r < 200; r++ {
			cand := randomMonotone(rng, a, n)
			if v := weightedSSE(a, w, cand); v < got-1e-9 {
				t.Fatalf("随机单调表 %.10f 击败 PAVA %.10f", v, got)
			}
		}
	}
}

func weightedSSE(a, w, r []float64) float64 {
	var s float64
	for k := range a {
		d := r[k] - a[k]
		s += w[k] * d * d
	}
	return s
}

// blocksFromMask 按相邻合并掩码（bit k 表示 k 与 k+1 合并）构造块均值表。
func blocksFromMask(a, w []float64, mask int) []float64 {
	n := len(a)
	r := make([]float64, n)
	i := 0
	for i < n {
		j := i
		for j+1 < n && mask&(1<<j) != 0 {
			j++
		}
		var W, S float64
		for k := i; k <= j; k++ {
			W += w[k]
			S += w[k] * a[k]
		}
		v := S / W
		for k := i; k <= j; k++ {
			r[k] = v
		}
		i = j + 1
	}
	return r
}

// TestArbitraryStepsRepairIsMonotone 定理 1 的直接检验：对任意正整数步长序列
// （和为 256）、任意实数名义表，两步构造都得到非降重建电平，展开成 256 点映射
// 后也非降。逐对比较，不抽样。
func TestArbitraryStepsRepairIsMonotone(t *testing.T) {
	rng := rand.New(rand.NewSource(31415))
	for trial := 0; trial < 300; trial++ {
		steps := randomPartition(rng)
		a := make([]float64, len(steps))
		w := make([]float64, len(steps))
		for k := range a {
			a[k] = rng.Float64() * 255
			w[k] = 0.01 + rng.Float64()*3
		}
		r, _, err := PAVA(a, w)
		if err != nil {
			t.Fatalf("steps=%v: %v", steps, err)
		}
		levels := RoundLevels(r)
		for k := 0; k+1 < len(levels); k++ {
			if levels[k] > levels[k+1] {
				t.Fatalf("steps=%v 电平逆序 at %d", steps, k)
			}
		}
		y, err := RampLevels(steps, levels)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i+1 < len(y); i++ {
			if y[i] > y[i+1] {
				t.Fatalf("steps=%v 映射逆序 at %d", steps, i)
			}
		}
	}
}

// TestMidpointAlwaysFeasible 对任意步长序列，中点表都非降（故是单调锥的可行点）。
func TestMidpointAlwaysFeasible(t *testing.T) {
	rng := rand.New(rand.NewSource(2718))
	for trial := 0; trial < 2000; trial++ {
		steps := randomPartition(rng)
		mid, err := MidpointLevels(steps)
		if err != nil {
			t.Fatal(err)
		}
		for k := 0; k+1 < len(mid); k++ {
			if mid[k] > mid[k+1] {
				t.Fatalf("steps=%v 中点逆序: %v", steps, mid)
			}
		}
	}
}

// TestRealOptimumNotAboveMidpointLS 实值最优目标恒不超过中点表（中点可行 + 最优）。
func TestRealOptimumNotAboveMidpointLS(t *testing.T) {
	rng := rand.New(rand.NewSource(1618))
	for trial := 0; trial < 500; trial++ {
		steps := randomPartition(rng)
		a := make([]float64, len(steps))
		w := make([]float64, len(steps))
		for k := range a {
			a[k] = rng.Float64() * 255
			w[k] = 0.01 + rng.Float64()*3
		}
		r, _, err := PAVA(a, w)
		if err != nil {
			t.Fatal(err)
		}
		mid, err := MidpointLevels(steps)
		if err != nil {
			t.Fatal(err)
		}
		jr := weightedSSE(a, w, r)
		jm := weightedSSE(a, w, f64(mid))
		if jr > jm+1e-9 {
			t.Fatalf("steps=%v J(opt)=%.6f > J(mid)=%.6f", steps, jr, jm)
		}
	}
}

// randomPartition 随机生成一个和为 256 的正整数步长序列。
func randomPartition(rng *rand.Rand) []int {
	n := 1 + rng.Intn(50)
	remaining := DomainN - n
	steps := make([]int, n)
	for k := range steps {
		steps[k] = 1
	}
	for remaining > 0 {
		j := rng.Intn(n)
		add := 1 + rng.Intn(remaining)
		steps[j] += add
		remaining -= add
	}
	return steps
}

// TestBlockPrefixMeanDominates PAVA 块的前缀/后缀均值引理（对偶可行的基石）：
// 每个最终块 [Lo,Hi) 的值为 v，则块内任一 [Lo,k] 的加权均值 ≥ v，
// 任一 [k,Hi) 的加权均值 ≤ v。逐块、逐位置枚举，不抽样。
func TestBlockPrefixMeanDominates(t *testing.T) {
	rng := rand.New(rand.NewSource(424242))
	const trials = 500000
	for trial := 0; trial < trials; trial++ {
		n := 2 + rng.Intn(10)
		a := make([]float64, n)
		w := make([]float64, n)
		for k := range a {
			a[k] = rng.Float64()*10 - 5
			w[k] = 0.01 + rng.Float64()*5
		}
		_, blocks, err := PAVA(a, w)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range blocks {
			for k := b.Lo; k < b.Hi; k++ {
				var pw, pSum float64
				for i := b.Lo; i <= k; i++ {
					pw += w[i]
					pSum += w[i] * a[i]
				}
				if pm := pSum / pw; pm < b.Value-1e-9 {
					t.Fatalf("n=%d 块 [%d,%d) 前缀 [%d,%d] 均值 %.12f < 块值 %.12f", n, b.Lo, b.Hi, b.Lo, k, pm, b.Value)
				}
				var sw, sSum float64
				for i := k; i < b.Hi; i++ {
					sw += w[i]
					sSum += w[i] * a[i]
				}
				if sm := sSum / sw; sm > b.Value+1e-9 {
					t.Fatalf("n=%d 块 [%d,%d) 后缀 [%d,%d) 均值 %.12f > 块值 %.12f", n, b.Lo, b.Hi, k, b.Hi, sm, b.Value)
				}
			}
		}
	}
}

// TestRoundedRepairMatchesDiscreteOptimum 取整后的修复解（整数电平、整数单调约束）
// 在本题规模上与整数可行域上的穷举 DP 最优值一致：随机小数加权下逐一核对。
func TestRoundedRepairMatchesDiscreteOptimum(t *testing.T) {
	rng := rand.New(rand.NewSource(13579))
	for trial := 0; trial < 400; trial++ {
		steps := randomPartition(rng)
		if len(steps) > 7 {
			steps = steps[:7]
			// 重新归一到 256，保持正数
			sum := 0
			for _, s := range steps {
				sum += s
			}
			steps[0] += DomainN - sum
		}
		a := make([]float64, len(steps))
		w := make([]float64, len(steps))
		for k := range a {
			a[k] = float64(rng.Intn(256))
			w[k] = 1 + float64(rng.Intn(5))
		}
		rf, _, err := PAVA(a, w)
		if err != nil {
			t.Fatal(err)
		}
		got := RoundLevels(rf)
		best := integerOptimum(a, w)
		if math.Abs(weightedSSE(a, w, f64(got))-best) > 1e-9 {
			t.Fatalf("steps=%v a=%v: 取整修复 %.10f != 整数最优 %.10f",
				steps, a, weightedSSE(a, w, f64(got)), best)
		}
	}
}

// integerOptimum 在整数单调锥上穷举 DP：dp_k[v]=前 k+1 格非降且第 k 格取 v 的最小代价。
func integerOptimum(a, w []float64) float64 {
	const inf = math.MaxFloat64 / 4
	n := len(a)
	dp := make([]float64, DomainN)
	pref := make([]float64, DomainN)
	for v := 0; v < DomainN; v++ {
		d := float64(v) - a[0]
		dp[v] = w[0] * d * d
	}
	for k := 1; k < n; k++ {
		best := inf
		for v := 0; v < DomainN; v++ {
			if dp[v] < best {
				best = dp[v]
			}
			pref[v] = best
		}
		for v := 0; v < DomainN; v++ {
			d := float64(v) - a[k]
			dp[v] = pref[v] + w[k]*d*d
		}
	}
	best := inf
	for v := 0; v < DomainN; v++ {
		if dp[v] < best {
			best = dp[v]
		}
	}
	return best
}

func isNondecreasing(r []float64) bool {
	for k := 0; k+1 < len(r); k++ {
		if r[k] > r[k+1]+1e-12 {
			return false
		}
	}
	return true
}

func randomMonotone(rng *rand.Rand, a []float64, n int) []float64 {
	r := make([]float64, n)
	base := rng.Float64()*200 - 100
	v := base
	for k := 0; k < n; k++ {
		v += rng.Float64() * 5
		r[k] = v
	}
	return r
}

// TestDualMultipliersNonnegative 对偶乘子 λ_k = 2Σ_{i≤k} w_i(a_i-r_i) 非负。
func TestDualMultipliersNonnegative(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	for trial := 0; trial < 5000; trial++ {
		n := 1 + rng.Intn(24)
		a := make([]float64, n)
		w := make([]float64, n)
		for k := range a {
			a[k] = rng.Float64()*400 - 200
			w[k] = 0.01 + rng.Float64()*5
		}
		r, _, err := PAVA(a, w)
		if err != nil {
			t.Fatal(err)
		}
		for k, lam := range Duals(a, w, r) {
			if lam < -1e-9 {
				t.Fatalf("n=%d λ[%d]=%g 为负，a=%v", n, k, lam, a)
			}
		}
	}
}

// TestKKTComplementarySlackness 在 PAVA 解上逐项核对四条 KKT 条件；互补松弛
// 的形式是 λ_k(r_k-r_{k+1})=0，且约束起作用时 λ_k>0、不起作用时 λ_k=0。
func TestKKTComplementarySlackness(t *testing.T) {
	rng := rand.New(rand.NewSource(2026))
	for trial := 0; trial < 1000; trial++ {
		n := 1 + rng.Intn(30)
		a := make([]float64, n)
		w := make([]float64, n)
		for k := range a {
			a[k] = rng.Float64()*400 - 200
			w[k] = 0.01 + rng.Float64()*5
		}
		r, _, err := PAVA(a, w)
		if err != nil {
			t.Fatal(err)
		}
		rep := KKT(a, w, r)
		if rep.Stationarity > 1e-8 {
			t.Fatalf("n=%d 稳定性残差 %g", n, rep.Stationarity)
		}
		if rep.Complementarity > 1e-8 {
			t.Fatalf("n=%d 互补松弛残差 %g", n, rep.Complementarity)
		}
		if rep.MinLambda < -1e-9 {
			t.Fatalf("n=%d λmin=%g 为负", n, rep.MinLambda)
		}
		lam := Duals(a, w, r)
		for k := range lam {
			equal := r[k] == r[k+1]
			if equal && !(lam[k] > 0) {
				// 合并块内部：λ 必为正（除非整表常数的退化情形，此时全零也合法）。
				nonConst := false
				for j := range a {
					if a[j] != a[0] {
						nonConst = true
						break
					}
				}
				if nonConst {
					t.Fatalf("n=%d 约束 %d 起作用但 λ=%g", n, k, lam[k])
				}
			}
			if !equal && math.Abs(lam[k]) > 1e-9 {
				t.Fatalf("n=%d 约束 %d 不起作用但 λ=%g", n, k, lam[k])
			}
		}
	}
}

// TestRoundLevelsPreservesMonotonicity 四舍五入保持非降，并夹到 [0,255]。
func TestRoundLevelsPreservesMonotonicity(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	for trial := 0; trial < 500; trial++ {
		n := 1 + rng.Intn(50)
		r := make([]float64, n)
		v := -40.0
		for k := range r {
			v += rng.Float64() * 8
			r[k] = v
		}
		got := RoundLevels(r)
		for k := 0; k+1 < n; k++ {
			if got[k] > got[k+1] {
				t.Fatalf("取整后逆序: %v", got)
			}
		}
		for _, v := range got {
			if v < 0 || v > 255 {
				t.Fatalf("越界: %v", got)
			}
		}
	}
}
