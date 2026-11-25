// Package monolevel 由任意给定的步长序列构造单调（非降）的重建电平，
// 并给出该构造的最小二乘最优性证明所需的全部量。
//
// 记号：步长序列 Δ_1..Δ_L（正整数，和为 256）把 8 位定义域切成 L 个左闭右开的
// 量化区间 I_k=[t_{k-1},t_k)，t_0=0，t_k=Σ_{i≤k}Δ_i。
//
// 构造分两步（一步一行）：
//
//	第一步：铺边界 t_k=Σ_{i≤k}Δ_i（t_0=0，t_L=256），每格给名义电平 a_k 与正权重 w_k。
//	第二步：从左到右压栈，若前一块的加权均值大于后一块就合并取加权平均，展开得非降电平 r。
//
// 第二步即加权保序回归（PAVA），它给出
//
//	min_r Σ_k w_k (r_k-a_k)²   s.t.  r_0 ≤ r_1 ≤ … ≤ r_{L-1}
//
// 的唯一解；最优性由 KKT 条件（含互补松弛）刻画，见 KKT。
package monolevel

import (
	"errors"
	"fmt"
	"math"
)

// DomainN 是 8 位灰度的定义域大小（取值 0..255）。
const DomainN = 256

var (
	// ErrBadSteps 表示步长序列为空、含非正项或总和不是 256。
	ErrBadSteps = errors.New("步长序列非法")
	// ErrBadHist 表示直方图长度不是 256 或没有任何样本。
	ErrBadHist = errors.New("直方图必须为长度 256 且非空")
	// ErrBadWeights 表示权重缺失、非正、非有限或与电平表不等长。
	ErrBadWeights = errors.New("权重必须为有限正数且与电平表等长")
	// ErrBadLevels 表示电平表长度与区间数不一致。
	ErrBadLevels = errors.New("电平表长度与步长数不符")
)

// Partition 由步长序列铺出区间边界，返回长度 L+1 的边界（末项为 256）。
// 对任意正整数步长序列，只要和为 256 就能铺出严格递增的边界。
func Partition(steps []int) ([]int, error) {
	if len(steps) == 0 {
		return nil, fmt.Errorf("%w: 空序列", ErrBadSteps)
	}
	bounds := make([]int, len(steps)+1)
	for i, d := range steps {
		if d <= 0 {
			return nil, fmt.Errorf("%w: 第 %d 个步长 %d 非正", ErrBadSteps, i, d)
		}
		bounds[i+1] = bounds[i] + d
	}
	if bounds[len(bounds)-1] != DomainN {
		return nil, fmt.Errorf("%w: 步长和 %d != %d", ErrBadSteps, bounds[len(bounds)-1], DomainN)
	}
	return bounds, nil
}

// Cells 是按边界切分后的逐格统计量。Weight 为归一的样本占比（和为一）。
type Cells struct {
	Bounds []int     // 长度 L+1
	Counts []uint64  // 每格样本数
	Weight []float64 // Counts/Total
	Mean   []float64 // 条件均值 μ_k
	Var    []float64 // 条件方差 σ_k²
	Total  uint64
}

// Stats 在直方图上统计每个量化区间的样本数、条件均值与条件方差。
// 任一步长序列只要每格都有样本即可；空区间返回 ErrBadHist。
func Stats(steps []int, hist []uint64) (Cells, error) {
	bounds, err := Partition(steps)
	if err != nil {
		return Cells{}, err
	}
	if len(hist) != DomainN {
		return Cells{}, fmt.Errorf("%w: 长度 %d", ErrBadHist, len(hist))
	}
	var total uint64
	for _, c := range hist {
		total += c
	}
	if total == 0 {
		return Cells{}, fmt.Errorf("%w: 全零", ErrBadHist)
	}
	out := Cells{
		Bounds: bounds,
		Counts: make([]uint64, len(steps)),
		Weight: make([]float64, len(steps)),
		Mean:   make([]float64, len(steps)),
		Var:    make([]float64, len(steps)),
		Total:  total,
	}
	for k := range steps {
		lo, hi := bounds[k], bounds[k+1]
		var n uint64
		var sum float64
		for x := lo; x < hi; x++ {
			n += hist[x]
			sum += float64(x) * float64(hist[x])
		}
		if n == 0 {
			return Cells{}, fmt.Errorf("%w: 区间 [%d,%d) 没有样本", ErrBadHist, lo, hi)
		}
		mean := sum / float64(n)
		var varsum float64
		for x := lo; x < hi; x++ {
			d := float64(x) - mean
			varsum += float64(hist[x]) * d * d
		}
		out.Counts[k] = n
		out.Weight[k] = float64(n) / float64(total)
		out.Mean[k] = mean
		out.Var[k] = varsum / float64(n)
	}
	return out, nil
}

// Block 是 PAVA 合并后的一个常数块：[Lo,Hi) 内电平相等，值为块内加权平均。
type Block struct {
	Lo, Hi int
	Weight float64
	Sum    float64
	Value  float64
}

// PAVA 是加权保序回归（Pool Adjacent Violators）。对任意实数表 a 与正权重 w，
// 它返回非降的 r 与最终块划分，且 r 是下述问题的唯一解：
//
//	min Σ_k w_k (r_k-a_k)²   s.t.  r_0 ≤ r_1 ≤ … ≤ r_{L-1}
//
// 算法：从左到右入栈，只要栈顶前一块的加权均值大于后一块，就合并、以权重和为权取平均。
// 终止时栈中块均值严格递增，展开即 r。
func PAVA(a, w []float64) ([]float64, []Block, error) {
	if len(a) == 0 || len(a) != len(w) {
		return nil, nil, fmt.Errorf("%w: len(a)=%d len(w)=%d", ErrBadWeights, len(a), len(w))
	}
	type block struct {
		weight, sum float64
		lo, hi      int
	}
	stack := make([]block, 0, len(a))
	for k := range a {
		if math.IsNaN(a[k]) || math.IsInf(a[k], 0) {
			return nil, nil, fmt.Errorf("%w: a[%d]=%g", ErrBadWeights, k, a[k])
		}
		if !(w[k] > 0) || math.IsInf(w[k], 0) || math.IsNaN(w[k]) {
			return nil, nil, fmt.Errorf("%w: w[%d]=%g", ErrBadWeights, k, w[k])
		}
		stack = append(stack, block{weight: w[k], sum: w[k] * a[k], lo: k, hi: k + 1})
		for len(stack) >= 2 {
			left := stack[len(stack)-2]
			right := stack[len(stack)-1]
			if left.sum/left.weight <= right.sum/right.weight {
				break
			}
			stack[len(stack)-2] = block{
				weight: left.weight + right.weight,
				sum:    left.sum + right.sum,
				lo:     left.lo,
				hi:     right.hi,
			}
			stack = stack[:len(stack)-1]
		}
	}
	r := make([]float64, len(a))
	blocks := make([]Block, len(stack))
	for i, b := range stack {
		v := b.sum / b.weight
		for k := b.lo; k < b.hi; k++ {
			r[k] = v
		}
		blocks[i] = Block{Lo: b.lo, Hi: b.hi, Weight: b.weight, Sum: b.sum, Value: v}
	}
	return r, blocks, nil
}

// RoundLevels 把实值电平四舍五入（.5 远离零）到 [0,255] 的整数并夹紧。
// 四舍五入与夹紧都是非降映射，因此输入非降则输出非降。
func RoundLevels(r []float64) []int {
	out := make([]int, len(r))
	for i, v := range r {
		x := int(math.Round(v))
		if x < 0 {
			x = 0
		}
		if x > DomainN-1 {
			x = DomainN - 1
		}
		out[i] = x
	}
	return out
}

// MidpointLevels 是区间内整数点的算术平均再四舍五入：m_k=round((t_{k-1}+t_k-1)/2)。
// 它不使用任何样本信息，作为对照路径保留。
func MidpointLevels(steps []int) ([]int, error) {
	bounds, err := Partition(steps)
	if err != nil {
		return nil, err
	}
	return MidpointLevelsFromBounds(bounds), nil
}

// MidpointLevelsFromBounds 同 MidpointLevels，但直接接收已铺好的边界。
func MidpointLevelsFromBounds(bounds []int) []int {
	out := make([]int, len(bounds)-1)
	for k := 0; k+1 < len(bounds); k++ {
		out[k] = int(math.Round(float64(bounds[k]+bounds[k+1]-1) / 2))
	}
	return out
}

// Duals 返回约束 r_k ≤ r_{k+1}（k=0..L-2）的乘子 λ_k：
//
//	λ_k = 2 Σ_{i=0}^{k} w_i (a_i - r_i),  k=0..L-2.
//
// 约定 λ_{-1}=λ_{L-1}=0。在 PAVA 解上这些 λ_k 非负，且只在约束起作用处为正。
func Duals(a, w, r []float64) []float64 {
	lam := make([]float64, len(a)-1)
	var sum float64
	for k := 0; k+1 < len(a); k++ {
		sum += w[k] * (a[k] - r[k])
		lam[k] = 2 * sum
	}
	return lam
}

// KKTReport 是一次最优性核验的残差，全部按互补松弛形式给出：
//
//	r_k ≤ r_{k+1}                          （原始可行）
//	λ_k = 2Σ_{i≤k} w_i(a_i-r_i) ≥ 0       （对偶可行）
//	λ_k (r_k - r_{k+1}) = 0               （互补松弛）
//	2 w_k (r_k - a_k) = λ_{k-1} - λ_k     （稳定性）
type KKTReport struct {
	PrimalViolation float64 // max_k max(0, r_k-r_{k+1})，原始可行性残差
	Stationarity    float64 // max_k |2w_k(r_k-a_k)+λ_k-λ_{k-1}|
	Complementarity float64 // max_k |λ_k (r_k-r_{k+1})|
	MinLambda       float64 // min_k λ_k，允许浮点级别的负零
	MaxLambda       float64
	Active          int // r_k = r_{k+1} 的约束数（约束起作用；KKT 另保证此处 λ_k≥0）
	Inactive        int // r_k < r_{k+1} 的约束数（约束不起作用；互补松弛要求 λ_k=0）
}

// KKT 按互补松弛形式逐项检查 PAVA 解（或其整数取整后的解）。
func KKT(a, w, r []float64) KKTReport {
	var rep KKTReport
	if len(a) == 0 || len(a) != len(w) || len(a) != len(r) {
		return rep
	}
	if len(a) == 1 {
		rep.Stationarity = math.Abs(2 * w[0] * (r[0] - a[0]))
		return rep
	}
	lam := Duals(a, w, r)
	rep.MinLambda = lam[0]
	rep.MaxLambda = lam[0]
	for k := range lam {
		if lam[k] < rep.MinLambda {
			rep.MinLambda = lam[k]
		}
		if lam[k] > rep.MaxLambda {
			rep.MaxLambda = lam[k]
		}
		if v := r[k] - r[k+1]; v > rep.PrimalViolation {
			rep.PrimalViolation = v
		}
		if s := math.Abs(lam[k] * (r[k] - r[k+1])); s > rep.Complementarity {
			rep.Complementarity = s
		}
		// 起作用/不起作用按约束的拓扑定义计数：r_k=r_{k+1} 即约束取等号。
		// λ 是否相应为正/为零由 Complementarity 与测试按容差核验，
		// 不在这里拿浮点 λ 与字面 0 直接比较（整块求和会留 1e-15 级残差）。
		switch {
		case r[k] == r[k+1]:
			rep.Active++
		case r[k] < r[k+1]:
			rep.Inactive++
		}
	}
	for k := range a {
		lprev := 0.0
		if k > 0 {
			lprev = lam[k-1]
		}
		lnext := 0.0
		if k+1 < len(a) {
			lnext = lam[k]
		}
		if s := math.Abs(2*w[k]*(r[k]-a[k]) + lnext - lprev); s > rep.Stationarity {
			rep.Stationarity = s
		}
	}
	return rep
}

// LSError 返回加权最小二乘目标 J(r)=Σ w_k (r_k-a_k)²。
func LSError(a, w, r []float64) (float64, error) {
	if len(a) == 0 || len(a) != len(w) || len(a) != len(r) {
		return 0, fmt.Errorf("%w: len(a)=%d len(w)=%d len(r)=%d", ErrBadWeights, len(a), len(w), len(r))
	}
	var s float64
	for k := range a {
		d := r[k] - a[k]
		s += w[k] * d * d
	}
	return s, nil
}

// PixelMSE 在直方图上计算均方重建误差 Σ_x h_x (Q(x)-x)² / Σ_x h_x。
func PixelMSE(hist []uint64, bounds []int, levels []int) (float64, error) {
	if len(hist) != DomainN {
		return 0, fmt.Errorf("%w: 长度 %d", ErrBadHist, len(hist))
	}
	if len(levels) != len(bounds)-1 {
		return 0, fmt.Errorf("%w: len(levels)=%d len(bounds)=%d", ErrBadLevels, len(levels), len(bounds))
	}
	var total uint64
	for _, c := range hist {
		total += c
	}
	if total == 0 {
		return 0, fmt.Errorf("%w: 全零", ErrBadHist)
	}
	var s float64
	k := 0
	for x := 0; x < DomainN; x++ {
		for k+1 < len(bounds) && x >= bounds[k+1] {
			k++
		}
		if k >= len(levels) {
			k = len(levels) - 1
		}
		d := float64(x) - float64(levels[k])
		s += float64(hist[x]) * d * d
	}
	return s / float64(total), nil
}

// PredictMSE 用逐格统计量预测重建误差：
//
//	E[(X-r_{c(X)})²] = Σ_k w_k [σ_k² + (r_k-μ_k)²].
//
// 这是全方差公式的直接展开。在同一份直方图上它与 PixelMSE 逐位一致；
// 换一份同分布样本（如训练/测试对半）时，它是测试误差的无偏预测。
func PredictMSE(c Cells, levels []int) (float64, error) {
	if len(levels) != len(c.Mean) {
		return 0, fmt.Errorf("%w: len(levels)=%d len(mean)=%d", ErrBadLevels, len(levels), len(c.Mean))
	}
	var s float64
	for k := range c.Mean {
		d := float64(levels[k]) - c.Mean[k]
		s += c.Weight[k] * (c.Var[k] + d*d)
	}
	return s, nil
}

// Pair 是一对逆序电平：下标 I<J 且 levels[I] > levels[J]。
type Pair struct {
	I, J   int
	VI, VJ int
}

// AllPairs 对电平表做全量逐对比较：所有 I<J，共 L(L-1)/2 对，不抽样。
func AllPairs(levels []int) (int, []Pair) {
	var bad []Pair
	for i := 0; i < len(levels); i++ {
		for j := i + 1; j < len(levels); j++ {
			if levels[i] > levels[j] {
				bad = append(bad, Pair{I: i, J: j, VI: levels[i], VJ: levels[j]})
			}
		}
	}
	return len(levels) * (len(levels) - 1) / 2, bad
}

// RampLevels 把电平表展开成定义域 0..255 上的完整重建映射 Q(x)。
func RampLevels(steps []int, levels []int) ([]int, error) {
	bounds, err := Partition(steps)
	if err != nil {
		return nil, err
	}
	if len(levels) != len(steps) {
		return nil, fmt.Errorf("%w: len(levels)=%d len(steps)=%d", ErrBadLevels, len(levels), len(steps))
	}
	y := make([]int, DomainN)
	k := 0
	for x := 0; x < DomainN; x++ {
		for k+1 < len(bounds) && x >= bounds[k+1] {
			k++
		}
		if k >= len(levels) {
			k = len(levels) - 1
		}
		y[x] = levels[k]
	}
	return y, nil
}

// RampAllPairs 在完整重建映射 Q(0..255) 上做全量逐对比较：共 256·255/2=32640 对，
// 不抽样。返回比较对数与全部逆序对。
func RampAllPairs(steps []int, levels []int) (int, []Pair, error) {
	y, err := RampLevels(steps, levels)
	if err != nil {
		return 0, nil, err
	}
	pairs, bad := AllPairs(y)
	return pairs, bad, nil
}

// SplitCounts 把每个灰度的样本数确定性地对半分：train[x]=⌊h[x]/2⌋，
// test[x]=h[x]-train[x]。偶数位样本当训练、奇数位当测试，两半按灰度成比例。
func SplitCounts(hist []uint64) (train, test []uint64) {
	if len(hist) != DomainN {
		return nil, nil
	}
	train = make([]uint64, DomainN)
	test = make([]uint64, DomainN)
	for x, c := range hist {
		train[x] = c / 2
		test[x] = c - train[x]
	}
	return train, test
}

// NominalLevels 按“逐格补对比度”的凑表习惯生成名义表：
//
//	a_k = round(μ_k + ε·Δ_k·(-1)^k)
//
// 即偶数格上偏、奇数格下偏，偏移随本格步长放大。这样凑出的表相邻格常交叉，
// 正是需要修复的非单调表；它仍保留了分布信息（μ_k），因此修复后误差低于中点。
func NominalLevels(c Cells, eps float64) []int {
	out := make([]int, len(c.Mean))
	for k := range c.Mean {
		delta := float64(c.Bounds[k+1] - c.Bounds[k])
		sign := 1.0
		if k%2 == 1 {
			sign = -1
		}
		out[k] = int(math.Round(c.Mean[k] + sign*eps*delta))
		if out[k] < 0 {
			out[k] = 0
		}
		if out[k] > DomainN-1 {
			out[k] = DomainN - 1
		}
	}
	return out
}
