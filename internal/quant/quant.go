// Package quant 实现 8 位灰度的标量量化，并提供两类重建电平：
//
//   - 中点重建：把 [0,255] 均分为 K=2^b 个区间，取每个区间的中点；
//   - 条件期望重建：对给定区间取 E[X|X∈区间]，用不动点迭代同时更新
//     重建电平与最近邻判决边界。
//
// 两类重建都保留，调用方通过 api.Options.Recon 选择。
package quant

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

const (
	// MinBits / MaxBits 是支持的量化位深范围。8 位时 K=256。
	MinBits = 1
	MaxBits = 8

	// MaxValue 是输入灰度的最大值。
	MaxValue = 255

	// DefaultTol 是不动点迭代的默认收敛阈值（重建电平的绝对变化）。
	DefaultTol = 1e-6
	// DefaultMaxIter 是不动点迭代的默认上限。
	DefaultMaxIter = 100
)

var (
	// ErrNotConverged 表示迭代到上限仍未满足收敛判据。
	// 此时调用方拿到的是错误，而不是半成品电平。
	ErrNotConverged = errors.New("不动点迭代超过上限仍未收敛")
	// ErrBadBits 表示位深不在 [MinBits, MaxBits]。
	ErrBadBits = errors.New("量化位深必须在 1..8")
	// ErrBadTol 表示收敛阈值为负或非有限值。
	ErrBadTol = errors.New("收敛阈值必须是非负有限数")
	// ErrBadMaxIter 表示迭代上限不是正整数。
	ErrBadMaxIter = errors.New("最大迭代次数必须为正整数")
	// ErrEmptyHistogram 表示输入直方图没有任何样本。
	ErrEmptyHistogram = errors.New("直方图为空")
	// ErrUnsortedLevels 表示迭代过程中重建电平失去严格递增顺序。
	ErrUnsortedLevels = errors.New("重建电平未保持递增")
)

// Histogram 是 8 位灰度的样本计数，下标即灰度值。
// 用计数而不是概率，避免先验分布归一化带来的额外舍入。
type Histogram [256]uint64

// FromPixels 统计一张灰度图的直方图。
func FromPixels(pix []uint8) Histogram {
	var h Histogram
	for _, v := range pix {
		h[v]++
	}
	return h
}

// Total 返回样本总数。
func (h Histogram) Total() uint64 {
	var n uint64
	for _, c := range h {
		n += c
	}
	return n
}

// Result 是一次量化器构造的结果。
type Result struct {
	Bits       int       // 位深 b
	Levels     []float64 // K=2^b 个重建电平，按灰度升序
	Iterations int       // 不动点迭代实际轮数；中点重建为 0
	MSE        float64   // 该组电平在输入直方图上的均方误差
}

// LevelCount 返回位深 b 对应的电平数 K=2^b。
func LevelCount(bits int) (int, error) {
	if bits < MinBits || bits > MaxBits {
		return 0, fmt.Errorf("%w: %d", ErrBadBits, bits)
	}
	return 1 << bits, nil
}

// Midpoint 返回中点重建电平。第 k 个区间是
//
//	[-0.5+k·Δ, -0.5+(k+1)·Δ),  Δ = 256/K,
//
// 电平取区间中点 -0.5+(k+0.5)·Δ。
// 该路径不依赖样本分布，Iterations 固定为 0。
func Midpoint(bits int) (Result, error) {
	levels, err := midpointLevels(bits)
	if err != nil {
		return Result{}, err
	}
	return Result{Bits: bits, Levels: levels}, nil
}

// Optimal 用不动点迭代求条件期望重建电平。
//
// 初值取中点重建电平 r_k^(0) = -0.5+(k+0.5)·256/K。
// 每轮先用 r^(t) 的最近邻边界 b_k=(r_k+r_{k+1})/2 划分样本，再把每个
// 电平整体替换为该区间的条件期望（步长为 1，无阻尼）：
//
//	r_k^(t+1) = Σ_{x∈R_k} x·p_x / Σ_{x∈R_k} p_x
//
// 收敛判据：max_k |r_k^(t+1)-r_k^(t)| <= tol。
// 若迭代 maxIter 轮仍未满足，返回 ErrNotConverged，不静默返回半成品。
//
// 空区间的电平保持上一轮取值；条件期望对空集无定义。调用方应保证
// 直方图有足够多的非零灰度值，通常 K 不超过非零值个数。
func Optimal(h Histogram, bits int, tol float64, maxIter int) (Result, error) {
	levels, err := midpointLevels(bits)
	if err != nil {
		return Result{}, err
	}
	if h.Total() == 0 {
		return Result{}, ErrEmptyHistogram
	}
	if math.IsNaN(tol) || math.IsInf(tol, 0) || tol < 0 {
		return Result{}, fmt.Errorf("%w: %g", ErrBadTol, tol)
	}
	if maxIter <= 0 {
		return Result{}, fmt.Errorf("%w: %d", ErrBadMaxIter, maxIter)
	}

	k := len(levels)
	sums := make([]float64, k)
	weights := make([]float64, k)
	next := make([]float64, k)
	converged := false
	iters := 0

	for iters = 1; iters <= maxIter; iters++ {
		bounds := boundaries(levels)

		for i := range sums {
			sums[i] = 0
			weights[i] = 0
		}
		for x, c := range h {
			if c == 0 {
				continue
			}
			bin := lookup(bounds, float64(x))
			sums[bin] += float64(x) * float64(c)
			weights[bin] += float64(c)
		}

		for i := range next {
			if weights[i] == 0 {
				next[i] = levels[i] // 空区间：保持上一轮电平
			} else {
				next[i] = sums[i] / weights[i] // 条件期望，步长 1
			}
		}
		if err := checkIncreasing(next); err != nil {
			return Result{Iterations: iters}, err
		}

		delta := 0.0
		for i := range levels {
			if d := math.Abs(next[i] - levels[i]); d > delta {
				delta = d
			}
		}
		copy(levels, next)

		if delta <= tol {
			converged = true
			break
		}
	}

	if !converged {
		// 不返回任何电平：调用方只能从错误里拿到失败信息，避免误用半成品。
		return Result{}, fmt.Errorf("%w: bits=%d maxIter=%d tol=%g",
			ErrNotConverged, bits, maxIter, tol)
	}

	return Result{
		Bits:       bits,
		Levels:     levels,
		Iterations: iters,
		MSE:        Distortion(h, levels),
	}, nil
}

// Distortion 返回给定重建电平在直方图上的均方误差：
//
//	MSE = (1/N)·Σ_x h[x]·(x-r_{k(x)})²,  k(x)=argmin_k |x-r_k|.
func Distortion(h Histogram, levels []float64) float64 {
	total := h.Total()
	if total == 0 || len(levels) == 0 {
		return math.NaN()
	}
	bounds := boundaries(levels)
	var sum float64
	for x, c := range h {
		if c == 0 {
			continue
		}
		r := levels[lookup(bounds, float64(x))]
		d := float64(x) - r
		sum += float64(c) * d * d
	}
	return sum / float64(total)
}

// QuantizePixels 按最近邻电平重建。输出是 uint8，因此每档电平取整
// （round 到最近的整数，再夹到 [0,255]）。这与"整档电平最小化平方误差"
// 一致：固定一档的样本集合后，最优整数电平就是该档实数电平的四舍五入。
//
// 取整会让实际图像的误差与实数电平的 Distortion 略有差别；要报告实际
// 误差请用 DistortionRounded，两条重建路径都用同一把尺子。
func QuantizePixels(pix []uint8, levels []float64) []uint8 {
	out := make([]uint8, len(pix))
	if len(levels) == 0 {
		copy(out, pix)
		return out
	}
	bounds := boundaries(levels)
	for i, v := range pix {
		out[i] = roundLevel(levels[lookup(bounds, float64(v))])
	}
	return out
}

func roundLevel(r float64) uint8 {
	x := math.Round(r)
	if x < 0 {
		return 0
	}
	if x > MaxValue {
		return MaxValue
	}
	return uint8(x)
}

// DistortionRounded 与 Distortion 相同，但先把每档电平取整到 uint8，
// 用来评估实际写出的图像误差。
func DistortionRounded(h Histogram, levels []float64) float64 {
	total := h.Total()
	if total == 0 || len(levels) == 0 {
		return math.NaN()
	}
	bounds := boundaries(levels)
	var sum float64
	for x, c := range h {
		if c == 0 {
			continue
		}
		r := float64(roundLevel(levels[lookup(bounds, float64(x))]))
		d := float64(x) - r
		sum += float64(c) * d * d
	}
	return sum / float64(total)
}

func midpointLevels(bits int) ([]float64, error) {
	k, err := LevelCount(bits)
	if err != nil {
		return nil, err
	}
	step := 256.0 / float64(k)
	levels := make([]float64, k)
	for i := range levels {
		levels[i] = -0.5 + (float64(i)+0.5)*step
	}
	return levels, nil
}

// boundaries 返回 K+1 个最近邻判决边界。b[0]=-0.5、b[K]=255.5 把整数
// 灰度 0..255 全部覆盖，内部边界取相邻电平的中点。
func boundaries(levels []float64) []float64 {
	k := len(levels)
	b := make([]float64, k+1)
	b[0] = -0.5
	for i := 1; i < k; i++ {
		b[i] = (levels[i-1] + levels[i]) / 2
	}
	b[k] = MaxValue + 0.5
	return b
}

// lookup 返回 x 所属的区间下标：bounds[k] <= x < bounds[k+1]。
func lookup(bounds []float64, x float64) int {
	k := sort.Search(len(bounds)-1, func(i int) bool {
		return x < bounds[i+1]
	})
	if k >= len(bounds)-1 {
		k = len(bounds) - 2
	}
	return k
}

func checkIncreasing(levels []float64) error {
	for i := 1; i < len(levels); i++ {
		if !(levels[i] > levels[i-1]) {
			return fmt.Errorf("%w: r[%d]=%g, r[%d]=%g",
				ErrUnsortedLevels, i-1, levels[i-1], i, levels[i])
		}
	}
	return nil
}
