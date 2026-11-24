// Package quant 实现调色板量化与有序抖动的联合配比。
//
// # 误差拆分
//
// 把每个输入灰度值 v 看作一个分解单元（下称色阶）。同一个 v 的像素在经过
// 抖动后输出并不相同，对有序抖动的全部 64 个相位求统计量：
//
//	μ_v(τ) = E_d[ out(v, τ, d) ]
//	σ_v²(τ) = Var_d[ out(v, τ, d) ]
//
// 记 p_v 为 v 在图中占的像素比例，定义
//
//	色块误差 B = Σ_v p_v · (μ_v(τ) - v)²
//	噪声误差 N = Σ_v p_v · σ_v²(τ)
//
// B 是“整片同灰度区域被系统性搬移了多少”，也就是色带的来源；N 是“同一灰度
// 区域内部被抖动打散出多少起伏”，也就是噪声。由全方差公式，对每个 v 都有
//
//	E[(out-v)²] = (μ_v-v)² + σ_v²
//
// 所以 B + N 恰好等于整幅图的均方误差（MSE），两部分量纲完全一致：
// 均为“灰度级²/像素”。加权相加因此不会因量纲不同而失真。
//
// # 加权
//
// W = WeightBanding·B + WeightNoise·N，默认 4:1。依据见 WeightBanding 与
// docs/dither.md：色块是低频、空间相干的伪影，缩放和观看距离都无法把它平均
// 掉；抖动噪声是高频、逐像素近独立的，2×2 视觉积分会把噪声功率摊薄约 4 倍。
// 相同均方能量下色块的可见度约为噪声的 4 倍。权重比低于约 1 时最优解退化为
// τ=0（不抖动），4:1 位于最优 τ 变化平缓的平台中部。
//
// # 抖动
//
// τ∈[0,1] 是抖动强度：阈值偏移 d=(b(x,y)+0.5)/64-0.5 乘上 τ，再以量化步长
// q=255/(L-1) 为单位加到输入上，四舍五入到最近的调色板桶。τ=0 为纯色块，
// τ=1 为满强度抖动。相位取自标准 8×8 Bayer 矩阵。
package quant

import (
	"errors"
	"math"
	"sync"
)

// 权重常量。两部分量纲相同（灰度级²/像素），可以直接加权。
const (
	// WeightBanding 是色块误差的权重。色块是低频伪影，缩放、拉远观看都不会
	// 被平均掉，主观可见度约为同能量高频噪声的 4 倍。
	WeightBanding = 4.0
	// WeightNoise 是噪声误差的权重，作为基准取 1。
	WeightNoise = 1.0
)

// 调色板桶数的合法范围。
const (
	MinLevels = 2
	MaxLevels = 256
)

// 抖动强度的合法范围。
const (
	MinTau = 0.0
	MaxTau = 1.0
)

// Parts 是一幅图（或模型预测）的误差分解。
// 两个字段单位相同：灰度级²/像素。
type Parts struct {
	Banding float64 // 色块：各色阶均值误差的平方，按像素占比加权
	Noise   float64 // 噪声：各色阶在抖动相位上的方差，按像素占比加权
}

// MSE 返回两部分之和，即逐像素均方误差。
func (p Parts) MSE() float64 { return p.Banding + p.Noise }

// Weighted 返回按默认 4:1 权重合并的误差。
func (p Parts) Weighted() float64 {
	return WeightBanding*p.Banding + WeightNoise*p.Noise
}

// Plan 是某个桶数下的最优抖动配比。
type Plan struct {
	Levels int     // 调色板桶数
	Tau    float64 // 最优抖动强度
	Parts  Parts   // 该配比下的模型误差
}

// Weighted 返回该配比按默认权重合并的误差。
func (p Plan) Weighted() float64 { return p.Parts.Weighted() }

// bayer 是标准 8×8 Bayer 阈值矩阵，取值 0..63。
var bayer = [8][8]int{
	{0, 48, 12, 60, 3, 51, 15, 63},
	{32, 16, 44, 28, 35, 19, 47, 31},
	{8, 56, 4, 52, 11, 59, 7, 55},
	{40, 24, 36, 20, 43, 27, 39, 23},
	{2, 50, 14, 62, 1, 49, 13, 61},
	{34, 18, 46, 30, 33, 17, 45, 29},
	{10, 58, 6, 54, 9, 57, 5, 53},
	{42, 26, 38, 22, 41, 25, 37, 21},
}

// phases 是 Bayer 矩阵的 64 个归一化偏移，严格落在 (-0.5, 0.5)。
var phases = func() [64]float64 {
	var p [64]float64
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			p[y*8+x] = (float64(bayer[y][x])+0.5)/64.0 - 0.5
		}
	}
	return p
}()

// Step 返回 L 级等距调色板的标称量化步长 q=255/(L-1)。
func Step(levels int) float64 { return 255.0 / float64(levels-1) }

// Palette 返回 L 级等距调色板，两端固定为 0 和 255。
// levels 越界时返回 nil。
func Palette(levels int) []uint8 {
	if levels < MinLevels || levels > MaxLevels {
		return nil
	}
	q := Step(levels)
	pal := make([]uint8, levels)
	for k := range pal {
		v := int(math.Floor(float64(k)*q + 0.5))
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		pal[k] = uint8(v)
	}
	return pal
}

// levelIndex 把“以量化步长为单位”的坐标 z 四舍五入到桶下标并夹紧。
func levelIndex(z float64, levels int) int {
	k := int(math.Floor(z + 0.5))
	if k < 0 {
		return 0
	}
	if k >= levels {
		return levels - 1
	}
	return k
}

// Quantize 把灰度图量化到 levels 级调色板，并按强度 tau 做有序抖动。
// tau=0 时输出只取决于像素值；tau>0 时相位由像素坐标决定。
// 返回与 pix 等长的新切片，不修改输入。
func Quantize(w, h int, pix []uint8, levels int, tau float64) ([]uint8, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	if levels < MinLevels || levels > MaxLevels {
		return nil, errors.New("调色板桶数越界")
	}
	if !(tau >= MinTau && tau <= MaxTau) {
		return nil, errors.New("抖动强度越界")
	}
	pal := Palette(levels)
	q := Step(levels)
	out := make([]uint8, len(pix))
	if tau == 0 {
		for i, v := range pix {
			out[i] = pal[levelIndex(float64(v)/q, levels)]
		}
		return out, nil
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			d := phases[(y&7)*8+(x&7)]
			out[i] = pal[levelIndex(float64(pix[i])/q+tau*d, levels)]
		}
	}
	return out, nil
}

// Measure 把 src 到 out 的逐像素误差拆成色块与噪声。
// src 与 out 必须等长；levels 必须合法。
//
// 同一输入灰度值的像素归为一组，组内误差均值进入 Banding，组内方差进入
// Noise；两部分之和等于整幅图的 MSE。
func Measure(src, out []uint8, levels int) (Parts, bool) {
	if len(src) != len(out) || len(src) == 0 {
		return Parts{}, false
	}
	if levels < MinLevels || levels > MaxLevels {
		return Parts{}, false
	}
	var count, sum, sumSq [256]float64
	for i := range src {
		v := src[i]
		e := float64(out[i]) - float64(v)
		count[v]++
		sum[v] += e
		sumSq[v] += e * e
	}
	n := float64(len(src))
	var p Parts
	for v := 0; v < 256; v++ {
		if count[v] == 0 {
			continue
		}
		mean := sum[v] / count[v]
		variance := sumSq[v]/count[v] - mean*mean
		if variance < 0 {
			variance = 0
		}
		p.Banding += count[v] / n * mean * mean
		p.Noise += count[v] / n * variance
	}
	return p, true
}

// Predict 用“每个输入灰度的抖动相位在 64 个 Bayer 相位上均匀分布”的假设，
// 预测给定直方图在桶数 levels、强度 tau 下的误差分解。
//
// hist[v] 是灰度 v 的权重（计数或概率均可）。预测满足 B+N = MSE，且当图中
// 每个灰度的像素都均匀覆盖全部相位时与 Measure 完全一致。
func Predict(hist [256]float64, levels int, tau float64) Parts {
	if levels < MinLevels || levels > MaxLevels || !(tau >= MinTau && tau <= MaxTau) {
		return Parts{}
	}
	var total float64
	for _, h := range hist {
		total += h
	}
	if total <= 0 {
		return Parts{}
	}
	pal := Palette(levels)
	q := Step(levels)
	const nPhase = float64(len(phases))
	var p Parts
	for v := 0; v < 256; v++ {
		if hist[v] == 0 {
			continue
		}
		var sum, sumSq float64
		for _, d := range phases {
			out := float64(pal[levelIndex(float64(v)/q+tau*d, levels)])
			sum += out
			sumSq += out * out
		}
		mean := sum / nPhase
		variance := sumSq/nPhase - mean*mean
		if variance < 0 {
			variance = 0
		}
		delta := mean - float64(v)
		w := hist[v] / total
		p.Banding += w * delta * delta
		p.Noise += w * variance
	}
	return p
}

// UniformHist 返回 0..255 等权直方图，对应均匀灰度分布。
func UniformHist() [256]float64 {
	var h [256]float64
	for v := range h {
		h[v] = 1
	}
	return h
}

var (
	cacheMu sync.Mutex
	cache   = map[int]Plan{}
)

// Optimize 返回桶数 levels 下、以均匀灰度分布为先验、使默认加权误差最小的
// 抖动配比。结果按桶数缓存。
func Optimize(levels int) Plan {
	if levels < MinLevels || levels > MaxLevels {
		return Plan{Levels: levels}
	}
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if p, ok := cache[levels]; ok {
		return p
	}
	p := optimizeHist(UniformHist(), levels, WeightBanding, WeightNoise)
	cache[levels] = p
	return p
}

// OptimizeHist 在给定输入直方图上按默认权重搜索最优抖动强度。
func OptimizeHist(hist [256]float64, levels int) Plan {
	if levels < MinLevels || levels > MaxLevels {
		return Plan{Levels: levels}
	}
	return optimizeHist(hist, levels, WeightBanding, WeightNoise)
}

// optimizeHist 先用 0.005 步长扫描 τ∈[0,1]，再在最优值附近逐级细化到
// 5e-5。同分时保留较小的 τ（噪声更少）。
func optimizeHist(hist [256]float64, levels int, wb, wn float64) Plan {
	eval := func(tau float64) (float64, Parts) {
		p := Predict(hist, levels, tau)
		return wb*p.Banding + wn*p.Noise, p
	}
	bestTau := 0.0
	bestScore, bestParts := eval(0)
	scan := func(lo, hi, step float64) {
		if lo < 0 {
			lo = 0
		}
		if hi > 1 {
			hi = 1
		}
		n := int(math.Floor((hi-lo)/step + 1e-9))
		for i := 0; i <= n; i++ {
			tau := lo + float64(i)*step
			if tau > hi {
				tau = hi
			}
			score, parts := eval(tau)
			if score < bestScore {
				bestScore, bestTau, bestParts = score, tau, parts
			}
		}
	}
	scan(0, 1, 0.005)
	for _, step := range []float64{0.0005, 0.00005} {
		center := bestTau
		scan(center-0.02, center+0.02, step)
	}
	return Plan{Levels: levels, Tau: bestTau, Parts: bestParts}
}
