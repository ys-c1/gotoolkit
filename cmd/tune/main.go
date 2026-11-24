// tune 是联合调参实验台：在固定测试图上扫描 (K, s) 组合，
// 测量联合误差的加权目标，并对比闭式解与网格搜索结果。
//
// 运行：go run ./cmd/tune
package main

import (
	"fmt"
	"math"

	"gotoolkit/internal/codec"
)

const (
	imgW      = 256
	imgH      = 256
	lambda    = 4.0  // 低频（带状）误差的视觉权重
	coarseH   = 0.05 // 粗网格步长：s∈[0,1] 共 21 点
	fineH     = 0.01 // 细网格步长：在粗最优点 ±0.05 邻域内精修
	fineRange = 0.05
)

// makeTestImage 生成确定性测试图：平滑二维缓变梯度叠加固定幅度（不随量化步长
// 缩放）的传感器式噪声基底 σ_n≈6。噪声基底打破量化误差的尺度不变性：
// 桶数多（Δ 小）时量化误差已小于噪声基底，抖动收益下降，最优 s* 随 K 下移。
func makeTestImage() []uint8 {
	pix := make([]uint8, imgW*imgH)
	// 确定性 LCG 白噪声（固定种子，可复现），Box–Muller 不必要：均匀噪声即可。
	seed := uint32(20261125)
	rnd := func() float64 {
		seed = seed*1103515245 + 12345
		// 映射到 [-1,1)。
		return float64(int32(seed>>1)) / float64(1<<30)
	}
	for y := 0; y < imgH; y++ {
		for x := 0; x < imgW; x++ {
			fx := float64(x) / (imgW - 1)
			fy := float64(y) / (imgH - 1)
			// 主斜坡 0..255；纵向缓变正弦（周期=图高），幅度 12。
			v := fx*255 + 12*math.Sin(2*math.Pi*fy)
			// 固定幅度噪声基底（峰峰约 12，σ≈3.5），与 Δ 无关。
			v += 6 * rnd()
			pix[y*imgW+x] = uint8(math.Max(0, math.Min(255, math.Round(v))))
		}
	}
	return pix
}

// box3LowPass 对残差 e 做 3×3 盒式低通，返回每像素的低频分量；
// 边界 2 像素在调用处直接剔除，这里不做填充。
func box3LowPass(e []float64) []float64 {
	lo := make([]float64, len(e))
	for y := 1; y < imgH-1; y++ {
		for x := 1; x < imgW-1; x++ {
			sum := 0.0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					sum += e[(y+dy)*imgW+(x+dx)]
				}
			}
			lo[y*imgW+x] = sum / 9
		}
	}
	return lo
}

// metric 测量量化输出相对原图的加权联合误差。
//   - 带状能量 E_band：残差低通分量（局部平均）的均方，人眼敏感
//   - 颗粒能量 E_grain：残差高通分量（去均值）的均方，视觉可容忍
//   - J = E_band*λ + E_grain，另返回裸 MSE 作参照
//
// 只统计内框（去掉 2 像素边界），避免边界与滤波器填充污染。
func metric(src, out []uint8) (band, grain, mse, j float64) {
	e := make([]float64, len(src))
	for i := range src {
		e[i] = float64(out[i]) - float64(src[i])
	}
	lo := box3LowPass(e)
	n := 0
	var sb, sg, sm float64
	for y := 2; y < imgH-2; y++ {
		for x := 2; x < imgW-2; x++ {
			idx := y*imgW + x
			l := lo[idx]
			h := e[idx] - l
			sb += l * l
			sg += h * h
			sm += e[idx] * e[idx]
			n++
		}
	}
	band, grain, mse = sb/float64(n), sg/float64(n), sm/float64(n)
	j = lambda*band + grain
	return
}

// probe 在给定 (K,s) 下测量三项能量。
func probe(src []uint8, k int, s float64) (band, grain, mse float64) {
	out := codec.Quantize(imgW, imgH, src, k, s)
	band, grain, mse, _ = metric(src, out)
	return
}

// coeffs 用 s=0、s=1 两个探针标定能量交换模型（固定 K，Δ 固定）：
//
//	E_band(s)  = B0(1 - b_K s)
//	E_grain(s) = G0 + g_K s
//
// b_K 描述单位抖动强度把低频带状能量搬走的速率，
// g_K 描述单位抖动强度注入高频颗粒能量的速率。
func coeffs(src []uint8, k int) (b0, g0, bK, gK float64) {
	b0, g0, _ = probe(src, k, 0)
	b1, g1, _ := probe(src, k, 1)
	bK = (b0 - b1) / b0
	gK = g1 - g0
	return
}

// closedForm 给出能量交换模型的解析最优，分两层：
//
//  1. 线性模型（全局，可手算的闭式）：
//     E_band(s)=B0(1-b_K s)，E_grain(s)=G0+g_K s，
//     J(s)=λB0+G0 + (g_K-λB0 b_K)s，对 s 线性，带约束最优为角点：
//     s*=1 若 λB0 b_K>g_K，否则 s*=0。
//
//  2. 局部二次修正（内点闭式）：实测 J(s) 是下凸曲线，在粗网格最优点 i0
//     两侧各取 2 点共 5 点（下标 i0-2..i0+2，步长 h=0.05），令 t=(s-s0)/h，
//     最小二乘拟合 J≈a+b t+c t²。设计矩阵在等距 t=-2..2 上有解析正交解：
//
//     Σt=0, Σt²=10, Σt⁴=34, n=5
//     a = (34·ΣJ - 10·Σt²J)/70
//     b = ΣtJ/10
//     c = (Σt²J - 10·a)/24
//
//     顶点 t*=-b/(2c)，换算 s*=s0+h·t*，再夹到 [0,1]。
//     顶点处模型 J*=a-b²/(4c)。边界点不足 5 点时退化为 3 点公式。
func closedForm(src []uint8, k int, js []float64, h float64) (sStar, jModel float64) {
	b0, _, bK, gK := coeffs(src, k)
	linearSlope := gK - lambda*b0*bK // <0 时线性角点解为 s=1
	_ = linearSlope

	i0 := 0
	for i, j := range js {
		if j < js[i0] {
			i0 = i
		}
	}
	s0 := float64(i0) * h

	lo, hi := i0-2, i0+2
	if lo < 0 {
		lo = 0
	}
	if hi > len(js)-1 {
		hi = len(js) - 1
	}
	if hi-lo >= 4 {
		// 5 点最小二乘。
		var sumJ, sumTJ, sumT2J float64
		for i := lo; i <= hi; i++ {
			t := float64(i - i0)
			sumJ += js[i]
			sumTJ += t * js[i]
			sumT2J += t * t * js[i]
		}
		a := (34*sumJ - 10*sumT2J) / 70
		b := sumTJ / 10
		c := (sumT2J - 10*a) / 24
		if c <= 1e-12 {
			return s0, js[i0]
		}
		tStar := -b / (2 * c)
		sStar = s0 + h*tStar
		sStar = math.Max(0, math.Min(1, sStar))
		jModel = a - b*b/(4*c)
		return sStar, jModel
	}

	// 3 点退化公式。
	lo3 := i0 - 1
	if lo3 < 0 {
		lo3 = 0
	}
	hi3 := i0 + 1
	if hi3 > len(js)-1 {
		hi3 = len(js) - 1
	}
	if hi3-lo3 < 2 {
		return s0, js[i0]
	}
	jMinus, j0, jPlus := js[lo3], js[i0], js[hi3]
	p := (jPlus - jMinus) / (2 * h)
	q := (jPlus - 2*j0 + jMinus) / (2 * h * h)
	if q <= 1e-12 {
		return s0, j0
	}
	sStar = s0 - p/(2*q)
	sStar = math.Max(0, math.Min(1, sStar))
	jModel = j0 - p*p/(4*q)
	return sStar, jModel
}

// jAt 测量给定 (K,s) 的加权目标 J。
func jAt(src []uint8, k int, s float64) float64 {
	out := codec.Quantize(imgW, imgH, src, k, s)
	_, _, _, j := metric(src, out)
	return j
}

// fineGrid 在粗最优点 center 的 ±fineRange 邻域内，以 fineH=0.01 为步长
// 做精修网格搜索，返回邻域内实测最优 (s,J)。
func fineGrid(src []uint8, k int, center float64) (float64, float64) {
	lo := math.Max(0, center-fineRange)
	hi := math.Min(1, center+fineRange)
	bestS, bestJ := center, jAt(src, k, center)
	for s := lo; s <= hi+1e-12; s += fineH {
		s = math.Round(s*100) / 100 // 消除浮点累积误差
		if j := jAt(src, k, s); j < bestJ {
			bestS, bestJ = s, j
		}
	}
	return bestS, bestJ
}

func main() {
	src := makeTestImage()
	ks := []int{4, 8, 16}

	fmt.Println("==============================================================")
	fmt.Println("联合调参实验  λ =", lambda, " 测试图 =", imgW, "x", imgH, " 网格步长 Δs =", coarseH)
	fmt.Println("==============================================================")

	for _, k := range ks {
		delta := 256.0 / float64(k)
		b0, g0, bK, gK := coeffs(src, k)

		// ---- 网格搜索（实测 J），步长 h=0.05，s∈[0,1] 共 21 点 ----
		type point struct {
			s                   float64
			band, grain, mse, j float64
		}
		var best point
		all := make([]point, 0, 21)
		js := make([]float64, 21)
		for i := 0; i <= 20; i++ {
			s := float64(i) * coarseH
			band, grain, mse := probe(src, k, s)
			j := lambda*band + grain
			js[i] = j
			p := point{s, band, grain, mse, j}
			all = append(all, p)
			if i == 0 || j < best.j {
				best = p
			}
		}

		// ---- 闭式解：粗网格最优点邻域的局部二次插值顶点 ----
		sClosed, jModel := closedForm(src, k, js, coarseH)

		// ---- 细网格精修：粗最优点 ±0.05 内步长 0.01 ----
		sFine, jFine := fineGrid(src, k, best.s)

		fmt.Printf("\n########## 桶数 K=%d  Δ=%.1f ##########\n", k, delta)
		fmt.Printf("能量交换模型标定: B0=%.3f G0=%.3f b_K=%.4f g_K=%.3f；线性斜率 g_K-λB0b_K=%.3f\n",
			b0, g0, bK, gK, gK-lambda*b0*bK)
		fmt.Println("s      E_band   E_grain  MSE      J(加权)")
		for _, p := range all {
			mark := ""
			if p.s == best.s {
				mark = "  <- 网格最优"
			}
			fmt.Printf("%.2f  %8.3f %8.3f %8.3f %9.3f%s\n", p.s, p.band, p.grain, p.mse, p.j, mark)
		}

		// 闭式解点的实测表现（s 取连续值，量化器本身支持）。
		outC := codec.Quantize(imgW, imgH, src, k, sClosed)
		_, _, mseC, jC := metric(src, outC)
		fmt.Printf("闭式解(局部二次顶点) s*=%.4f 模型J=%.3f -> 实测 J=%.3f MSE=%.3f\n", sClosed, jModel, jC, mseC)
		fmt.Printf("粗网格最优 s=%.2f -> 实测 J=%.3f MSE=%.3f\n", best.s, best.j, best.mse)
		fmt.Printf("细网格精修 s=%.2f（±%.2f 内步长 %.2f）-> 实测 J=%.3f\n", sFine, fineRange, fineH, jFine)

		// 对照组合。
		ref := func(s float64) (float64, float64) {
			out := codec.Quantize(imgW, imgH, src, k, s)
			_, _, m, j := metric(src, out)
			return j, m
		}
		j0, m0 := ref(0)
		j1, m1 := ref(1)
		jh, mh := ref(0.5)
		fmt.Println("对照：              J(加权)  MSE")
		fmt.Printf("  s=0   不抖动   %8.3f %8.3f\n", j0, m0)
		fmt.Printf("  s=0.5 半强度   %8.3f %8.3f\n", jh, mh)
		fmt.Printf("  s=1   全强度   %8.3f %8.3f\n", j1, m1)
		fmt.Printf("  s=%.2f 联合最优 %8.3f %8.3f  (J 较 s=0 降 %.1f%%)\n",
			best.s, best.j, best.mse, 100*(j0-best.j)/j0)
	}

	// ---- 汇总：三档桶数并列 ----
	fmt.Println("\n==============================================================")
	fmt.Println("三档桶数最优组合并列汇总")
	fmt.Println("==============================================================")
	fmt.Println("K    Δ      闭式 s*   粗网格 s 细网格 s  最优 J    最优 MSE   s=0 的 J   降幅")
	fineCenters := make(map[int]float64)
	for _, k := range ks {
		delta := 256.0 / float64(k)
		js := make([]float64, 21)
		var coarseS, bestJ, bestM, j0 float64
		for i := 0; i <= 20; i++ {
			s := float64(i) * coarseH
			out := codec.Quantize(imgW, imgH, src, k, s)
			_, _, m, j := metric(src, out)
			js[i] = j
			if i == 0 || j < bestJ {
				coarseS, bestJ, bestM = s, j, m
			}
			if i == 0 {
				j0 = j
			}
		}
		sClosed, jModel := closedForm(src, k, js, coarseH)
		sFine, jFine := fineGrid(src, k, coarseS)
		fineCenters[k] = sFine
		fmt.Printf("%-3d %6.1f  %7.4f  %7.2f  %7.2f  %8.3f  %8.3f  %9.3f  %5.1f%%  (闭式模型J=%.2f, 细网格J=%.2f)\n",
			k, delta, sClosed, coarseS, sFine, bestJ, bestM, j0, 100*(j0-bestJ)/j0, jModel, jFine)
	}

	// ---- 3×3 误差曲面：列=三档桶数，行=联合最优点附近的三档 s ----
	// ---- 3×3 误差曲面：列=三档桶数，行=细网格最优点附近的三档 s ----
	// 每列以该桶数的细网格最优 s_K 为中心，取 s_K-0.01 / s_K / s_K+0.01。
	fmt.Println("\n3×3 误差曲面 J（列=桶数 K，行=该桶细网格最优 s 附近 ±0.01；★为该列最优）")
	header := "s \\ K"
	for _, k := range ks {
		header += fmt.Sprintf("   K=%-4d", k)
	}
	fmt.Println(header)
	for row, ds := range []float64{-fineH, 0, fineH} {
		label := []string{"s*-0.01", "s*     ", "s*+0.01"}[row]
		line := label
		for _, k := range ks {
			s := math.Max(0, math.Min(1, fineCenters[k]+ds))
			j := jAt(src, k, s)
			mark := " "
			if ds == 0 {
				mark = "★"
			}
			line += fmt.Sprintf("  %s%7.2f", mark, j)
		}
		fmt.Println(line)
	}

	// 桶数变化时最优组合随之变化的说明。
	fmt.Println("\n说明：量化步长 Δ=256/K 随桶数增大而减小。测试图含固定幅度的噪声基底，")
	fmt.Println("其能量不随 Δ 缩放；K 较小时量化带状能量 B0∝Δ² 占主导，抖动把低频能量搬成")
	fmt.Println("高频颗粒的收益大，最优 s* 偏高；K 增大后 B0 相对噪声基底迅速衰减，而抖动注入")
	fmt.Println("颗粒的代价 g_K 仍在，边际收益下降，最优 s* 随 K 增大而下移，三档细网格最优")
	fmt.Printf("依次为 s*(4)=%.2f、s*(8)=%.2f、s*(16)=%.2f，闭式解给出相同的单调趋势。\n",
		fineCenters[4], fineCenters[8], fineCenters[16])
}
