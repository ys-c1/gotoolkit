package predict

import (
	"fmt"
	"math"
	"testing"
)

const (
	testW = 192
	testH = 192
	block = 16
)

func clamp8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(math.Round(v))
}

// imgSmooth 低频平滑图: 正弦渐变叠加缓斜坡。
func imgSmooth(w, h int) []uint8 {
	p := make([]uint8, w*h)
	for i := 0; i < h; i++ {
		for j := 0; j < w; j++ {
			v := 128 +
				70*math.Sin(2*math.Pi*float64(j)/96)*math.Cos(2*math.Pi*float64(i)/128) +
				0.15*float64(j) - 0.10*float64(i)
			p[i*w+j] = clamp8(v)
		}
	}
	return p
}

// imgEdges 方向性结构图: 左半为沿 i-j 走向的正弦波(NW 邻域同相),
// 右半为竖直走向的正弦波(N 邻域同相), 叠加缓坡,
// 用来观察全局固定系数的折中与分块权重随梯度方向的翻转。
func imgEdges(w, h int) []uint8 {
	p := make([]uint8, w*h)
	for i := 0; i < h; i++ {
		for j := 0; j < w; j++ {
			v := 108 + 0.15*float64(j) + 0.10*float64(i)
			if j < w/2 {
				v += 55 * math.Sin(2*math.Pi*float64(i-j)/24)
			} else {
				v += 55 * math.Sin(2*math.Pi*float64(j)/24)
			}
			p[i*w+j] = clamp8(v)
		}
	}
	return p
}

// imgTexture 高频纹理图: 走向随位置缓慢旋转的正弦光栅。
// 局部方向性强但全局不一致, 是固定系数容易失利、
// 需要按局部梯度加权的典型区域。
func imgTexture(w, h int) []uint8 {
	p := make([]uint8, w*h)
	for i := 0; i < h; i++ {
		for j := 0; j < w; j++ {
			phi := 2 * math.Pi * float64(i+j) / 384 // 局部走向角
			v := 128 + 45*math.Sin(2*math.Pi*(math.Cos(phi)*float64(i)+math.Sin(phi)*float64(j))/6)
			p[i*w+j] = clamp8(v)
		}
	}
	return p
}

// TestPredictReport 对三张图比较一维预测、约束最小二乘二维预测与
// 梯度加权分块融合预测, 打印系数/熵/残差均值/正交性内积。
func TestPredictReport(t *testing.T) {
	imgs := []struct {
		name string
		pix  []uint8
	}{
		{"smooth", imgSmooth(testW, testH)},
		{"edges", imgEdges(testW, testH)},
		{"texture", imgTexture(testW, testH)},
	}
	fmt.Println("图       系数(a·W+b·N+c·NW)          H_1D   H_2D   H_adp  残差均值  ⟨r,W⟩     ⟨r,N⟩     ⟨r,NW⟩")
	for _, im := range imgs {
		co := SolveConstrained(testW, testH, im.pix)
		r1 := Residual1D(testW, testH, im.pix)
		r2 := Residual2D(testW, testH, im.pix, co)
		ra, ws := ResidualAdaptive(testW, testH, im.pix, block)
		h1, h2, ha := Entropy(r1), Entropy(r2), Entropy(ra)
		iw, in, inw := Orthogonality(testW, testH, im.pix, co)
		fmt.Printf("%-8s (%+.3f, %+.3f, %+.3f)  %.3f  %.3f  %.3f  %+.4f  %+.4f  %+.4f  %+.4f\n",
			im.name, co.A, co.B, co.C, h1, h2, ha, Mean(r2), iw, in, inw)

		if !(h2 < h1) {
			t.Errorf("%s: 二维约束LS熵 %.3f 应低于一维 %.3f", im.name, h2, h1)
		}
		if !(ha < h1) {
			t.Errorf("%s: 自适应融合熵 %.3f 应低于一维 %.3f", im.name, ha, h1)
		}
		// KKT: 三个内积应相等(共同值 λ/2)
		mx := math.Max(math.Abs(iw), math.Max(math.Abs(in), math.Abs(inw)))
		tol := 1e-8 * math.Max(mx, 1)
		if math.Abs(iw-in) > tol || math.Abs(in-inw) > tol {
			t.Errorf("%s: 正交性不满足, 内积应相等: %v %v %v", im.name, iw, in, inw)
		}
		_ = ws
	}
}

// TestAdaptiveWeightsFollowGradient 验证权重随局部梯度方向变化:
// 左半斜条纹块应把权重压到 NW, 右半竖条纹块应压到 N。
func TestAdaptiveWeightsFollowGradient(t *testing.T) {
	pix := imgEdges(testW, testH)
	_, ws := ResidualAdaptive(testW, testH, pix, block)
	nbw := (testW + block - 1) / block
	left := ws[6*nbw+2]  // 左半斜条纹区
	right := ws[6*nbw+9] // 右半竖条纹区
	fmt.Printf("edges 块权重: 左半斜条纹 W=%.3f N=%.3f NW=%.3f | 右半竖条纹 W=%.3f N=%.3f NW=%.3f\n",
		left.W, left.N, left.NW, right.W, right.N, right.NW)
	if left.NW <= left.W || left.NW <= left.N {
		t.Errorf("斜条纹块应以 NW 为主: %+v", left)
	}
	if right.N <= right.W || right.N <= right.NW {
		t.Errorf("竖条纹块应以 N 为主: %+v", right)
	}
	for k, wgt := range ws {
		s := wgt.W + wgt.N + wgt.NW
		if math.Abs(s-1) > 1e-9 {
			t.Errorf("块 %d 权重和不为 1: %v", k, s)
		}
	}
}
