package chroma

import (
	"errors"
	"math"
)

// 本文件实现 4:2:0 色度上采样重建。
//
// 设待重建像素的亮度为 Y，半分辨率色度样本位于各 2x2 块中心，
// 参与插值的四个相邻色度样本记为 C_k（k=1..4），其对应块中心亮度为 μ_k。
//
// 【权重推导（色度-亮度相关性，两行）】
//   由色度与亮度局部相关，重建误差 E[ (Σ w_k C_k − C)^2 ] 在 Σw_k=1 下对 w_k 求偏导置零，
//   得相似性 s_k=exp(−(Y−μ_k)²/(2σ²))（加下限 s_min）；最终权重取几何双线性与相似度的凸混合 w_k=(1−α)g_k+α·ĝ_k，ĝ_k=g_k s_k/Σ_j g_j s_j。
//
// 【局部方差调整（两行）】
//   σ² 取以目标像素为中心的 3x3 亮度窗方差 σ² = (1/9)Σ(Y_j−Ȳ)²，加 ε=1 防平坦区除零；
//   平坦区 σ²→0 时各 s_k 趋同、ĝ_k→g_k 权重退回双线性；边缘处 σ² 大、跨边缘邻居 (Y−μ_k)² 大被指数压低，权重集中到同侧边样本。

// adaptiveGamma 控制相似度衰减强度（对方差归一化距离的敏感度），取 1 为经验常数。
const adaptiveGamma = 1.0

// adaptiveMix 是相似度权重在凸混合中的占比 α；保留 (1−α) 的纯几何权重作为下限，
// 防止强边缘角点上单个邻居权重过大造成过冲（最大色差反弹）。
const adaptiveMix = 0.85

// similarityFloor 是相似度 s_k 的下限，保证跨边缘邻居也保有最低权重，抑制多向边缘过冲。
const similarityFloor = 0.2

// 防止平坦区除零的方差下限（亮度量化单位的平方）。
const varianceEps = 1.0

// Reconstruct 按指定模式把半分辨率色度重建到全分辨率，返回重建后的 RGB。
func Reconstruct(c *YCbCr, mode string) (*RGB, error) {
	w, h := c.W, c.H
	cbFull := &Plane{W: w, H: h, V: make([]float64, w*h)}
	crFull := &Plane{W: w, H: h, V: make([]float64, w*h)}
	switch mode {
	case ModeBilinear:
		upsampleBilinear(c, c.Cb, cbFull)
		upsampleBilinear(c, c.Cr, crFull)
	case ModeAdaptive:
		upsampleAdaptive(c, c.Cb, cbFull)
		upsampleAdaptive(c, c.Cr, crFull)
	default:
		return nil, errors.New("chroma: 未知重建模式 " + mode)
	}
	return YCbCrToRGB(c.Y, cbFull, crFull), nil
}

// 双线性权重：目标全分辨率像素 (x,y) 映射到色度网格的连续坐标。
// 色度样本 k 位于块中心 (2cx+0.5, 2cy+0.5)，故 fx=(x−0.5)/2, fy=(y−0.5)/2。
func bilinearStencil(w, h, cw, ch, x, y int) (idx [4]int, g [4]float64) {
	fx := (float64(x) - 0.5) / 2.0
	fy := (float64(y) - 0.5) / 2.0
	cx0 := int(fx)
	cy0 := int(fy)
	tx := fx - float64(cx0)
	ty := fy - float64(cy0)
	cx1, cy1 := cx0+1, cy0+1
	// 边界外推：钳制到合法色度样本下标。
	if cx0 < 0 {
		cx0, cx1 = 0, 0
		tx = 0
	}
	if cy0 < 0 {
		cy0, cy1 = 0, 0
		ty = 0
	}
	if cx1 >= cw {
		cx0, cx1 = cw-1, cw-1
		tx = 0
	}
	if cy1 >= ch {
		cy0, cy1 = ch-1, ch-1
		ty = 0
	}
	idx = [4]int{
		cy0*cw + cx0, cy0*cw + cx1,
		cy1*cw + cx0, cy1*cw + cx1,
	}
	g = [4]float64{
		(1 - tx) * (1 - ty), tx * (1 - ty),
		(1 - tx) * ty, tx * ty,
	}
	return idx, g
}

func upsampleBilinear(c *YCbCr, src Plane, dst *Plane) {
	w, h := c.W, c.H
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			idx, g := bilinearStencil(w, h, c.CW, c.CH, x, y)
			var v float64
			for k := 0; k < 4; k++ {
				v += g[k] * src.V[idx[k]]
			}
			dst.V[y*w+x] = v
		}
	}
}

// blockMeanLuma 计算色度样本 k 所在 2x2 块的平均亮度 μ_k。
func blockMeanLuma(c *YCbCr, sampleIndex int) float64 {
	cx := sampleIndex % c.CW
	cy := sampleIndex / c.CW
	var s float64
	for dy := 0; dy < 2; dy++ {
		for dx := 0; dx < 2; dx++ {
			s += float64(c.Y[(2*cy+dy)*c.W+(2*cx+dx)])
		}
	}
	return s / 4
}

// localLumaVariance 返回以 (x,y) 为中心 3x3 亮度窗的方差 σ²（边界窗截断）。
func localLumaVariance(c *YCbCr, x, y int) float64 {
	var sum, sum2 float64
	n := 0
	for dy := -1; dy <= 1; dy++ {
		jy := y + dy
		if jy < 0 || jy >= c.H {
			continue
		}
		for dx := -1; dx <= 1; dx++ {
			jx := x + dx
			if jx < 0 || jx >= c.W {
				continue
			}
			v := float64(c.Y[jy*c.W+jx])
			sum += v
			sum2 += v * v
			n++
		}
	}
	mean := sum / float64(n)
	return sum2/float64(n) - mean*mean
}

func upsampleAdaptive(c *YCbCr, src Plane, dst *Plane) {
	w, h := c.W, c.H
	mu := make([]float64, c.CW*c.CH)
	for k := range mu {
		mu[k] = blockMeanLuma(c, k)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			idx, g := bilinearStencil(w, h, c.CW, c.CH, x, y)
			sigma2 := localLumaVariance(c, x, y) + varianceEps
			target := float64(c.Y[y*w+x])

			// 先算相似度归一化权重 ĝ_k = g_k·s_k / Σ g_j·s_j。
			// s_min 给相似度设下限：任何邻居都不被完全压制，约束多向边缘交汇处的过冲。
			var sim [4]float64
			var simSum float64
			for k := 0; k < 4; k++ {
				d := target - mu[idx[k]]
				// 方差归一化的相似性：σ² 大（边缘）时同侧边样本主导，σ² 小（平坦）时退化为双线性。
				s := similarityFloor + (1-similarityFloor)*math.Exp(-adaptiveGamma*d*d/(2*sigma2))
				sim[k] = g[k] * s
				simSum += sim[k]
			}
			// 凸混合：(1−α) 纯几何 + α 相似度，几何下限抑制角点过冲。
			var vsum float64
			for k := 0; k < 4; k++ {
				ghat := sim[k] / simSum
				wk := (1-adaptiveMix)*g[k] + adaptiveMix*ghat
				vsum += wk * src.V[idx[k]]
			}
			dst.V[y*w+x] = vsum
		}
	}
}
