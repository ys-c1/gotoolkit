// Package deblock 实现块压缩（按块取 DC 量化电平）、块边界量化误差统计、
// 边界滤波以及“边界跳变 + 细节损失”闭式最优强度。
//
// 本包是内部实现，对外入口见 gotoolkit/api。
package deblock

import "math"

// Gray 是一张灰度图的最小描述。
type Gray struct {
	W, H int
	Pix  []uint8
}

func clamp8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(math.Round(v))
}

// SampleImage 生成一张确定性的、兼具缓变梯度与细尺度硬细节的实验图（128x128，
// 可被三档块长 4/8/16 整除）。缓坡在粗块 DC 重建下产生随 B 增大的“台阶”，
// 两个 4 像素栅格条纹区提供细块能保留、粗块会抹平的真实细节。
func SampleImage(w, h int) *Gray {
	const cov = 32
	pix := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := 128 + 1.2*float64(x-w/2) + 0.30*float64(y-h/2)
			// 左上角：竖直 4px 条纹（跨竖直块边界）。
			if x < cov && y < cov {
				if (x/4)%2 == 0 {
					v += 40
				} else {
					v -= 40
				}
			}
			// 右下角：水平 4px 条纹（跨水平块边界）。
			if x >= w-cov && y >= h-cov {
				if (y/4)%2 == 0 {
					v += 32
				} else {
					v -= 32
				}
			}
			// 两处低频柔光，让画面更接近自然图的平滑区，不显著改变边界二阶矩。
			dx := float64(x-88) / 70.0
			dy := float64(y-30) / 70.0
			v += 16 * math.Exp(-(dx*dx+dy*dy)*5)
			dx2 := float64(x-30) / 70.0
			dy2 := float64(y-96) / 70.0
			v += 14 * math.Exp(-(dx2*dx2+dy2*dy2)*6)
			pix[y*w+x] = clamp8(v)
		}
	}
	return &Gray{W: w, H: h, Pix: pix}
}

// DefaultQ 是实验统一使用的 DC 量化步长。
const DefaultQ = 16

// BlockSizes 是题目要求的三档块长。
var BlockSizes = []int{4, 8, 16}

// FixedStrength 是对照组使用的单一固定强度（对称半混合）。
const FixedStrength = 0.5

// BlockCompress 做最朴素的“块 DC 电平量化”压缩：
// 每个 BxB 块取均值，按量化步长 q 四舍五入到 q 的整数倍，整块重建为该电平。
// 这会在块网格上产生清晰可见的台阶。q<=1 时退化为只取块均值（仍有台阶）。
func BlockCompress(g *Gray, B, q int) *Gray {
	out := make([]uint8, len(g.Pix))
	for by := 0; by < g.H; by += B {
		for bx := 0; bx < g.W; bx += B {
			x1 := min(bx+B, g.W)
			y1 := min(by+B, g.H)
			var sum int
			n := 0
			for y := by; y < y1; y++ {
				for x := bx; x < x1; x++ {
					sum += int(g.Pix[y*g.W+x])
					n++
				}
			}
			mean := float64(sum) / float64(n)
			level := mean
			if q > 1 {
				level = math.Round(mean/float64(q)) * float64(q)
			} else {
				level = math.Round(mean)
			}
			v := clamp8(level)
			for y := by; y < y1; y++ {
				for x := bx; x < x1; x++ {
					out[y*g.W+x] = v
				}
			}
		}
	}
	return &Gray{W: g.W, H: g.H, Pix: out}
}

// Stats 是块边界差值样本的二阶统计。
//
// 对每条块边界像素对，记：
//
//	s = 原图跨边界差（真实细节）
//	g = 压缩图跨边界差（可见跳变）
//	n = g - s（块压缩在边界引入的量化/台阶误差）
//
// 所有 V 均为样本均方（除以样本数，不是无偏方差），与代价定义保持一致。
type Stats struct {
	B          int
	Pairs      int     // 边界像素对总数
	Vs         float64 // E[s^2] 真实细节能量
	Vn         float64 // E[n^2] 边界量化误差能量
	Vg         float64 // E[g^2] 压缩跳变总能量
	MeanN      float64 // E[n]，理想零均值
	Q          int
	TheoryVnQ  float64 // 纯 DC 量化残差之差的理论二阶矩 q^2/6
	AlphaStar  float64 // 闭式最优强度 Vn/(Vn+Vs)
	AlphaFixed float64 // 对照用固定强度
}

// BoundaryStats 统计横、纵两个方向所有“内部块边界”像素对。
func BoundaryStats(orig, comp *Gray, B, q int) Stats {
	var ss, nn, gg, nsum float64
	var cnt int

	consider := func(po, qo, pc, qc uint8) {
		s := float64(qo) - float64(po)
		g := float64(qc) - float64(pc)
		n := g - s
		ss += s * s
		nn += n * n
		gg += g * g
		nsum += n
		cnt++
	}

	// 竖直块边界：x = kB-1 | kB，遍历每个 y。
	for bx := B; bx < orig.W; bx += B {
		for y := 0; y < orig.H; y++ {
			consider(orig.Pix[y*orig.W+bx-1], orig.Pix[y*orig.W+bx],
				comp.Pix[y*comp.W+bx-1], comp.Pix[y*comp.W+bx])
		}
	}
	// 水平块边界：y = kB-1 | kB，遍历每个 x。
	for by := B; by < orig.H; by += B {
		for x := 0; x < orig.W; x++ {
			consider(orig.Pix[(by-1)*orig.W+x], orig.Pix[by*orig.W+x],
				comp.Pix[(by-1)*comp.W+x], comp.Pix[by*comp.W+x])
		}
	}

	st := Stats{B: B, Pairs: cnt, Q: q}
	if cnt > 0 {
		c := float64(cnt)
		st.Vs = ss / c
		st.Vn = nn / c
		st.Vg = gg / c
		st.MeanN = nsum / c
	}
	if q > 0 {
		st.TheoryVnQ = float64(q*q) / 6.0
	}
	st.AlphaStar = OptimalAlpha(st.Vn, st.Vs)
	return st
}

// OptimalAlpha 是闭式最优滤波强度：
//
//	alpha* = Vn / (Vn + Vs)
//
// 其中 Vn=E[n^2] 为块边界量化误差二阶矩，Vs=E[s^2] 为真实跨边界细节二阶矩。
// 推导：代价 C(a)=Vn(1-a)^2 + Vs a^2，令 C'(a)=0 即得。
func OptimalAlpha(vn, vs float64) float64 {
	d := vn + vs
	if d <= 0 {
		return 0
	}
	a := vn / d
	return math.Min(1, math.Max(0, a))
}

// BoundaryJump 指标 J(a)=Vn(1-a)^2：残留的边界量化跳变能量（每边界对）。
func BoundaryJump(st Stats, a float64) float64 {
	d := 1 - clamp01(a)
	return st.Vn * d * d
}

// DetailLoss 指标 D(a)=Vs a^2：被滤波抹掉的真实跨边界细节能量（每边界对）。
func DetailLoss(st Stats, a float64) float64 {
	a = clamp01(a)
	return st.Vs * a * a
}

// Cost = J + D。
func Cost(st Stats, a float64) float64 {
	return BoundaryJump(st, a) + DetailLoss(st, a)
}

func clamp01(a float64) float64 { return math.Min(1, math.Max(0, a)) }

// GridOptimal 在 [0,1] 上密集搜索经验最优强度，用来核对闭式解。
func GridOptimal(st Stats, steps int) (aBest, cBest float64) {
	if steps < 2 {
		steps = 1000
	}
	cBest = math.Inf(1)
	for i := 0; i <= steps; i++ {
		a := float64(i) / float64(steps)
		c := Cost(st, a)
		if c < cBest {
			cBest, aBest = c, a
		}
	}
	return aBest, cBest
}

// Deblock 对块边界两侧像素做对称差分混合：
//
//	L' = L + a/2 (R-L), R' = R - a/2 (R-L)
//
// 从而边界差被缩放为 (1-a)(R-L)，且像素对均值不变。
// a==0 时原样返回（滤波关闭，产物逐字节不变）。
// 横、纵两遍，修正量累加后一次性取整，保证确定性。
func Deblock(g *Gray, B int, a float64) *Gray {
	if a == 0 {
		// 显式拷贝，语义为“产物不变”，同时避免调用方误以为会改入参。
		out := make([]uint8, len(g.Pix))
		copy(out, g.Pix)
		return &Gray{W: g.W, H: g.H, Pix: out}
	}
	a = clamp01(a)
	corr := make([]float64, len(g.Pix))

	addPair := func(iL, iR int) {
		delta := (a / 2.0) * (float64(g.Pix[iR]) - float64(g.Pix[iL]))
		corr[iL] += delta
		corr[iR] -= delta
	}
	for bx := B; bx < g.W; bx += B {
		for y := 0; y < g.H; y++ {
			addPair(y*g.W+bx-1, y*g.W+bx)
		}
	}
	for by := B; by < g.H; by += B {
		for x := 0; x < g.W; x++ {
			addPair((by-1)*g.W+x, by*g.W+x)
		}
	}
	out := make([]uint8, len(g.Pix))
	for i, v := range g.Pix {
		out[i] = clamp8(float64(v) + corr[i])
	}
	return &Gray{W: g.W, H: g.H, Pix: out}
}

// RawBoundaryJump 直接从（可能已滤波的）光栅图像测量块边界差的均方能量，
// 用于核对模型预测 J 与实际可见跳变是否同向下降。
func RawBoundaryJump(g *Gray, B int) float64 {
	var sum float64
	var cnt int
	for bx := B; bx < g.W; bx += B {
		for y := 0; y < g.H; y++ {
			d := float64(g.Pix[y*g.W+bx]) - float64(g.Pix[y*g.W+bx-1])
			sum += d * d
			cnt++
		}
	}
	for by := B; by < g.H; by += B {
		for x := 0; x < g.W; x++ {
			d := float64(g.Pix[by*g.W+x]) - float64(g.Pix[(by-1)*g.W+x])
			sum += d * d
			cnt++
		}
	}
	if cnt == 0 {
		return 0
	}
	return sum / float64(cnt)
}

// MSE 计算两张同尺寸图的均方误差。
func MSE(a, b *Gray) float64 {
	var s float64
	for i := range a.Pix {
		d := float64(a.Pix[i]) - float64(b.Pix[i])
		s += d * d
	}
	return s / float64(len(a.Pix))
}
