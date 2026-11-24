package subband

import (
	"math"
)

// AllocDetail 是单个子带的注水分配明细。
type AllocDetail struct {
	Label      string
	N          int
	Variance   float64
	Continuous float64 // 连续注水求出的 b*（未整数化）
	Bits       int     // 最终整数比特（DP 落地）
	ZeroBit    bool    // 是否被整带截断（0 比特）
}

// WaterfillResult 是方差反向注水的连续解与活动集记录。
type WaterfillResult struct {
	Budget         int
	Theta          float64 // 失真水位 θ=σ_i²·4^-b_i
	Iterations     int
	Active         []int // 活动带下标
	Continuous     []float64
	ContinuousSum  float64 // Σ n_i b_i*，应等于预算
	BudgetResidual float64
	Details        []AllocDetail
}

// Waterfill 按各带方差做连续反向注水。
//
//	min  Σ_i c σ_i² 4^-b_i   s.t. Σ_i n_i b_i = B, b_i≥0
//
// 拉格朗日求导后活动带满足共同失真水位 θ：
//
//	σ_i² 4^-b_i* = θ  ⇒  b_i* = ½ log₂(σ_i²/θ)
//
// 给定活动集 A，由预算约束闭式解：
//
//	log₂ θ = (Σ_A n_i log₂ σ_i² − 2B) / Σ_A n_i
//
// σ_i²≤θ 的带不活动（b*=0，整带用均值重构）。活动集不满足非负条件时
// 剔除 σ² 最小的候选带并重解，直到所有活动带 σ²>θ。
func Waterfill(stats []Stats, budget int) WaterfillResult {
	m := len(stats)
	ns := make([]int, m)
	s2 := make([]float64, m)
	for i, st := range stats {
		ns[i] = st.N
		s2[i] = math.Max(st.Var, 1e-12) // 零方差带用地板值，随后会被剔出活动集
	}
	active := make([]int, 0, m)
	for i, st := range stats {
		if st.Var > 1e-12 {
			active = append(active, i)
		}
	}

	iters := 0
	theta := 0.0
	for len(active) > 0 {
		iters++
		var nA int
		var logSum float64
		for _, i := range active {
			nA += ns[i]
			logSum += float64(ns[i]) * math.Log2(s2[i])
		}
		log2Theta := (logSum - 2*float64(budget)) / float64(nA)
		theta = math.Pow(2, log2Theta)

		drop := -1
		dropVar := math.Inf(1)
		for _, i := range active {
			if s2[i] <= theta && s2[i] < dropVar {
				dropVar = s2[i]
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
	}
	if len(active) == 0 {
		theta = 0
	}

	cont := make([]float64, m)
	sum := 0.0
	for i := range cont {
		if theta > 0 && s2[i] > theta {
			cont[i] = 0.5 * math.Log2(s2[i]/theta)
			if cont[i] < 0 {
				cont[i] = 0
			}
		}
		sum += float64(ns[i]) * cont[i]
	}
	resid := 0.0
	if budget > 0 {
		resid = math.Abs(float64(budget)-sum) / float64(budget)
	}

	details := make([]AllocDetail, m)
	for i, st := range stats {
		details[i] = AllocDetail{
			Label: st.Label, N: st.N, Variance: st.Var, Continuous: cont[i],
		}
	}
	return WaterfillResult{
		Budget: budget, Theta: theta, Iterations: iters,
		Active: append([]int(nil), active...), Continuous: cont,
		ContinuousSum: sum, BudgetResidual: resid, Details: details,
	}
}

// DistortionTables 实测每个带在 b=0..MaxBitsPerCoef 下的“加权失真”
// weight_i*MSE_i。DP 直接在真实量化器失真上优化，而不是只拟合理论曲线。
// 同时返回每行的 QuantBand 结果缓存会占用内存，这里只回传失真；
// 分配定案后调用方再 Quantize 一次即可。
func DistortionTables(bands []Band, stats []Stats, weights []float64) [][]float64 {
	d := make([][]float64, len(bands))
	for i, b := range bands {
		d[i] = make([]float64, MaxBitsPerCoef+1)
		for bits := 0; bits <= MaxBitsPerCoef; bits++ {
			q := Quantize(b, stats[i], bits)
			d[i][bits] = weights[i] * q.MSE
		}
	}
	return d
}

// ModelDistortionTables 是反向注水的方差解析模型：
//
//	D_i(b)=weight_i·σ_i²·4^-b （b=0 即均值重构失真 σ²）。
//
// 在这张表上做整数 DP，得到的就是方差反向注水的整数落地解。
func ModelDistortionTables(stats []Stats, weights []float64) [][]float64 {
	d := make([][]float64, len(stats))
	for i, st := range stats {
		d[i] = make([]float64, MaxBitsPerCoef+1)
		base := weights[i] * math.Max(st.Var, 1e-12)
		for bits := 0; bits <= MaxBitsPerCoef; bits++ {
			d[i][bits] = base * math.Pow(4, -float64(bits))
		}
	}
	return d
}

// DPResult 是整数比特分配结果。
type DPResult struct {
	Bits []int
	Used int  // Σ n_i b_i
	Exact bool // 是否严格用满请求预算
}

// AllocateInteger 在总比特约束下用分组背包 DP 求全局最优整数分配：
//
//	min Σ_i D_i(b_i)  s.t. Σ_i n_i b_i = B（exact=true）或 ≤B（exact=false），
//	0≤b_i≤8。
//
// 复杂度 O(m·B·9)。exact=true 时平分向量（B=kN）是可行解，
// 因此 DP 解的加权误差必然不劣于平分。
func AllocateInteger(dist [][]float64, ns []int, budget int, exact bool) DPResult {
	m := len(dist)
	inf := math.Inf(1)
	dp := make([]float64, budget+1)
	for i := 1; i <= budget; i++ {
		dp[i] = inf
	}
	// choice[i][c] 记录处理完第 i 个带后、总成本恰为 c 时该带选取的比特数。
	choice := make([][]int8, m)
	for i := range choice {
		choice[i] = make([]int8, budget+1)
		for c := range choice[i] {
			choice[i][c] = -1
		}
	}

	for i := 0; i < m; i++ {
		ndp := make([]float64, budget+1)
		for c := range ndp {
			ndp[c] = inf
		}
		for c := 0; c <= budget; c++ {
			if math.IsInf(dp[c], 1) {
				continue
			}
			for b := 0; b <= MaxBitsPerCoef; b++ {
				c2 := c + ns[i]*b
				if c2 > budget {
					break
				}
				v := dp[c] + dist[i][b]
				if v < ndp[c2] {
					ndp[c2] = v
					choice[i][c2] = int8(b)
				}
			}
		}
		dp = ndp
	}

	end := budget
	if !exact {
		best := inf
		for c := 0; c <= budget; c++ {
			if dp[c] < best {
				best = dp[c]
				end = c
			}
		}
	} else if math.IsInf(dp[budget], 1) {
		// 精确预算不可行（例如预算小于最小颗粒），退化为不超预算。
		return AllocateInteger(dist, ns, budget, false)
	}

	bits := make([]int, m)
	c := end
	for i := m - 1; i >= 0; i-- {
		b := int(choice[i][c])
		if b < 0 {
			b = 0
		}
		bits[i] = b
		c -= ns[i] * b
	}
	return DPResult{Bits: bits, Used: end, Exact: exact && end == budget}
}

// EqualBits 构造平分向量：每带每系数相同比特 k。
func EqualBits(m, k int) []int {
	out := make([]int, m)
	for i := range out {
		out[i] = k
	}
	return out
}

// TotalBits 计算 Σ n_i b_i。
func TotalBits(ns, bits []int) int {
	t := 0
	for i := range ns {
		t += ns[i] * bits[i]
	}
	return t
}

// TruncatedBands 返回比特数为 0 的带下标（被整带截断）。
func TruncatedBands(bits []int) []int {
	var out []int
	for i, b := range bits {
		if b == 0 {
			out = append(out, i)
		}
	}
	return out
}
