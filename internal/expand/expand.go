// Package expand 把 8 位灰度按亮度守恒展开成 R/G/B 三通道，再按同一组
// 权重把三通道收回成灰度。
//
// 展开系数不是试出来的：亮度守恒只给出一条等式 Σ_c w_c·k_c = 1，
// 在三维系数空间里是一个平面，解不唯一。再取「系数向量范数最小」，
// 即求线性约束下的最小范数解，得到唯一解 k = w/‖w‖²。这组解的
// R:G:B = w_R:w_G:w_B，是沿亮度权重方向的一次抬高；谁都能用一条
// 约束式加一条最小范数条件把它复算出来。
//
// 注意 k=(1,1,1) 同样满足守恒（因 Σw=1），那正是旧的「三通道同值」
// 灰度 BMP，零误差但三通道没有区别；最小范数解才让三个通道真正分开，
// 代价是 G 的系数 k_G>1，高灰度档会撞上 255 截断。
//
// 全部推导、误差上界的来源与整数复算步骤见 docs/expand.md，
// 调用方也可直接用 Coefficients 返回的分子分母复算。
package expand

import "math"

// 亮度权重取 Rec.601 的千分值，三者之和恰为 1000：
//
//	Y = (299·R + 587·G + 114·B) / 1000
//
// 用整数千分值而不是浮点权重，是为了让守恒等式可以精确复算。
const (
	LumaR = 299
	LumaG = 587
	LumaB = 114

	LumaSum   = LumaR + LumaG + LumaB                   // 1000
	LumaNorm2 = LumaR*LumaR + LumaG*LumaG + LumaB*LumaB // 446966
)

// 展开系数的分子：k_c = Num_c / LumaNorm2。
const (
	NumR = LumaSum * LumaR // 299000
	NumG = LumaSum * LumaG // 587000
	NumB = LumaSum * LumaB // 114000
)

// MaxBound 是全 8 位灰度域上往返误差的上界，等于 Bound(255)。
const MaxBound = 47

// UnclippedMax 是不触发任何通道截断的最大灰度。
// g > UnclippedMax 时 G 通道的 g·k_G 超过 255（255/k_G = 194.16…）。
const UnclippedMax = 194

// Coefficient 是一个通道的展开系数 k = Num/Den。
type Coefficient struct {
	Channel string
	Num     int
	Den     int
}

// Coefficients 返回三通道展开系数，按 R、G、B 排列：
//
//	k_c = w_c / Σ_j w_j² = (Luma_c/1000) / (LumaNorm2/10^6)
//	    = 1000·Luma_c / LumaNorm2
//
// 这组系数的两个来源，都可独立复算：
//
//   - 亮度守恒：Σ_c (Luma_c/1000)·k_c = 1；
//   - 最小范数：在满足上式的所有 k 中，‖k‖² 最小。这是一个线性约束的
//     最小范数问题，解唯一，即 k = w/‖w‖²。（k=(1,1,1) 也守恒，因为
//     Σw=1，但它的范数更大，且三通道相同。）
//
// 因为 w 的分量都是正数且和为 1，k_c > 0；又因为 ‖w‖² < 1，
// 有的 k_c 会大于 1（本例是 G），这就是高灰度档截断的来源。
func Coefficients() [3]Coefficient {
	return [3]Coefficient{
		{Channel: "r", Num: NumR, Den: LumaNorm2},
		{Channel: "g", Num: NumG, Den: LumaNorm2},
		{Channel: "b", Num: NumB, Den: LumaNorm2},
	}
}

// CoefficientsFloat 返回同一组系数的浮点值，仅供展示，不参与计算。
func CoefficientsFloat() (r, g, b float64) {
	return float64(NumR) / LumaNorm2, float64(NumG) / LumaNorm2, float64(NumB) / LumaNorm2
}

// roundDiv 返回 n/d 的四舍五入值（半数向上），要求 d > 0、n ≥ 0。
// 写成 (2n+d)/(2d) 而不是先转浮点，保证任何平台结果一致。
func roundDiv(n, d int) int {
	return (2*n + d) / (2 * d)
}

// Expand 把一级灰度 g 展开成三通道：
//
//	q_c = round(g·k_c)，再截断到 [0,255]。
//
// 返回值 clipped 的位 0/1/2 分别记录 R/G/B 是否发生了截断。
func Expand(g uint8) (r, gg, b uint8, clipped uint8) {
	v := int(g)
	q := [3]int{
		roundDiv(v*NumR, LumaNorm2),
		roundDiv(v*NumG, LumaNorm2),
		roundDiv(v*NumB, LumaNorm2),
	}
	for i, x := range q {
		if x > 255 {
			q[i] = 255
			clipped |= 1 << uint(i)
		}
	}
	return uint8(q[0]), uint8(q[1]), uint8(q[2]), clipped
}

// Collapse 按亮度权重把三通道收回成灰度：
//
//	ĝ = round((299·R + 587·G + 114·B) / 1000)
//
// 对 R=G=B 的旧 BMP，Collapse 是恒等映射，所以旧字节流不会因解码口径
// 变化而变样。
func Collapse(r, g, b uint8) uint8 {
	s := LumaR*int(r) + LumaG*int(g) + LumaB*int(b)
	return uint8((s + LumaSum/2) / LumaSum)
}

// Bound 返回灰度 g 经 Expand→Collapse 后的往返误差上界，单位：级。
//
// 推导（docs/expand.md 有逐项说明）：
//
//	展开舍入    q_c = g·k_c + e_c，|e_c| ≤ 1/2
//	通道截断    q_c = 255，误差 −d_c，其中 d_c = max(0, g·k_c − 255)
//	收回舍入    ĝ = round(g + Σ_c w_c·e_c)，再添 ±1/2
//	合成        |e| ≤ 1/2 + Σ_c w_c·max(1/2, d_c)
//
// 本函数用整数精确算 floor(上式)。对 8 位域全枚举验证：逐级都不越界，
// 且 g=255 处取到 47，所以既不可收紧也不能再放宽。
func Bound(g uint8) int {
	v := int64(g)
	luma := [3]int64{LumaR, LumaG, LumaB}
	num := [3]int64{NumR, NumG, NumB}

	// 通分到 2·LumaSum·LumaNorm2。
	//   最外层 1/2            → LumaSum·LumaNorm2
	//   每通道 w_c·1/2        → luma_c·LumaNorm2
	//   每通道 w_c·d_c        → luma_c·(t−255·LumaNorm2)
	// 每通道取 max(1/2, d_c)，故截断分支也要与 1/2 取大。
	var numerator int64 = LumaSum * LumaNorm2
	var unclipped, clipped int64
	for i := range luma {
		t := v * num[i] // g·k_c，分母 LumaNorm2
		over := t - 255*LumaNorm2
		if over > 0 {
			if over > LumaNorm2 {
				clipped += 2 * luma[i] * over
			} else {
				clipped += 2 * luma[i] * LumaNorm2
			}
		} else {
			unclipped += luma[i]
		}
	}
	numerator += LumaNorm2*unclipped + clipped
	return int(numerator / (2 * LumaSum * LumaNorm2))
}

// Tier 是一档灰度的验证区间与档内误差上界。
type Tier struct {
	Lo, Hi uint8
	Bound  int
}

// Tiers 把 8 位灰度均分为三档：0–85、86–170、171–255，并给出档内
// 逐级 Bound 的最大值。第三档的 47 由 G 通道截断贡献，见 docs/expand.md。
func Tiers() [3]Tier {
	bounds := [3][2]uint8{{0, 85}, {86, 170}, {171, 255}}
	var out [3]Tier
	for i, r := range bounds {
		m := 0
		for g := int(r[0]); g <= int(r[1]); g++ {
			if b := Bound(uint8(g)); b > m {
				m = b
			}
		}
		out[i] = Tier{Lo: r[0], Hi: r[1], Bound: m}
	}
	return out
}

// Entropy 按符号出现次数返回经验熵，单位：比特/符号。
// 调用方传入像素样本时，该单位即比特每像素（bit per pixel）：
//
//	H = −Σ_i (n_i/N)·log2(n_i/N)
//
// 只统计出现过的符号；N=0 时返回 0。
func Entropy(counts []int) float64 {
	n := 0
	for _, c := range counts {
		if c < 0 {
			c = 0
		}
		n += c
	}
	if n == 0 {
		return 0
	}
	var h float64
	for _, c := range counts {
		if c <= 0 {
			continue
		}
		p := float64(c) / float64(n)
		h -= p * math.Log2(p)
	}
	return h
}

// EntropySamples 统计 0..255 各符号出现次数后求熵，单位：比特每像素。
func EntropySamples(samples []uint8) float64 {
	var hist [256]int
	for _, v := range samples {
		hist[v]++
	}
	return Entropy(hist[:])
}
