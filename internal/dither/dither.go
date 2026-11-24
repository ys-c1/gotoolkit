// Package dither 实现有序抖动（ordered dithering）的阈值矩阵与二值化。
//
// 三种 8×8 矩阵（等级均为 0..63 的排列）：
//   - MatrixBayer：经典 Bayer 递归矩阵，完全确定性，无需随机数；
//   - MatrixVoidCluster：Ulichney void-and-cluster 蓝噪声矩阵，
//     周期边界高斯核（σ=1.5），LCG 种子 SeedVAC 固定可复现；
//   - MatrixSpectral：以 void-and-cluster 矩阵为初值，按矩阵傅里叶
//     系数的低频泄漏做贪心交换得到，种子 SeedSwap 固定可复现。
package dither

import "math"

// Size 是所有矩阵的边长（8×8）。
const Size = 8

// 固定随机种子，保证两种含随机构造的矩阵逐位可复现。
const (
	SeedVAC  = 20261125
	SeedSwap = 20261125
)

// 构造参数，写在此处而非隐式常量，便于测试与文档引用。
const (
	sigmaVAC    = 1.5 // void-and-cluster 高斯核标准差（像素）
	swapPasses  = 4   // 频谱交换的最大扫描轮数（无改进即提前停止）
	halfDensity = Size * Size / 2
	// tieEps 判定两个高斯响应并列的容差：响应量级约 1..12，
	// 跨语言浮点求和误差约 1e-14，取 1e-9 远在真实差异（>=1e-3）之下。
	tieEps = 1e-9
	// swapEps 是频谱交换的接受阈值。最小真实改进约 8e-2，
	// 而朴素 DFT 与 FFT 的跨实现舍入差约 1e-13；取 1e-6 同时
	// 远离两者，保证不同浮点路径做出完全相同的交换决策。
	swapEps = 1e-6
)

// 矩阵名 -> 构造函数的选择器，供 api 层使用。
const (
	NameBayer       = "bayer"
	NameVoidCluster = "void-cluster"
	NameSpectral    = "spectral"
)

// LCG：与分析脚本 analysis/dither_prototype.py 共用同一组参数的
// 线性同余生成器（glibc 风格），保证 Go 与 Python 构造结果一致。
type LCG struct {
	state uint32
}

const (
	lcgA    = 1103515245
	lcgC    = 12345
	lcgM    = 1 << 31
	lcgMask = lcgM - 1
)

// NewLCG 以 seed 初始化（取低 31 位）。
func NewLCG(seed uint32) *LCG { return &LCG{state: seed & lcgMask} }

// Rand 返回 [0,1) 的伪随机浮点。
func (r *LCG) Rand() float64 {
	r.state = (lcgA*r.state + lcgC) & lcgMask
	return float64(r.state) / float64(lcgM)
}

// Matrix 返回 8×8、元素为 0..63 的阈值矩阵副本（调用方可自由修改）。
func Matrix(name string) ([Size * Size]int, bool) {
	switch name {
	case NameBayer:
		return bayerMatrix(), true
	case NameVoidCluster:
		return voidClusterMatrix(NewLCG(SeedVAC), sigmaVAC), true
	case NameSpectral:
		return spectralMatrix(), true
	default:
		return [Size * Size]int{}, false
	}
}

// bayerMatrix 构造经典递归 Bayer 矩阵：B1=[[0,2],[3,1]]，
// 每放大一级 B_{k+1}=np.block([[4B,4B+2],[4B+3,4B+1]])，直至 8×8。
// 即四个 n×n 象限块：左上 4B、右上 4B+2、左下 4B+3、右下 4B+1。
func bayerMatrix() [Size * Size]int {
	cur := []int{0, 2, 3, 1} // B1 的行主序扁平化
	n := 2
	for n < Size {
		w := 2 * n
		next := make([]int, w*w)
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				v := cur[y*n+x]
				next[y*w+x] = 4 * v           // 左上块
				next[y*w+(x+n)] = 4*v + 2     // 右上块
				next[(y+n)*w+x] = 4*v + 3     // 左下块
				next[(y+n)*w+(x+n)] = 4*v + 1 // 右下块
			}
		}
		cur = next
		n = w
	}
	var out [Size * Size]int
	copy(out[:], cur)
	return out
}

// gaussianKernel 构造周期边界的 8×8 高斯核，中心格置 0（不含自身）。
// 距离按到中心 (4,4) 的环绕最短距离计算。
func gaussianKernel(sigma float64) [Size * Size]float64 {
	var g [Size * Size]float64
	half := Size / 2
	inv := -1.0 / (2.0 * sigma * sigma)
	for y := 0; y < Size; y++ {
		for x := 0; x < Size; x++ {
			if x == half && y == half {
				continue // 自身权重为 0
			}
			dx := x - half
			dy := y - half
			g[y*Size+x] = math.Exp(float64(dx*dx+dy*dy) * inv)
		}
	}
	return g
}

// convolvePeriodic 在 8×8 周期网格上做 pattern 与核 g 的循环卷积。
// 输出 v[y,x]=Σ_{ky,kx} pattern[ky,kx]*g[(y-ky) mod 8,(x-kx) mod 8]。
func convolvePeriodic(pattern [Size * Size]int, g [Size * Size]float64) [Size * Size]float64 {
	var v [Size * Size]float64
	for y := 0; y < Size; y++ {
		for x := 0; x < Size; x++ {
			sum := 0.0
			for ky := 0; ky < Size; ky++ {
				for kx := 0; kx < Size; kx++ {
					if pattern[ky*Size+kx] == 0 {
						continue
					}
					dy := (y - ky + Size) % Size
					dx := (x - kx + Size) % Size
					sum += float64(pattern[ky*Size+kx]) * g[dy*Size+dx]
				}
			}
			v[y*Size+x] = sum
		}
	}
	return v
}

// voidClusterMatrix 实现 Ulichney void-and-cluster 阈值矩阵：
//  1. LCG+Fisher-Yates 洗牌取前 32 格作为初始 1/2 密度二值图案；
//  2. 移除阶段：反复把“最紧簇”（高斯响应最大的 1）置 0，按顺序赋等级 0..31；
//  3. 加入阶段：从初始图案出发，反复把“最大空隙”（高斯响应最小的 0，
//     等价于对补图案取最大响应）置 1，按顺序赋等级 32..63。
//
// 并列时取行优先最小下标（与 numpy argmax 的取法一致）。
func voidClusterMatrix(rng *LCG, sigma float64) [Size * Size]int {
	g := gaussianKernel(sigma)

	// 1) 洗牌决定初始 1 的位置。
	order := make([]int, Size*Size)
	for i := range order {
		order[i] = i
	}
	for i := len(order) - 1; i > 0; i-- {
		j := int(rng.Rand() * float64(i+1))
		order[i], order[j] = order[j], order[i]
	}
	var pattern [Size * Size]int
	for _, idx := range order[:halfDensity] {
		pattern[idx] = 1
	}

	var rank [Size * Size]int

	// argMax 在 eligible 格中取 v 最大者；差值小于 tieEps 视为并列，
	// 并列取行优先最小下标，与 numpy argmax 的平局语义一致。
	// 必须显式容差：不同语言/顺序的浮点求和会在数学相等的并列点上
	// 产生 ~1e-15 的舍入差，翻转选择会使整个矩阵发散。
	argMax := func(v [Size * Size]float64, eligible [Size * Size]int) int {
		best, bestVal := -1, math.Inf(-1)
		for i := 0; i < Size*Size; i++ {
			if eligible[i] == 0 {
				continue
			}
			if best == -1 || v[i] > bestVal+tieEps {
				best, bestVal = i, v[i]
			}
			// 差值在容差内时保留更小下标（i 递增，故不更新）。
		}
		return best
	}

	// 2) 移除阶段：找响应最大的 1。
	ones := pattern
	for r := 0; r < halfDensity; r++ {
		v := convolvePeriodic(ones, g)
		best := argMax(v, ones)
		ones[best] = 0
		rank[best] = r
	}

	// 3) 加入阶段：在空格中取高斯响应最大者（与 Python 原型
	// argmax(where(zeros==0, v, -inf)) 同式），按顺序赋等级 32..63。
	var eligible [Size * Size]int
	for i := range eligible {
		eligible[i] = 1 - pattern[i] // 空格为候选
	}
	zeros := pattern
	for r := halfDensity; r < Size*Size; r++ {
		v := convolvePeriodic(zeros, g)
		best := argMax(v, eligible)
		zeros[best] = 1
		eligible[best] = 0
		rank[best] = r
	}
	return rank
}

// dft 对 8×8 实数矩阵做未归一化二维 DFT（与 numpy.fft.fft2 同约定），
// 返回每个频点的 |F(v,u)|²，扁平下标按 [行频率 v][列频率 u] 存储，
// 即 p[v*Size+u] 对应 numpy 的 P[v,u]。直流位于 p[0]。
func powerSpectrum(m [Size * Size]int) [Size * Size]float64 {
	var mean float64
	for _, v := range m {
		mean += float64(v)
	}
	mean /= float64(Size * Size)

	var p [Size * Size]float64
	tau := 2.0 * math.Pi
	for v := 0; v < Size; v++ { // 行方向频率
		for u := 0; u < Size; u++ { // 列方向频率
			// 用 complex128 累加 e^{-iθ} 项，与 numpy 复数 FFT 的
			// 舍入路径对齐，保证频谱交换跨语言做出相同并列判定。
			var acc complex128
			for y := 0; y < Size; y++ {
				for x := 0; x < Size; x++ {
					ang := tau * (float64(v*y + u*x)) / float64(Size)
					a := complex(float64(m[y*Size+x])-mean, 0)
					acc += a * complex(math.Cos(ang), -math.Sin(ang))
				}
			}
			p[v*Size+u] = real(acc)*real(acc) + imag(acc)*imag(acc)
		}
	}
	return p
}

// lowFreqLeakage 计算矩阵傅里叶系数的低频泄漏。
//
// 频点选取（与分析原型一致，在 fftshift 坐标系，中心 (4,4)）：
// 半径 r≤2 的 13 个系数，去掉直流 (4,4)，对剩余 12 个 |F|² 求和。
// 12 个偏移 (du,dv) 为：
//
//	(0,±1),(±1,0),(0,±2),(±2,0),(±1,±1)
//
// powerSpectrum 的输出是未平移坐标（DC 在 [0,0]）。频点 (v,u) 的
// fftshift 偏移为 dv=(v+c) mod N-c、du=(u+c) mod N-c。
func lowFreqLeakage(p [Size * Size]float64) float64 {
	c := Size / 2
	sum := 0.0
	for v := 0; v < Size; v++ {
		dv := (v+c)%Size - c
		for u := 0; u < Size; u++ {
			du := (u+c)%Size - c
			d2 := du*du + dv*dv
			if d2 <= 4 && d2 > 0 {
				sum += p[v*Size+u]
			}
		}
	}
	return sum
}

// spectralMatrix 以 void-and-cluster 矩阵为初值，逐对交换像素位置：
// 仅当交换后低频泄漏严格下降时接受。每轮以 LCG 洗牌固定扫描顺序，
// 最多 swapPasses 轮，一整轮无改进即停止。结果仍是 0..63 的排列。
func spectralMatrix() [Size * Size]int {
	m := voidClusterMatrix(NewLCG(SeedVAC), sigmaVAC)
	rng := NewLCG(SeedSwap ^ 0x9E3779B9)

	type pair struct{ a, b int }
	var pairs []pair
	for i := 0; i < Size*Size; i++ {
		for j := i + 1; j < Size*Size; j++ {
			pairs = append(pairs, pair{i, j})
		}
	}
	best := lowFreqLeakage(powerSpectrum(m))
	for pass := 0; pass < swapPasses; pass++ {
		// Fisher-Yates 洗牌扫描顺序（与 Python 原型同一 LCG 流）。
		for i := len(pairs) - 1; i > 0; i-- {
			j := int(rng.Rand() * float64(i+1))
			pairs[i], pairs[j] = pairs[j], pairs[i]
		}
		improved := false
		for _, pr := range pairs {
			m[pr.a], m[pr.b] = m[pr.b], m[pr.a]
			e := lowFreqLeakage(powerSpectrum(m))
			if e < best-swapEps {
				best, improved = e, true
			} else {
				m[pr.a], m[pr.b] = m[pr.b], m[pr.a] // 回退
			}
		}
		if !improved {
			break
		}
	}
	return m
}

// Dither 用给定阈值矩阵对灰度图做有序抖动二值化。
// 比较规则：out=255 当且仅当 pix[y,x]*64/256 > matrix[y mod 8, x mod 8]，
// 与分析原型一致；矩阵周期平铺。
func Dither(w, h int, pix []uint8, matrix [Size * Size]int) ([]uint8, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, ErrBadSize
	}
	levels := Size * Size
	out := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		rowBase := y * w
		my := (y % Size) * Size
		for x := 0; x < w; x++ {
			scaled := int(pix[rowBase+x]) * levels / 256
			if scaled > matrix[my+(x%Size)] {
				out[rowBase+x] = 255
			}
		}
	}
	return out, nil
}
