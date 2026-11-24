package subband

import (
	"math"
)

// MaxBitsPerCoef 限制单个系数最多使用的比特数，对应 256 级量化。
const MaxBitsPerCoef = 8

// AllocDetail 是单个子带的分配明细。
type AllocDetail struct {
	Name       string
	Level      int
	N          int
	Weight     float64
	Variance   float64  // σ²
	Continuous float64  // 注水求出的连续比特 b*（未整数化）
	Bits       int      // 实际使用的整数比特
	Truncated  int      // 整数化截断阶段被削掉的比特数
	Filled     int      // 预算未用满时补回的比特数
}

// WaterfillResult 是一次反向注水的完整求解记录。
type WaterfillResult struct {
	Budget          int           // 总比特预算 B
	Used            int           // 整数化后实际占用的系数比特
	ContinuousTotal float64       // 连续解的总比特（应恰好等于 B）
	Lambda          float64       // 水位 θ（失真拉格朗日乘子）
	Active          []int         // 活动子带下标（σ²>θ，拿到正比特）
	Iterations      int           // 活动集迭代次数
	KKTResidual     float64       // KKT 条件的最大相对违反量
	BudgetResidual  float64       // 连续解相对预算的残差 |B-Σn b*|/B
	TruncatedBands  int           // 截断涉及的子带数
	TruncatedBits   int           // 截断削掉的总比特
	Details         []AllocDetail // 逐带明细
}

// EqualResult 是平分（等比特）基线的分配记录。
type EqualResult struct {
	Budget int
	Bits   int // 每个子带统一拿到的每系数比特
	Used   int // 实际占用（预算余量在比特流中填充）
	Details []AllocDetail
}

// 反向注水推导（写在实现处，便于对照报告）：
//
//	min  D = Σ_i w_i c σ_i² 4^-b_i   （w_i=n_i/Nimg，高比特均匀量化模型）
//	s.t. Σ_i n_i b_i = B,  b_i ≥ 0
//
// 拉格朗日 L = Σ_i w_i c σ_i² 4^-b_i + λ(Σ_i n_i b_i - B)。
// 令 ∂L/∂b_i=0 得活动带共同失真水位 θ：
//
//	D_i = c σ_i² 4^-b_i = θ  ⇒  b_i* = ½ log₂(σ_i²/θ)
//
// σ_i² ≤ θ 的带不活动，b_i*=0，整体用均值重构（失真即 σ_i²）。
// 给定活动集 A，由预算约束闭式解水位：
//
//	log₂θ = (Σ_A n_i log₂σ_i² − 2B) / Σ_A n_i
//
// 活动集不满足非负条件时剔除 σ² 最小的带并重解，即活动集（反向注水）迭代。

// Waterfill 按各带方差做反向注水，返回连续解、整数化解与迭代/残差记录。
func Waterfill(stats []Stats, budget int) WaterfillResult {
	m := len(stats)
	ns := make([]int, m)
	sigma2 := make([]float64, m)
	nSum := 0
	for i, st := range stats {
		ns[i] = st.N
		sigma2[i] = st.Var
		nSum += st.N
	}

	// 初始活动集：所有方差为正的带；零方差带天然不活动。
	active := make([]int, 0, m)
	for i, v := range sigma2 {
		if v > 0 {
			active = append(active, i)
		}
	}

	iters := 0
	var theta float64
	for {
		iters++
		var nA int
		var logSum float64 // Σ n_i log2 σ_i²
		for _, i := range active {
			nA += ns[i]
			logSum += float64(ns[i]) * math.Log2(sigma2[i])
		}
		// 闭式水位：log2 θ = (Σ n log2 σ² − 2B) / Σ n
		log2Theta := (logSum - 2.0*float64(budget)) / float64(nA)
		theta = math.Pow(2, log2Theta)

		// 检查活动带是否都满足 σ²>θ；最小 σ² 带若不满足则剔除。
		drop := -1
		minVar := math.Inf(1)
		for _, i := range active {
			if sigma2[i] <= theta && sigma2[i] < minVar {
				minVar = sigma2[i]
				drop = i
			}
		}
		if drop < 0 {
			break
		}
		next := active[:0]
		for _, i := range active {
			if i != drop {
				next = append(next, i)
			}
		}
		active = next
		if len(active) == 0 {
			// 极端情况：预算为 0 或全部零方差，水位无意义。
			theta = 0
			break
		}
	}

	// 连续解与残差。
	cont := make([]float64, m)
	contTotal := 0.0
	for i := range cont {
		if sigma2[i] > theta && theta > 0 {
			cont[i] = 0.5 * math.Log2(sigma2[i]/theta)
			if cont[i] < 0 {
				cont[i] = 0
			}
		}
		contTotal += float64(ns[i]) * cont[i]
	}
	budgetResid := math.Abs(float64(budget)-contTotal) / math.Max(float64(budget), 1)

	// KKT 残差：活动带共同失真水位 θ=σ_i²4^-b_i（θ 不含模型常数 c，
	// 因水位是用 σ² 闭式解出的），活动带比值应等于 1；非活动带 σ²/θ 应不超过 1。
	kkt := 0.0
	activeSet := map[int]bool{}
	for _, i := range active {
		activeSet[i] = true
	}
	for i := 0; i < m; i++ {
		var viol float64
		if activeSet[i] {
			level := sigma2[i] * math.Pow(4, -cont[i])
			viol = math.Abs(level/theta - 1)
		} else if theta > 0 {
			viol = math.Max(0, sigma2[i]/theta-1)
		}
		if viol > kkt {
			kkt = viol
		}
	}

	details := make([]AllocDetail, m)
	for i, st := range stats {
		details[i] = AllocDetail{
			Name: st.Name, Level: st.Level, N: st.N, Weight: st.Weight,
			Variance: st.Var, Continuous: cont[i],
		}
	}

	// 整数化：先就近取整（夹到 [0,MaxBits]），再按边际失真削峰/填谷。
	bits := make([]int, m)
	used := 0
	for i := range bits {
		b := int(math.Round(cont[i]))
		if b < 0 {
			b = 0
		}
		if b > MaxBitsPerCoef {
			b = MaxBitsPerCoef
		}
		bits[i] = b
		used += ns[i] * b
	}

	truncBits, truncBands := 0, 0
	// 超预算截断：每次削掉边际失真增量最小的一个比特。
	for used > budget {
		best, bestCost := -1, math.Inf(1)
		for i := 0; i < m; i++ {
			if bits[i] <= 0 {
				continue
			}
			// 削一个比特的加权失真增量 ∝ w_i σ_i² 4^-b_i
			cost := stats[i].Weight * sigma2[i] * math.Pow(4, -float64(bits[i]))
			if cost < bestCost {
				bestCost, best = cost, i
			}
		}
		if best < 0 {
			break
		}
		bits[best]--
		used -= ns[best]
		details[best].Truncated++
		truncBits++
	}
	if truncBits > 0 {
		for i := range details {
			if details[i].Truncated > 0 {
				truncBands++
			}
		}
	}
	// 未用满则按边际失真减量最大的方向回填，直到预算耗尽或全部触顶。
	for used < budget {
		best, bestGain := -1, math.Inf(1)
		for i := 0; i < m; i++ {
			if bits[i] >= MaxBitsPerCoef {
				continue
			}
			gain := stats[i].Weight * sigma2[i] * math.Pow(4, -float64(bits[i]))
			if gain < bestGain {
				bestGain, best = gain, i
			}
		}
		if best < 0 {
			break
		}
		bits[best]++
		used += ns[best]
		details[best].Filled++
	}
	// 硬保护：任何路径结束后仍超预算（如连续解触顶夹断），按边际成本继续截断。
	for used > budget {
		best, bestCost := -1, math.Inf(1)
		for i := 0; i < m; i++ {
			if bits[i] <= 0 {
				continue
			}
			cost := stats[i].Weight * sigma2[i] * math.Pow(4, -float64(bits[i]))
			if cost < bestCost {
				bestCost, best = cost, i
			}
		}
		if best < 0 {
			break
		}
		bits[best]--
		used -= ns[best]
		details[best].Truncated++
		truncBits++
	}
	if truncBits > 0 {
		truncBands = 0
		for i := range details {
			if details[i].Truncated > 0 {
				truncBands++
			}
		}
	}
	for i := range details {
		details[i].Bits = bits[i]
	}

	return WaterfillResult{
		Budget:          budget,
		Used:            used,
		ContinuousTotal: contTotal,
		Lambda:          theta,
		Active:          append([]int(nil), active...),
		Iterations:      iters,
		KKTResidual:     kkt,
		BudgetResidual:  budgetResid,
		TruncatedBands:  truncBands,
		TruncatedBits:   truncBits,
		Details:         details,
	}
}

// EqualAllocation 是平分基线：每个子带拿相同的每系数比特 floor(B/N)，
// 预算余量不参与分配（在比特流中填充），保证用量不超预算。
func EqualAllocation(stats []Stats, budget int) EqualResult {
	nSum := 0
	for _, st := range stats {
		nSum += st.N
	}
	b := budget / nSum
	if b > MaxBitsPerCoef {
		b = MaxBitsPerCoef
	}
	details := make([]AllocDetail, len(stats))
	used := 0
	for i, st := range stats {
		details[i] = AllocDetail{
			Name: st.Name, Level: st.Level, N: st.N, Weight: st.Weight,
			Variance: st.Var, Continuous: float64(b), Bits: b,
		}
		used += st.N * b
	}
	return EqualResult{Budget: budget, Bits: b, Used: used, Details: details}
}
