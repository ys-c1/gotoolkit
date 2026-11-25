// Package graymap 实现位深感知的灰度映射（LUT）。
//
// 流程分三步：
//  1. 按理想单调曲线（gamma 2.2，端点固定 0..L-1）逐点取整得到原始 LUT；
//     逐点独立取整会在曲线斜率小于 1 LSB 的暗部产生倒挂。
//  2. 用累积最大值（cummax）强制单调，消除全部倒挂且不改外层端点。
//  3. 对 cummax 产生的平台段做整数插值铺开，能严格递增的位置不允许并列；
//     位深不够、无码级可铺的固有平台保留并计入报告。
package graymap

import (
	"errors"
	"math"
)

// 支持的三档位深：8/10/12 bit。
const (
	Bits8  = 8
	Bits10 = 10
	Bits12 = 12
)

// Gamma 是理想映射曲线的指数，固定 2.2（暗部压缩，亮部拉伸）。
const Gamma = 2.2

// bayer4 是 4x4 有序抖动矩阵，值取自标准 Bayer 顺序。
// 归一化后给逐点取整提供确定性的误差序列，误差严格落在 [-0.5, 0.5)。
var bayer4 = [16]int{
	0, 8, 2, 10,
	12, 4, 14, 6,
	3, 11, 1, 9,
	15, 7, 13, 5,
}

// LUT 是一份位深为 Bits 的灰度查找表，长度 1<<Bits，取值域 [0, 1<<Bits-1]。
// LUT 保证非递减（倒挂点数为零），外层端点恒为 0 与 1<<Bits-1。
type LUT struct {
	Bits int
	// Raw 是逐点取整后的原始映射，可能含倒挂与平台。
	Raw []int
	// Mono 是对 Raw 取累积最大值后的映射，无倒挂、端点不变。
	Mono []int
	// Fixed 是对 Mono 的平台段铺开后的最终映射，无倒挂，能严格递增处无并列。
	Fixed []int
}

// Inversion 记录一处倒挂：相邻两点 i0=I-1、i1=I，映射值从 Y0 跌到 Y1。
type Inversion struct {
	I0, I1 int
	Y0, Y1 int
}

// Plateau 记录一个平台段（Mono 中连续相等的区间）。
type Plateau struct {
	Start, End int // 闭区间
	Value      int
	// Spread 为 true 表示该平台有足够码级可铺，已做严格递增插值；
	// 为 false 表示位深不足、无富余码级，平台只能保留（固有并列，非倒挂）。
	Spread bool
}

// Report 汇总一次 Build 的全部可核对指标。
type Report struct {
	Bits int
	// InversionCount 是 Raw 中倒挂点（相邻差为负）的数量。
	InversionCount int
	// Inversions 保留倒挂明细，顺序沿输入码值递增。
	Inversions []Inversion
	// RawTieCount 是 Raw 中相邻并列（差为零）的步数。
	RawTieCount int
	// ResidualTieRuns / ResidualTieSteps 是 Fixed 中无法铺开的固有平台数与并列步数。
	ResidualTieRuns  int
	ResidualTieSteps int
	// Plateaus 是 Mono 中全部平台段的处理记录。
	Plateaus []Plateau
	// MaxDeviationRaw 是 max_i |Fixed[i]-Raw[i]|，单位 LSB。
	MaxDeviationRaw int
	// MaxDeviationMono 是 max_i |Mono[i]-Raw[i]|，单位 LSB。
	MaxDeviationMono int
	// MaxDeviationIdeal 是 max_i |Fixed[i]-ideal(i)|，单位 LSB（浮点）。
	MaxDeviationIdeal float64
	// RawEnd0/RawEndN 与 FixedEnd0/FixedEndN 用于端点复核。
	RawEnd0, RawEndN     int
	FixedEnd0, FixedEndN int
}

// ValidBits 判断位深是否在受支持的三档内。
func ValidBits(bits int) bool {
	return bits == Bits8 || bits == Bits10 || bits == Bits12
}

// ideal 是理想连续单调曲线 f(t)=(L-1)t^Gamma，t=i/(n-1)。
// 它在 [0,1] 上连续、严格递增，f(0)=0、f(1)=L-1。
func ideal(i, n int) float64 {
	t := float64(i) / float64(n-1)
	if t == 0 {
		return 0
	}
	return float64(n-1) * math.Pow(t, Gamma)
}

// roundHalfUp 是四舍五入（半数向上），把浮点映射值量化成整数码值。
func roundHalfUp(x float64) int { return int(math.Floor(x + 0.5)) }

// Build 生成指定位深的灰度映射表与核对报告。
func Build(bits int) (*LUT, *Report, error) {
	if !ValidBits(bits) {
		return nil, nil, errors.New("graymap: 仅支持 8/10/12 三档位深")
	}
	n := 1 << bits

	raw := make([]int, n)
	for i := 0; i < n; i++ {
		// 逐点独立取整：四舍五入叠加确定性有序抖动。
		// 抖动 e=d-0.5 ∈ [-0.5, 0.4375)，量化点为 round(f(i)+e)，
		// 与固定点实现中逐点独立产生的取整误差同分布，且可复现。
		e := float64(bayer4[i&15])/16.0 - 0.5
		y := roundHalfUp(ideal(i, n) + e)
		if y < 0 {
			y = 0
		}
		if y > n-1 {
			y = n - 1
		}
		raw[i] = y
	}

	mono := cumulativeMax(raw)
	fixed, plateaus := spreadPlateaus(mono)

	rep := &Report{Bits: bits}
	rep.Inversions, rep.InversionCount = findInversions(raw)
	rep.RawTieCount = countEqualSteps(raw)
	for _, p := range plateaus {
		if !p.Spread {
			rep.ResidualTieRuns++
			rep.ResidualTieSteps += p.End - p.Start
		}
	}
	rep.Plateaus = plateaus
	rep.MaxDeviationMono = maxAbsDiffInt(mono, raw)
	rep.MaxDeviationRaw = maxAbsDiffInt(fixed, raw)
	rep.MaxDeviationIdeal = maxDeviationIdeal(fixed, n)
	rep.RawEnd0, rep.RawEndN = raw[0], raw[n-1]
	rep.FixedEnd0, rep.FixedEndN = fixed[0], fixed[n-1]

	return &LUT{Bits: bits, Raw: raw, Mono: mono, Fixed: fixed}, rep, nil
}

// cumulativeMax 返回 m[i]=max_{k<=i} raw[k]。
// m 非递减；只要首点未被钳动则 m[0]=raw[0]，末点 m[n-1]=raw[n-1]
// （末点是全表最大值，见单调性证明文档）。
func cumulativeMax(raw []int) []int {
	m := make([]int, len(raw))
	peak := raw[0]
	for i, v := range raw {
		if v > peak {
			peak = v
		}
		m[i] = peak
	}
	return m
}

// spreadPlateaus 处理 Mono 上的每一个等值平台段。
//
// 平台 [s,e] 的值为 v，右邻码级为 h（末段 h=L-1）。
// 可用步进空间 span=h-v（末段）或 h-1-v（非末段，须给右邻预留一个码级，
// 否则整除插值的末点会与右邻相等，重新制造并列）。
// 需要的严格步数 need=e-s。span>=need 时按 v+floor(k*span/need) 整数插值铺开，
// 段内严格递增且严格小于右邻，避免并列；span<need 时位深不足，保留原平台
// （固有并列，只会形成平台，不会产生倒挂）。
func spreadPlateaus(mono []int) ([]int, []Plateau) {
	n := len(mono)
	out := append([]int(nil), mono...)
	var plateaus []Plateau
	i := 0
	for i < n {
		j := i
		for j+1 < n && out[j+1] == out[i] {
			j++
		}
		if j == i {
			i++
			continue
		}
		v := out[i]
		h := n - 1
		last := j+1 == n
		if !last {
			h = out[j+1]
		}
		span := h - v
		if !last {
			span-- // 给右邻预留一个码级
		}
		need := j - i
		p := Plateau{Start: i, End: j, Value: v}
		if span >= need {
			for k := i; k <= j; k++ {
				out[k] = v + (k-i)*span/need
			}
			if last {
				out[j] = h // 末段端点必须钉在 L-1
			}
			p.Spread = true
		}
		plateaus = append(plateaus, p)
		i = j + 1
	}
	return out, plateaus
}

func findInversions(raw []int) ([]Inversion, int) {
	var inv []Inversion
	for i := 1; i < len(raw); i++ {
		if raw[i] < raw[i-1] {
			inv = append(inv, Inversion{I0: i - 1, I1: i, Y0: raw[i-1], Y1: raw[i]})
		}
	}
	return inv, len(inv)
}

func countEqualSteps(v []int) int {
	c := 0
	for i := 1; i < len(v); i++ {
		if v[i] == v[i-1] {
			c++
		}
	}
	return c
}

func maxAbsDiffInt(a, b []int) int {
	m := 0
	for i := range a {
		d := a[i] - b[i]
		if d < 0 {
			d = -d
		}
		if d > m {
			m = d
		}
	}
	return m
}

func maxDeviationIdeal(fixed []int, n int) float64 {
	m := 0.0
	for i := range fixed {
		if d := math.Abs(float64(fixed[i]) - ideal(i, n)); d > m {
			m = d
		}
	}
	return m
}

// IsNonDecreasing 复核一份映射是否非递减（零倒挂的程序化判据）。
func IsNonDecreasing(v []int) bool {
	for i := 1; i < len(v); i++ {
		if v[i] < v[i-1] {
			return false
		}
	}
	return true
}
