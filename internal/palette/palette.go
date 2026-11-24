// Package palette 实现灰度图的中位切分（median cut）调色板量化。
//
// 量化只改像素值，不产生、也不修改任何格式的文件头；调用方拿到与输入同尺寸的
// 灰度像素后，仍交给原有的 PGM/BMP/RLE 编码器。因此调色板开关关闭时输出与基准
// 逐字节一致；开启时三种格式的头长度与内容同样不变，只有像素负载不同。
package palette

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

const (
	// MaxBuckets 是 8 位灰度调色板能表示的最大桶数。
	MaxBuckets = 256
	// ValueMax 是 8 位样本最大值。
	ValueMax = 255
	// VarianceCeiling 是 8 位单通道总体方差的理论上界：
	// 分布 p 在 0、1-p 在 255 时方差 p(1-p)·255²，p=1/2 取最大，即 255²/4。
	VarianceCeiling = float64(ValueMax*ValueMax) / 4.0 // 16256.25
)

var (
	// ErrBuckets 表示桶数不在 [1, MaxBuckets] 内（含桶数为 0）。
	ErrBuckets = errors.New("调色板桶数非法")
	// ErrThreshold 表示桶内方差阈值非有限、为负，或大于理论上界（倒置）。
	ErrThreshold = errors.New("调色板方差阈值倒置")
)

// Options 是中位切分量化参数。
type Options struct {
	// Buckets 是桶数上限 K，必须落在 [1, MaxBuckets]。
	Buckets int
	// VarianceThreshold 是桶内方差阈值 τ 的覆盖值（灰度阶²）。
	// 取 0 时按 τ(K) = (256/K)²/12 自动推导；取正值时手动指定，
	// 且必须落在 (0, VarianceCeiling]，否则按“倒置”拒绝。
	VarianceThreshold float64
}

// StopReason 说明切分为何结束。
type StopReason int

const (
	// StopThreshold 表示因最大桶内方差 ≤ τ 而停止。
	StopThreshold StopReason = iota
	// StopBucketCap 表示因桶数达到 K 而停止。
	StopBucketCap
	// StopNoSplittable 表示已没有可切分的桶（每个桶内样本全同）。
	StopNoSplittable
)

func (r StopReason) String() string {
	switch r {
	case StopThreshold:
		return "threshold"
	case StopBucketCap:
		return "bucket-cap"
	default:
		return "no-splittable"
	}
}

// DeriveThreshold 由桶数 K 推导桶内方差停止阈值 τ（灰度阶²）。
//
// 推导：
//  1. 理想 K 级均匀标量量化器把 0..255 划成 K 个等宽格，格宽 Δ = 256/K；
//  2. 量化误差在 [-Δ/2, Δ/2] 上均匀分布，均方误差为 Δ²/12；
//  3. 取 τ(K) = Δ²/12，含义是“桶已经和理想 K 级量化的单个格一样紧，
//     再切也不会比理想 K 级量化更好”，于是停止切分。
//
// τ 完全由公开参数 K 与 8 位值域 [0,255] 推出，不含魔数。
func DeriveThreshold(k int) (float64, error) {
	if k < 1 || k > MaxBuckets {
		return 0, fmt.Errorf("%w: K=%d 必须在 [1,%d]", ErrBuckets, k, MaxBuckets)
	}
	delta := float64(ValueMax+1) / float64(k)
	return delta * delta / 12.0, nil
}

// AxisOf 返回方差最大的通道下标；并列取最小下标。
// 灰度图只有一个通道（下标 0），该函数恒返回 0；
// 写成通用形式是为了让“切分维判定”可被独立复算与核对。
func AxisOf(variance []float64) int {
	axis := 0
	for c := 1; c < len(variance); c++ {
		if variance[c] > variance[axis] {
			axis = c
		}
	}
	return axis
}

// Split 记录一次切分，供核对“切分维判定”的完整序列。
type Split struct {
	Step     int     // 第几次切分，从 1 开始
	Axis     int     // 本次沿哪个通道切（灰度恒为 0）
	Variance float64 // 切分前父桶在 Axis 上的总体方差
	Pivot    uint8   // 中位切分值
	LeftN    int     // 左桶像素数
	RightN   int     // 右桶像素数
}

// Bucket 是一个最终桶的可复算中间量。
type Bucket struct {
	Count    int       // 桶内像素数
	Lo, Hi   int       // 在值有序索引数组中的半开区间
	Mean     []float64 // 各通道均值
	Variance []float64 // 各通道总体方差；Axis 即其 argmax
	Axis     int       // 本桶切分维 = AxisOf(Variance)
	Value    uint8     // 均值四舍五入后的调色板项
}

// Result 是一次量化的全部可核对数据。
type Result struct {
	Pix            []uint8  // 量化后的像素
	Palette        []uint8  // 去重升序的调色板项
	Buckets        []Bucket // 最终桶（按 Lo 升序）
	Splits         []Split  // 切分序列
	Requested      int      // 请求的桶数上限 K
	Actual         int      // 实际桶数
	Delta          float64  // Δ = 256/K
	Threshold      float64  // 实际使用的 τ
	Stop           StopReason
	MaxVariance    float64 // 终止时各桶方差的最大值
	WithinVariance float64 // W = (1/N)Σ_b Σ_{i∈b}(v_i-mean_b)²
	MSE            float64 // 量化后与输入的均方误差
	// MSEBound = W + 1/4，见 Quantize 的证明。
	MSEBound float64
	// ParamBound 为仅由参数与停止原因推出的上界：
	// 因阈值停止时 τ+1/4，否则 VarianceCeiling+1/4。
	ParamBound float64
}

// channelCount 在本仓库固定为 1：输入是灰度图。
const channelCount = 1

type bucket struct {
	lo, hi int
	id     int
	mean   [channelCount]float64
	vars   [channelCount]float64
	axis   int
	plain  bool // 桶内所有样本相同，无法再切
}

// Quantize 对灰度像素做中位切分量化。
//
// 算法：
//  1. 把所有像素索引按 (值, 原索引) 排序一次；之后每次切分只做区间划分，
//     子桶天然保持有序，结果确定且与运行环境无关。
//  2. 每轮在可切分的桶里选“本桶切分维上方差最大”者；并列时取像素更多者，
//     再并列取创建更早者。
//  3. 选中的桶在其切分维（argmax 方差，灰度恒为 0）上按“下中位值”切；
//     切点挪到第一个严格大于中位值处，避免把相同值劈到两个桶。
//  4. 终止：桶数达到 K、最大桶方差 ≤ τ、或没有可切桶。
//
// 调色板项取桶内均值四舍五入；每个像素映射到自己所在桶的调色板项。
//
// MSE 上界证明：设桶 b 的精确均值为 m_b、调色板项 p_b=round(m_b)，则
//
//	(1/n_b)Σ(v_i-p_b)² = σ_b² + (m_b-p_b)² ≤ σ_b² + 1/4，
//
// 因为 |m_b-p_b| ≤ 1/2。对桶按像素数加权即得
//
//	MSE = Σ_b (n_b/N)(σ_b²+δ_b²) ≤ W + 1/4，
//
// 其中 W=(1/N)Σ n_b σ_b²。又 σ_b² ≤ VarianceCeiling，故 MSE ≤ VarianceCeiling+1/4；
// 若因阈值停止，则每个桶 σ_b² ≤ τ，故 MSE ≤ τ+1/4。
func Quantize(pix []uint8, opt Options) (*Result, error) {
	if len(pix) == 0 {
		return nil, fmt.Errorf("%w: 空像素", ErrBuckets)
	}
	if opt.Buckets < 1 || opt.Buckets > MaxBuckets {
		return nil, fmt.Errorf("%w: K=%d 必须在 [1,%d]", ErrBuckets, opt.Buckets, MaxBuckets)
	}
	tau, err := DeriveThreshold(opt.Buckets)
	if err != nil {
		return nil, err
	}
	if opt.VarianceThreshold != 0 {
		t := opt.VarianceThreshold
		if math.IsNaN(t) || math.IsInf(t, 0) || t < 0 || t > VarianceCeiling {
			return nil, fmt.Errorf("%w: τ=%v 必须为 0（自动）或落在 (0,%v]",
				ErrThreshold, t, VarianceCeiling)
		}
		tau = t
	}

	all := make([]int, len(pix))
	for i := range all {
		all[i] = i
	}
	// 一次排序；子桶是该序的连续区间，切分只缩区间，不再排序。
	sort.SliceStable(all, func(i, j int) bool {
		vi, vj := pix[all[i]], pix[all[j]]
		if vi != vj {
			return vi < vj
		}
		return all[i] < all[j]
	})

	buckets := []*bucket{{lo: 0, hi: len(pix), id: 0}}
	computeBucket(buckets[0], pix, all)
	nextID := 1
	var splits []Split
	stop := StopBucketCap

	for len(buckets) < opt.Buckets {
		best := -1
		for i, b := range buckets {
			if b.plain {
				continue
			}
			if best == -1 {
				best = i
				continue
			}
			bb := buckets[best]
			bv, bbv := b.vars[b.axis], bb.vars[bb.axis]
			bn, bbn := b.hi-b.lo, bb.hi-bb.lo
			if bv > bbv || (bv == bbv && (bn > bbn || (bn == bbn && b.id < bb.id))) {
				best = i
			}
		}
		if best == -1 {
			stop = StopNoSplittable
			break
		}
		b := buckets[best]
		if b.vars[b.axis] <= tau {
			stop = StopThreshold
			break
		}
		k := splitIndex(b, pix, all)
		if k <= b.lo || k >= b.hi {
			b.plain = true
			continue
		}
		left := &bucket{lo: b.lo, hi: k, id: nextID}
		nextID++
		right := &bucket{lo: k, hi: b.hi, id: nextID}
		nextID++
		computeBucket(left, pix, all)
		computeBucket(right, pix, all)
		splits = append(splits, Split{
			Step:     len(splits) + 1,
			Axis:     b.axis,
			Variance: b.vars[b.axis],
			Pivot:    pix[all[(b.lo+b.hi-1)/2]],
			LeftN:    left.hi - left.lo,
			RightN:   right.hi - right.lo,
		})
		buckets[best] = left
		buckets = append(buckets, right)
	}

	sort.Slice(buckets, func(i, j int) bool { return buckets[i].lo < buckets[j].lo })

	res := &Result{
		Pix:       make([]uint8, len(pix)),
		Requested: opt.Buckets,
		Actual:    len(buckets),
		Delta:     float64(ValueMax+1) / float64(opt.Buckets),
		Threshold: tau,
		Stop:      stop,
		Splits:    splits,
	}

	var sumSqErr, within float64
	seen := make(map[uint8]bool, len(buckets))
	for _, b := range buckets {
		n := b.hi - b.lo
		p := roundMean(b.mean[0])
		res.Buckets = append(res.Buckets, Bucket{
			Count:    n,
			Lo:       b.lo,
			Hi:       b.hi,
			Mean:     []float64{b.mean[0]},
			Variance: []float64{b.vars[0]},
			Axis:     b.axis,
			Value:    p,
		})
		if !seen[p] {
			seen[p] = true
			res.Palette = append(res.Palette, p)
		}
		if b.vars[b.axis] > res.MaxVariance {
			res.MaxVariance = b.vars[b.axis]
		}
		within += float64(n) * b.vars[b.axis]
		for _, id := range all[b.lo:b.hi] {
			res.Pix[id] = p
			d := float64(int(pix[id]) - int(p))
			sumSqErr += d * d
		}
	}
	sort.Slice(res.Palette, func(i, j int) bool { return res.Palette[i] < res.Palette[j] })

	n := float64(len(pix))
	res.MSE = sumSqErr / n
	res.WithinVariance = within / n
	res.MSEBound = res.WithinVariance + 0.25
	if stop == StopThreshold {
		res.ParamBound = tau + 0.25
	} else {
		res.ParamBound = VarianceCeiling + 0.25
	}
	return res, nil
}

func computeBucket(b *bucket, pix []uint8, all []int) {
	n := b.hi - b.lo
	var sum, sumsq int
	mn, mx := ValueMax, 0
	for _, id := range all[b.lo:b.hi] {
		v := int(pix[id])
		sum += v
		sumsq += v * v
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	fn := float64(n)
	mean := float64(sum) / fn
	variance := float64(sumsq)/fn - mean*mean
	if variance < 0 {
		variance = 0 // 抵消浮点误差
	}
	b.mean[0] = mean
	b.vars[0] = variance
	b.axis = AxisOf(b.vars[:])
	b.plain = mn == mx
}

// splitIndex 返回右桶起点。首选“第一个严格大于下中位值”的位置；
// 若中位值恰好是区间最大值（如 [1,2,2]），退化到“第一个大于等于它”的位置，
// 仍能保证左右两桶非空。调用前 all[b.lo:b.hi] 已按值非降序。
func splitIndex(b *bucket, pix []uint8, all []int) int {
	n := b.hi - b.lo
	pivot := pix[all[b.lo+(n-1)/2]]
	off := sort.Search(n, func(i int) bool {
		return pix[all[b.lo+i]] > pivot
	})
	if off == n {
		off = sort.Search(n, func(i int) bool {
			return pix[all[b.lo+i]] >= pivot
		})
	}
	return b.lo + off
}

func roundMean(v float64) uint8 {
	q := int(math.Round(v))
	if q < 0 {
		return 0
	}
	if q > ValueMax {
		return ValueMax
	}
	return uint8(q)
}
