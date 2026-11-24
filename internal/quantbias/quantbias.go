// Package quantbias 检验均匀标量量化的误差分布是否真的均匀，并求解把分布
// “掰回”均匀所需的重建电平整体偏移。
//
// 模型（三档位深 b∈{2,3,4}，电平数 L=2^b，步长 Δ=256/L）：
//
//	q(x) = clamp(floor(x/Δ), 0, L-1)
//	原始（参照）重建电平 r0 = qΔ          —— 等价于整数截断，误差 e0=x-r0∈[0,Δ)
//	偏移 δ 后的重建电平   rδ = qΔ + δ
//
// 令整数相位 k = x-qΔ∈{0,…,Δ-1}，归一化相位 u=k/Δ∈[0,1)，偏置项
//
//	β = E[u] = (1/N) Σ k_i/Δ,   b0 = E[e0] = Δβ
//
// 任意偏移 c=δ/Δ 下，归一化误差为 u-c，故归一化均值（残余偏置）为 β-c。
// 令其为零得到闭式偏移 c*=β（灰度单位 δ*=Δβ，即桶内质心）。
//
// 为什么用 2-Wasserstein 散度求解偏移：KL/JS 这类 f-散度在“分布与其支撑上的
// 均匀分布一起平移”时是平移不变的，无法据此定位偏移；而 W₂² 对偏移是严格凸的，
// 其一阶条件恰好给出 c*=β（见 REPORT.md 第 4 节推导）。解出偏移后，再用固定
// 对称区间 [-Δ/2,Δ/2] 上的 JS/KL/χ² 三种散度验证分布是否更接近均匀。
package quantbias

import (
	"math"
	"sort"
)

// BiasTolerance 是“偏置是否解对”的归一化阈值：残余均值 |E[e]|/Δ ≤ 0.05。
// 取 0.05 的来源见 artifacts/quant_bias/REPORT.md 第 6 节（Fisher α=0.05
// 的惯例显著性水平，并借用为 1/20 步长的工程容差）。
const BiasTolerance = 0.05

// DefaultBits 是题目要求的三档位深：2/3/4 bit（4/8/16 个重建电平）。
var DefaultBits = []int{2, 3, 4}

// ClassNames 是四类测试图的固定名字（顺序即报告顺序）：
// 平滑渐变 / 类照片 / 结构化纹理 / 高频噪声，覆盖不同频谱与影调形态。
var ClassNames = []string{"gradient", "photo", "texture", "noise"}

// Cell 是“一类图 × 一档位深”的全部测量结果。
// 所有概率数组长度均为 B=Δ，对应灰度区间 [-Δ/2,+Δ/2] 内等宽（1 灰度/格）
// 的固定参考网格，端点越界样本夹到边格。
type Cell struct {
	Class  string  `json:"class"`
	Bits   int     `json:"bits"`
	Levels int     `json:"levels"`
	Step   float64 `json:"step"`
	Pixels int     `json:"pixels"`

	// —— 偏置项 ——
	PhaseMean           float64 `json:"phase_mean"`            // β = E[k/Δ]
	DiscreteUniformMean float64 `json:"discrete_uniform_mean"` // 离散均匀相位均值 (Δ-1)/(2Δ)
	BiasGrayOriginal    float64 `json:"bias_gray_original"`    // Δβ，原量化器平均误差
	TiltKappa           float64 `json:"tilt_kappa"`            // 相对离散均匀基线的一阶倾斜 κ=12(β-β0)
	PhaseSkewness       float64 `json:"phase_skewness"`        // 相位分布偏度 g1
	PositiveFraction    float64 `json:"positive_fraction"`     // 原误差 e0≥0 的比例（恒为 1）
	OutsideHalfFrac     float64 `json:"outside_half_frac"`     // 相位 >1/2 的比例（落到对称区间外）

	// —— 解出的重建电平偏移（灰度单位 / 归一化） ——
	OffsetClosedForm float64 `json:"offset_closed_form"` // Δβ（闭式质心）
	OffsetW2         float64 `json:"offset_w2"`          // W₂² 数值最小点
	OffsetInt        int     `json:"offset_int"`         // 可落地的整数偏移 round(Δ c*)
	SolverAgreement  float64 `json:"solver_agreement"`   // |c_W2-β|

	// —— 施加偏移后的残余归一化偏置 ——
	ResidualBiasContinuous float64 `json:"residual_bias_continuous"` // c_W2 连续偏移
	ResidualBiasInteger    float64 `json:"residual_bias_integer"`    // 整数偏移（实际量化器）

	// —— 散度（bit 为底；JS 为 JS 散度本身而非 JS 距离） ——
	JSOriginal       float64 `json:"js_original"`
	JSContinuous     float64 `json:"js_continuous"`
	JSInteger        float64 `json:"js_integer"`
	KLOriginal       float64 `json:"kl_original"`
	KLContinuous     float64 `json:"kl_continuous"`
	KLInteger        float64 `json:"kl_integer"`
	Chi2Original     float64 `json:"chi2_original"` // Σ(p-q)²/q
	Chi2Continuous   float64 `json:"chi2_continuous"`
	Chi2Integer      float64 `json:"chi2_integer"`
	Chi2StatOriginal float64 `json:"chi2_stat_original"` // N·χ²，自由度 Δ-1
	Chi2StatInteger  float64 `json:"chi2_stat_integer"`

	// W₂² 到 U[-1/2,1/2] 的平方距离（归一化单位）
	W2SqOriginal float64 `json:"w2sq_original"`
	W2SqOptimal  float64 `json:"w2sq_optimal"`
	W2SqInteger  float64 `json:"w2sq_integer"`

	// —— 判定 ——
	BiasPass       bool `json:"bias_pass"`       // |残余整数偏置| ≤ 0.05
	DivergenceDown bool `json:"divergence_down"` // JS/KL/χ² 整数档均严格下降
	CellPass       bool `json:"cell_pass"`       // 两者同时成立

	// —— 画图用直方图 ——
	BinEdgesGray []float64 `json:"bin_edges_gray"` // B+1，[-Δ/2 … Δ/2]
	HistOriginal []float64 `json:"hist_original"`  // 概率，B 格
	HistInteger  []float64 `json:"hist_integer"`
	Uniform      []float64 `json:"uniform"`

	// 宽窗直方图（仅画图，不参与散度判定）：2Δ 格覆盖 [-Δ,Δ]
	WideEdgesGray []float64 `json:"wide_edges_gray"`
	WideOriginal  []float64 `json:"wide_original"`
	WideInteger   []float64 `json:"wide_integer"`
}

// ClassVerdict 是“一类图测一次”的汇总：跨三档位深取最差偏差。
type ClassVerdict struct {
	Class             string  `json:"class"`
	MaxResidualBias   float64 `json:"max_residual_bias"`
	WorstDepthBits    int     `json:"worst_depth_bits"`
	BiasPass          bool    `json:"bias_pass"`
	AllDivergenceDown bool    `json:"all_divergence_down"`
	Pass              bool    `json:"pass"`
}

// Report 是四类图 × 三档位深的完整结果，可直接序列化为 results.json。
type Report struct {
	Threshold     float64        `json:"threshold"`
	BitDepths     []int          `json:"bit_depths"`
	Classes       []string       `json:"classes"`
	Width         int            `json:"width"`
	Height        int            `json:"height"`
	Seed          int64          `json:"seed"`
	Cells         []Cell         `json:"cells"`
	ClassVerdicts []ClassVerdict `json:"class_verdicts"`
	AllPass       bool           `json:"all_pass"`
}

// OriginalOffsetGray 是“原量化器”的重建电平偏移：δ=0，即桶下沿 qΔ（等价于
// 整数右移/截断）。它作为永久参照臂保留，所有报告中的橙色序列都来自该偏移。
const OriginalOffsetGray = 0.0

// Bin 返回像素 v 在 b 位均匀量化下的档号 q 与桶内整数相位 k=v-qΔ。
func Bin(v uint8, bits int) (q, k int) {
	levels := 1 << bits
	step := 256 / levels
	q = int(v) / step
	if q > levels-1 {
		q = levels - 1
	}
	k = int(v) - q*step
	return q, k
}

// Level 返回档 q 在给定步长 Δ 与重建偏移 δ（灰度单位）下的重建电平 qΔ+δ。
// δ=0 即原始桶下沿量化器；δ=Δβ 即散度最优/偏置校正量化器。
func Level(q int, delta, offsetGray float64) float64 {
	return float64(q)*delta + offsetGray
}

// Analyze 对一张灰度图在给定位深下完成全部测量。
func Analyze(pix []uint8, w, h, bits int, class string) Cell {
	n := w * h
	levels := 1 << bits
	step := 256.0 / float64(levels)

	// 统计整数相位 k=x-qΔ。
	maxK := int(step)
	counts := make([]int, maxK)
	var sumK int
	var sumK2, sumK3 float64
	outside := 0
	phases := make([]float64, 0, n) // 已排序的归一化相位（计数展开）
	for _, v := range pix {
		_, kk := Bin(v, bits)
		k := kk
		counts[k]++
		sumK += k
		dv := float64(k) / step
		sumK2 += dv * dv
		sumK3 += dv * dv * dv
		phases = append(phases, dv)
		if dv > 0.5 { // 落在标准对称区间 [-1/2,1/2] 之外（右侧）
			outside++
		}
	}
	sort.Float64s(phases)

	N := float64(n)
	beta := float64(sumK) / (N * step)
	beta0 := (step - 1) / (2 * step) // Δ 个等概率整数相位 k=0..Δ-1 的均值
	mean2 := sumK2 / N
	variance := math.Max(mean2-beta*beta, 0)
	skew := 0.0
	if variance > 1e-18 {
		skew = (sumK3/N - 3*beta*mean2 + 2*beta*beta*beta) / math.Pow(variance, 1.5)
	}

	// W₂² 目标函数：排序样本与 U[-1/2,1/2] 的中点分位数 t_i-1/2 比较。
	grid := make([]float64, n)
	for i := range grid {
		grid[i] = (float64(i)+0.5)/N - 0.5
	}
	w2sq := func(c float64) float64 {
		s := 0.0
		for i, u := range phases {
			d := u - c - grid[i]
			s += d * d
		}
		return s / N
	}
	cW2 := goldenMin(w2sq, 0, 1, 80)
	offW2 := cW2 * step
	offCF := beta * step
	offInt := int(math.Floor(offCF + 0.5))
	if offInt < 0 {
		offInt = 0
	}
	if offInt > maxK-1 {
		offInt = maxK - 1
	}

	resCont := beta - cW2
	resInt := (beta*step - float64(offInt)) / step // 直接由均值偏移与取整偏移之差得到

	// 固定参考网格 B=Δ，区间 [-Δ/2,Δ/2]。
	B := maxK
	edges := make([]float64, B+1)
	for j := 0; j <= B; j++ {
		edges[j] = -step/2 + float64(j)
	}
	histOrig := canonicalHist(counts, step, 0, B)
	histCont := canonicalHist(counts, step, cW2, B)
	histInt := canonicalHist(counts, step, float64(offInt)/step, B)
	uniform := make([]float64, B)
	for j := range uniform {
		uniform[j] = 1 / float64(B)
	}

	jsO := jsDivergence(histOrig, uniform)
	jsC := jsDivergence(histCont, uniform)
	jsI := jsDivergence(histInt, uniform)
	klO := klSmoothed(counts, step, 0, B, n)
	klC := klSmoothed(counts, step, cW2, B, n)
	klI := klSmoothed(counts, step, float64(offInt)/step, B, n)
	c2O := chi2(histOrig, uniform)
	c2C := chi2(histCont, uniform)
	c2I := chi2(histInt, uniform)

	down := jsI < jsO && klI < klO && c2I < c2O
	biasPass := math.Abs(resInt) <= BiasTolerance

	// 宽窗画图直方图 [-Δ,Δ]，2Δ 格。
	WB := 2 * B
	wideEdges := make([]float64, WB+1)
	for j := 0; j <= WB; j++ {
		wideEdges[j] = -step + float64(j)
	}
	wideOrig := wideHist(counts, 0, B)
	wideInt := wideHist(counts, offInt, B)

	return Cell{
		Class: class, Bits: bits, Levels: levels, Step: step, Pixels: n,
		PhaseMean: beta, DiscreteUniformMean: beta0, BiasGrayOriginal: beta * step,
		TiltKappa: 12 * (beta - beta0), PhaseSkewness: skew,
		PositiveFraction: 1.0, OutsideHalfFrac: float64(outside) / N,

		OffsetClosedForm: offCF, OffsetW2: offW2, OffsetInt: offInt,
		SolverAgreement: math.Abs(cW2 - beta),

		ResidualBiasContinuous: resCont, ResidualBiasInteger: resInt,

		JSOriginal: jsO, JSContinuous: jsC, JSInteger: jsI,
		KLOriginal: klO, KLContinuous: klC, KLInteger: klI,
		Chi2Original: c2O, Chi2Continuous: c2C, Chi2Integer: c2I,
		Chi2StatOriginal: N * c2O, Chi2StatInteger: N * c2I,
		W2SqOriginal: w2sq(0), W2SqOptimal: w2sq(cW2), W2SqInteger: w2sq(float64(offInt) / step),

		BiasPass: biasPass, DivergenceDown: down, CellPass: biasPass && down,

		BinEdgesGray: edges, HistOriginal: histOrig, HistInteger: histInt, Uniform: uniform,
		WideEdgesGray: wideEdges, WideOriginal: wideOrig, WideInteger: wideInt,
	}
}

// BuildReport 对四类图 × 三档位深跑完整分析并汇总每类判定。
func BuildReport(images map[string][]uint8, w, h int, bitsList []int, seed int64) *Report {
	r := &Report{
		Threshold: BiasTolerance, BitDepths: bitsList, Classes: ClassNames,
		Width: w, Height: h, Seed: seed, AllPass: true,
	}
	for _, class := range ClassNames {
		pix := images[class]
		v := ClassVerdict{Class: class, BiasPass: true, AllDivergenceDown: true}
		for _, bits := range bitsList {
			cell := Analyze(pix, w, h, bits, class)
			r.Cells = append(r.Cells, cell)
			if math.Abs(cell.ResidualBiasInteger) > v.MaxResidualBias {
				v.MaxResidualBias = math.Abs(cell.ResidualBiasInteger)
				v.WorstDepthBits = bits
			}
			v.BiasPass = v.BiasPass && cell.BiasPass
			v.AllDivergenceDown = v.AllDivergenceDown && cell.DivergenceDown
		}
		v.Pass = v.BiasPass && v.AllDivergenceDown
		r.ClassVerdicts = append(r.ClassVerdicts, v)
		r.AllPass = r.AllPass && v.Pass
	}
	return r
}

// canonicalHist 把相位计数按归一化偏移 c 平移后，硬分配到 B=Δ 个覆盖
// [-1/2,1/2] 的格子；越界夹到边格。返回概率向量。
func canonicalHist(counts []int, step, c float64, B int) []float64 {
	h := make([]float64, B)
	total := 0.0
	for k, cnt := range counts {
		if cnt == 0 {
			continue
		}
		v := float64(k)/step - c
		idx := int(math.Floor((v + 0.5) * float64(B)))
		if idx < 0 {
			idx = 0
		}
		if idx > B-1 {
			idx = B - 1
		}
		h[idx] += float64(cnt)
		total += float64(cnt)
	}
	for j := range h {
		h[j] /= total
	}
	return h
}

// klSmoothed 用 Add-1（Laplace）平滑计算 D_KL(p_hat || uniform)，单位 bit。
// 平滑在计数空间进行：p_j=(h_j+1)/(N+B)。
func klSmoothed(counts []int, step, c float64, B, n int) float64 {
	// 重新取硬分配的整数格计数。
	raw := make([]int, B)
	for k, cnt := range counts {
		if cnt == 0 {
			continue
		}
		v := float64(k)/step - c
		idx := int(math.Floor((v + 0.5) * float64(B)))
		if idx < 0 {
			idx = 0
		}
		if idx > B-1 {
			idx = B - 1
		}
		raw[idx] += cnt
	}
	den := float64(n + B)
	d := 0.0
	for _, hc := range raw {
		p := (float64(hc) + 1) / den
		q := 1 / float64(B)
		d += p * math.Log2(p/q)
	}
	return d
}

// wideHist 返回覆盖 [-Δ,Δ] 的 2Δ 格宽窗概率（单位灰度 1/格），不夹取，
// 因为所有误差都落在窗内。off 为整数偏移（灰度）。
func wideHist(counts []int, off, B int) []float64 {
	W := 2 * B
	h := make([]float64, W)
	total := 0
	for k, cnt := range counts {
		e := k - off // 灰度误差
		idx := e + B // -Δ -> 0, Δ-1 -> 2Δ-1
		if idx < 0 {
			idx = 0
		}
		if idx > W-1 {
			idx = W - 1
		}
		h[idx] += float64(cnt)
		total += cnt
	}
	for j := range h {
		h[j] /= float64(total)
	}
	return h
}

func jsDivergence(p, q []float64) float64 {
	d := 0.0
	for i := range p {
		m := (p[i] + q[i]) / 2
		if p[i] > 0 {
			d += 0.5 * p[i] * math.Log2(p[i]/m)
		}
		if q[i] > 0 {
			d += 0.5 * q[i] * math.Log2(q[i]/m)
		}
	}
	return d
}

func chi2(p, q []float64) float64 {
	d := 0.0
	for i := range p {
		dx := p[i] - q[i]
		d += dx * dx / q[i]
	}
	return d
}

// goldenMin 在 [lo,hi] 上用黄金分割求 f 的近似最小点。目标函数为凸二次，
// 80 次迭代后区间远小于任何实际需要的精度。
func goldenMin(f func(float64) float64, lo, hi float64, iters int) float64 {
	const gr = 0.6180339887498949
	c := hi - gr*(hi-lo)
	d := lo + gr*(hi-lo)
	fc, fd := f(c), f(d)
	for i := 0; i < iters; i++ {
		if fc < fd {
			hi = d
			d, fd = c, fc
			c = hi - gr*(hi-lo)
			fc = f(c)
		} else {
			lo = c
			c, fc = d, fd
			d = lo + gr*(hi-lo)
			fd = f(d)
		}
	}
	if fc < fd {
		return c
	}
	return d
}
