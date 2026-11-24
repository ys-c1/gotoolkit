package recon

import (
	"fmt"
	"math"
)

// 统计内缩边：卷积用 clamp 边界，但色差统计避开边界，杜绝边界约定影响结论。
const Margin = 4

// Method 是可切换的重建路径。双线性路径必须一直保留，可随时切回。
type Method string

const (
	// MethodBilinear 是经典“2x2 均值降一半 + 双线性放大回原尺寸”路径。
	MethodBilinear Method = "bilinear"
	// MethodCoSited 是共址双线性核 δ=(0,0,1,0,0)，即同一观测上的对齐双线性。
	MethodCoSited Method = "cosited"
	// MethodCLSK 是约束最小二乘解出的五抽头中心对称核。
	MethodCLSK Method = "clsk"
)

// TapNames 与抽头下标顺序 [上, 左, 中, 右, 下] 对应。
var TapNames = [TapCount]string{"h0(上)", "h1(左)", "h2(中)", "h3(右)", "h4(下)"}

// BlurWeights 是观测柔化（光学抗混叠）用的 3 抽头双线性/三角权重。
// 取较温和的 [0.15,0.70,0.15]，使约束 LS 逆核系数保持在温和、可解释的范围。
var BlurWeights = [3]float64{0.08, 0.84, 0.08}

// Soften 用 BlurWeights 做一次可分离柔化（先水平后垂直），边界 clamp。
func Soften(im *Image) *Image {
	return blurV(blurH(im, BlurWeights), BlurWeights)
}

// SoftenHorizontal 只做水平柔化，用来构造与一维求逆一致的训练样本。
// 本题最终采用二维五点模型，训练直接在 Soften 的二维观测上进行，保留该函数便于核对。
func SoftenHorizontal(im *Image) *Image { return blurH(im, BlurWeights) }

func blurH(im *Image, w [3]float64) *Image {
	out := NewImage(im.W, im.H)
	for y := 0; y < im.H; y++ {
		for x := 0; x < im.W; x++ {
			a := im.AtClamped(x-1, y)
			c := im.AtClamped(x, y)
			b := im.AtClamped(x+1, y)
			out.Pix[y*im.W+x] = RGB{
				R: w[0]*a.R + w[1]*c.R + w[2]*b.R,
				G: w[0]*a.G + w[1]*c.G + w[2]*b.G,
				B: w[0]*a.B + w[1]*c.B + w[2]*b.B,
			}
		}
	}
	return out
}

func blurV(im *Image, w [3]float64) *Image {
	out := NewImage(im.W, im.H)
	for y := 0; y < im.H; y++ {
		for x := 0; x < im.W; x++ {
			a := im.AtClamped(x, y-1)
			c := im.AtClamped(x, y)
			b := im.AtClamped(x, y+1)
			out.Pix[y*im.W+x] = RGB{
				R: w[0]*a.R + w[1]*c.R + w[2]*b.R,
				G: w[0]*a.G + w[1]*c.G + w[2]*b.G,
				B: w[0]*a.B + w[1]*c.B + w[2]*b.B,
			}
		}
	}
	return out
}

// ApplyFive 按 [上,左,中,右,下] 顺序应用五个抽头，边界 clamp，输出裁剪到 [0,1]。
func ApplyFive(im *Image, h [TapCount]float64) *Image {
	out := NewImage(im.W, im.H)
	for y := 0; y < im.H; y++ {
		for x := 0; x < im.W; x++ {
			up := im.AtClamped(x, y-1)
			left := im.AtClamped(x-1, y)
			c := im.AtClamped(x, y)
			right := im.AtClamped(x+1, y)
			down := im.AtClamped(x, y+1)
			out.Pix[y*im.W+x] = clampRGB(RGB{
				R: h[0]*up.R + h[1]*left.R + h[2]*c.R + h[3]*right.R + h[4]*down.R,
				G: h[0]*up.G + h[1]*left.G + h[2]*c.G + h[3]*right.G + h[4]*down.G,
				B: h[0]*up.B + h[1]*left.B + h[2]*c.B + h[3]*right.B + h[4]*down.B,
			})
		}
	}
	return out
}

// BilinearCoSitedTaps 是共址双线性核：输出点正好落在样本上时，三角核只取中心，
// 即 δ=(0,0,1,0,0)。它满足 Σh=1 与中心对称，是约束可行域中的一个点。
func BilinearCoSitedTaps() [TapCount]float64 {
	return [TapCount]float64{0, 0, 1, 0, 0}
}

// BilinearNeighborTaps 是十字四邻域双线性/梅花网格权重 (1/4...)，中心为 0。
func BilinearNeighborTaps() [TapCount]float64 {
	return [TapCount]float64{0.25, 0.25, 0, 0.25, 0.25}
}

// DownsampleBox2 用 2x2 均值把偶数画幅精确减半。
func DownsampleBox2(im *Image) *Image {
	w, h := im.W/2, im.H/2
	out := NewImage(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			x0, y0 := 2*x, 2*y
			a := im.Pix[y0*im.W+x0]
			b := im.Pix[y0*im.W+x0+1]
			c := im.Pix[(y0+1)*im.W+x0]
			d := im.Pix[(y0+1)*im.W+x0+1]
			out.Pix[y*w+x] = RGB{
				R: (a.R + b.R + c.R + d.R) / 4,
				G: (a.G + b.G + c.G + d.G) / 4,
				B: (a.B + b.B + c.B + d.B) / 4,
			}
		}
	}
	return out
}

// BilinearScale 是教科书双线性插值缩放（像素中心映射 + 边界 clamp）。
func BilinearScale(src *Image, dw, dh int) *Image {
	out := NewImage(dw, dh)
	scaleX := float64(src.W) / float64(dw)
	scaleY := float64(src.H) / float64(dh)
	for y := 0; y < dh; y++ {
		sy := (float64(y)+0.5)*scaleY - 0.5
		y0 := int(math.Floor(sy))
		fy := sy - math.Floor(sy)
		y0c := clampInt(y0, 0, src.H-1)
		y1c := clampInt(y0+1, 0, src.H-1)
		for x := 0; x < dw; x++ {
			sx := (float64(x)+0.5)*scaleX - 0.5
			x0 := int(math.Floor(sx))
			fx := sx - math.Floor(sx)
			x0c := clampInt(x0, 0, src.W-1)
			x1c := clampInt(x0+1, 0, src.W-1)
			p00 := src.Pix[y0c*src.W+x0c]
			p10 := src.Pix[y0c*src.W+x1c]
			p01 := src.Pix[y1c*src.W+x0c]
			p11 := src.Pix[y1c*src.W+x1c]
			w00 := (1 - fx) * (1 - fy)
			w10 := fx * (1 - fy)
			w01 := (1 - fx) * fy
			w11 := fx * fy
			out.Pix[y*dw+x] = clampRGB(RGB{
				R: w00*p00.R + w10*p10.R + w01*p01.R + w11*p11.R,
				G: w00*p00.G + w10*p10.G + w01*p01.G + w11*p11.G,
				B: w00*p00.B + w10*p10.B + w01*p01.B + w11*p11.B,
			})
		}
	}
	return out
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// BilinearRoundTrip 走完整经典双线性路径：2x2 均值减半，再双线性放大回原尺寸。
func BilinearRoundTrip(im *Image) *Image {
	half := DownsampleBox2(im)
	return BilinearScale(half, im.W, im.H)
}

// Reconstruct 是方法开关：同一观测 S 上可在三条路径间切换。
// 切回 MethodBilinear 时结果由纯函数决定，与首次基线严格一致。
func Reconstruct(m Method, observed *Image, h [TapCount]float64) (*Image, error) {
	switch m {
	case MethodBilinear:
		return BilinearRoundTrip(observed), nil
	case MethodCoSited:
		return ApplyFive(observed, BilinearCoSitedTaps()), nil
	case MethodCLSK:
		return ApplyFive(observed, h), nil
	default:
		return nil, fmt.Errorf("未知重建方法 %q", m)
	}
}

// BuildRegression 在整组图的柔化观测上抽取五点特征。
// 特征顺序 [上,左,中,右,下]，每个通道一行，目标是原始真值通道值。
func BuildRegression(imgs []*Image) (A [][]float64, y []float64) {
	for _, im := range imgs {
		s := Soften(im)
		for py := Margin - 2; py < im.H-(Margin-2); py++ {
			for px := Margin - 2; px < im.W-(Margin-2); px++ {
				up := s.AtClamped(px, py-1)
				left := s.AtClamped(px-1, py)
				c := s.AtClamped(px, py)
				right := s.AtClamped(px+1, py)
				down := s.AtClamped(px, py+1)
				feats := [TapCount]RGB{up, left, c, right, down}
				for ch := 0; ch < 3; ch++ {
					row := make([]float64, TapCount)
					var target float64
					for k := 0; k < TapCount; k++ {
						row[k] = chanAt(feats[k], ch)
					}
					target = chanAt(im.Pix[py*im.W+px], ch)
					A = append(A, row)
					y = append(y, target)
				}
			}
		}
	}
	return A, y
}

func chanAt(c RGB, ch int) float64 {
	switch ch {
	case 0:
		return c.R
	case 1:
		return c.G
	default:
		return c.B
	}
}

// ImageRow 是单张图上两种口径、三条路径的色差统计。
type ImageRow struct {
	Name                       string
	BilinearLab, ClskLab       Stats
	BilinearRGB, ClskRGB       Stats
	CoSitedLab                 Stats
}

// Report 是一次完整求解与同组图对比的结果。
type Report struct {
	Solved  *CLSResult
	Rows    []ImageRow
	BilLab  Stats // 跨全部像素汇总（平均为像素加权平均，最大为全局最大）
	ClskLab Stats
	BilRGB  Stats
	ClskRGB Stats
	CoLab   Stats
}

// Run 执行：生成同一组图 -> 约束 LS 解五抽头 -> 三条路径 -> 平均/最大色差。
func Run() (*Report, error) {
	imgs := Suite()
	A, y := BuildRegression(imgs)
	sol, err := SolveConstrainedLS(A, y)
	if err != nil {
		return nil, err
	}
	rep := &Report{Solved: sol}

	var bilLab, clskLab, coLab, bilRGB, clskRGB statAcc
	for i, im := range imgs {
		s := Soften(im)
		bil := BilinearRoundTrip(s)
		co := ApplyFive(s, BilinearCoSitedTaps())
		tap := ApplyFive(s, sol.H)

		row := ImageRow{Name: NameList[i]}
		row.BilinearLab, row.BilinearRGB = colorStats(im, bil, &bilLab, &bilRGB)
		coL, _ := colorStatsPair(im, co)
		row.CoSitedLab = coL
		accumStats(&coLab, coL)
		row.ClskLab, row.ClskRGB = colorStats(im, tap, &clskLab, &clskRGB)
		rep.Rows = append(rep.Rows, row)
	}
	rep.BilLab = bilLab.stats()
	rep.ClskLab = clskLab.stats()
	rep.BilRGB = bilRGB.stats()
	rep.ClskRGB = clskRGB.stats()
	rep.CoLab = coLab.stats()
	return rep, nil
}

// colorStats 计算并同时累加 Lab 与 RGB 色差。
func colorStats(ref, got *Image, accLab *statAcc, accRGB *statAcc) (Stats, Stats) {
	l, r := colorStatsPair(ref, got)
	accumStats(accLab, l)
	accumStats(accRGB, r)
	return l, r
}

func colorStatsPair(ref, got *Image) (Stats, Stats) {
	var la, ra statAcc
	for y := Margin; y < ref.H-Margin; y++ {
		for x := Margin; x < ref.W-Margin; x++ {
			i := y*ref.W + x
			l0 := RGBToLab(ref.Pix[i])
			l1 := RGBToLab(got.Pix[i])
			la.add(DeltaE76(l0, l1))
			ra.add(RGBDistance(ref.Pix[i], got.Pix[i]))
		}
	}
	return la.stats(), ra.stats()
}

func accumStats(acc *statAcc, s Stats) {
	// Stats.Mean 是 n 个像素的平均，按 n 加权还原总和，保证跨图汇总为像素加权平均。
	acc.n += s.N
	acc.sum += s.Mean * float64(s.N)
	if s.Max > acc.max {
		acc.max = s.Max
	}
}
