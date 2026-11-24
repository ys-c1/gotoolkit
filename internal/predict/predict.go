// Package predict 灰度图二维线性预测的分析实现。
//
// 取左(W)、上(N)、左上(NW) 三个邻域方向做线性预测:
//
//	p̂ = a·W + b·N + c·NW,  约束 a+b+c = 1
//
// 系数由约束最小二乘解出(SolveConstrained); 另提供按局部梯度方向
// 反比加权的分块融合预测(ResidualAdaptive)。所有统计只在内点
// (i>=1, j>=1) 上进行, 保证三种预测器在同一样本集上可比。
package predict

import "math"

// Coeffs 三方向预测系数, 满足 A+B+C=1, 依次对应 W、N、NW。
type Coeffs struct{ A, B, C float64 }

// Weights 单块的融合权重, 满足 W+N+NW=1。
type Weights struct{ W, N, NW float64 }

// SolveConstrained 解约束最小二乘
//
//	min Σ (x - a·W - b·N - c·NW)²   s.t. a+b+c = 1
//
// 代入 a = 1-b-c 后等价于无约束的二元问题
//
//	min Σ ((x-W) - b·(N-W) - c·(NW-W))²
//
// 即把"当前像素减左邻"回归到两个邻域差分上, 解 2x2 法方程即可。
// 约束平面包含一维预测器 (a,b,c)=(1,0,0), 故其残差能量不会超过一维基线。
func SolveConstrained(w, h int, pix []uint8) Coeffs {
	var s11, s12, s22, t1, t2 float64
	for i := 1; i < h; i++ {
		for j := 1; j < w; j++ {
			x := float64(pix[i*w+j])
			wv := float64(pix[i*w+j-1])
			d1 := float64(pix[(i-1)*w+j]) - wv   // N-W
			d2 := float64(pix[(i-1)*w+j-1]) - wv // NW-W
			e := x - wv
			s11 += d1 * d1
			s12 += d1 * d2
			s22 += d2 * d2
			t1 += e * d1
			t2 += e * d2
		}
	}
	det := s11*s22 - s12*s12
	if det == 0 {
		return Coeffs{1, 0, 0}
	}
	b := (t1*s22 - t2*s12) / det
	c := (s11*t2 - s12*t1) / det
	return Coeffs{1 - b - c, b, c}
}

// Residual1D 一维预测(左邻复制)的内点残差, 行主序。
func Residual1D(w, h int, pix []uint8) []int {
	res := make([]int, 0, (w-1)*(h-1))
	for i := 1; i < h; i++ {
		for j := 1; j < w; j++ {
			res = append(res, int(pix[i*w+j])-int(pix[i*w+j-1]))
		}
	}
	return res
}

// Residual2D 用给定系数做三方向线性预测的内点残差(四舍五入到整数)。
func Residual2D(w, h int, pix []uint8, co Coeffs) []int {
	res := make([]int, 0, (w-1)*(h-1))
	for i := 1; i < h; i++ {
		for j := 1; j < w; j++ {
			p := co.A*float64(pix[i*w+j-1]) +
				co.B*float64(pix[(i-1)*w+j]) +
				co.C*float64(pix[(i-1)*w+j-1])
			res = append(res, int(math.Round(float64(pix[i*w+j])-p)))
		}
	}
	return res
}

// ResidualAdaptive 按 block×block 分块, 每块用方向梯度反比加权融合:
//
//	w_k = (ḡ_k + ε)⁻¹ / Σ_j (ḡ_j + ε)⁻¹,  k,j ∈ {W, N, NW}
//
// 其中 ḡ_k 是块内沿方向 k 的平均绝对差分, ε 防止除零并压制噪声块。
// 返回内点残差与每块权重(块按行主序)。
func ResidualAdaptive(w, h int, pix []uint8, block int) ([]int, []Weights) {
	if block < 2 {
		block = 2
	}
	nbw := (w + block - 1) / block
	nbh := (h + block - 1) / block
	ws := make([]Weights, nbw*nbh)
	for bi := 0; bi < nbh; bi++ {
		for bj := 0; bj < nbw; bj++ {
			ws[bi*nbw+bj] = blockWeights(w, h, pix, block, bi, bj)
		}
	}
	res := make([]int, 0, (w-1)*(h-1))
	for i := 1; i < h; i++ {
		for j := 1; j < w; j++ {
			wgt := ws[(i/block)*nbw+(j/block)]
			p := wgt.W*float64(pix[i*w+j-1]) +
				wgt.N*float64(pix[(i-1)*w+j]) +
				wgt.NW*float64(pix[(i-1)*w+j-1])
			res = append(res, int(math.Round(float64(pix[i*w+j])-p)))
		}
	}
	return res, ws
}

// blockWeights 计算单块的融合权重。
func blockWeights(w, h int, pix []uint8, block, bi, bj int) Weights {
	const eps = 1.0 // 灰度级
	var gw, gn, gnw float64
	var cw, cn, cnw int
	i0, i1 := bi*block, min((bi+1)*block, h)
	j0, j1 := bj*block, min((bj+1)*block, w)
	for i := i0; i < i1; i++ {
		for j := j0; j < j1; j++ {
			x := float64(pix[i*w+j])
			if j >= 1 {
				gw += math.Abs(x - float64(pix[i*w+j-1]))
				cw++
			}
			if i >= 1 {
				gn += math.Abs(x - float64(pix[(i-1)*w+j]))
				cn++
			}
			if i >= 1 && j >= 1 {
				gnw += math.Abs(x - float64(pix[(i-1)*w+j-1]))
				cnw++
			}
		}
	}
	gw, gn, gnw = gw/float64(max(cw, 1)), gn/float64(max(cn, 1)), gnw/float64(max(cnw, 1))
	iw, in, inw := 1/(gw+eps), 1/(gn+eps), 1/(gnw+eps)
	s := iw + in + inw
	return Weights{iw / s, in / s, inw / s}
}

// Entropy 残差按 mod 256 归符号后的经验熵, 单位 bit/pixel。
func Entropy(res []int) float64 {
	if len(res) == 0 {
		return 0
	}
	var cnt [256]int
	for _, r := range res {
		cnt[((r%256)+256)%256]++
	}
	var h float64
	n := float64(len(res))
	for _, c := range cnt {
		if c == 0 {
			continue
		}
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}

// Mean 残差的有符号均值。
func Mean(res []int) float64 {
	if len(res) == 0 {
		return 0
	}
	var s float64
	for _, r := range res {
		s += float64(r)
	}
	return s / float64(len(res))
}

// Orthogonality 返回约束最小二乘残差与三个预测子的归一化内积
// ⟨r,W⟩、⟨r,N⟩、⟨r,NW⟩(各除以样本数)。
//
// KKT 条件: 拉格朗日函数对 a_k 求导得 -2⟨u_k, r⟩ + λ = 0,
// 故三个内积必须相等(共同值即 λ/2)。等价地, 残差正交于差分方向:
// ⟨r, N-W⟩ = ⟨r, NW-W⟩ = 0。三者相等即验证了约束最优性。
func Orthogonality(w, h int, pix []uint8, co Coeffs) (iw, in, inw float64) {
	var sw, sn, snw float64
	var n int
	for i := 1; i < h; i++ {
		for j := 1; j < w; j++ {
			wv := float64(pix[i*w+j-1])
			nv := float64(pix[(i-1)*w+j])
			nwv := float64(pix[(i-1)*w+j-1])
			r := float64(pix[i*w+j]) - (co.A*wv + co.B*nv + co.C*nwv)
			sw += r * wv
			sn += r * nv
			snw += r * nwv
			n++
		}
	}
	return sw / float64(n), sn / float64(n), snw / float64(n)
}
