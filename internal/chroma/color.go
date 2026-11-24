// Package chroma 实现彩色图像的色度 4:2:0 降采样与上采样重建。
//
// 亮度 Y 全分辨率保留；色度 Cb/Cr 在横纵各取一半（每 2x2 亮度块一个色度样本）。
// 重建时支持两种模式：
//   - Bilinear：直接双线性，权重只由几何位置决定；
//   - Adaptive：在双线性基础上，按亮度与色度的相关性、用局部亮度方差归一化的
//     相似度调整权重，使彩色边缘处的重建误差最小。
//
// 色彩空间采用 BT.601 全范围（JFIF/全摆动）转换，RGB 与内部 YCbCr 之间可逆。
package chroma

import "errors"

// RGB 是一幅 8 位彩色图，像素按 R,G,B 紧密排列，长度为 W*H*3。
type RGB struct {
	W, H int
	Pix  []uint8
}

// Plane 是单通道的全分辨率浮点平面（用于重建后的 Cb/Cr，保留亚像素精度）。
type Plane struct {
	W, H int
	V    []float64
}

// YCbCr 是 4:2:0 色度平面表示：Y 为全分辨率 uint8，Cb/Cr 为半分辨率 float64。
type YCbCr struct {
	W, H   int // 全分辨率宽高（调用方需保证均为偶数）
	Y      []uint8
	Cb, Cr Plane
	CW, CH int // 色度半分辨率宽高
}

// 重建模式。
const (
	ModeBilinear = "bilinear" // 直接双线性
	ModeAdaptive = "adaptive" // 局部方差自适应权重
)

// ErrOddSize 在画幅宽高不为偶数时返回（4:2:0 需要偶数对齐）。
var ErrOddSize = errors.New("chroma: 4:2:0 要求宽高均为偶数")

// BT.601 全范围系数。
const (
	kr = 0.299
	kg = 0.587
	kb = 0.114
)

// RGBToYCbCr 把整幅 RGB 图转成全分辨率 Y/Cb/Cr 浮点平面。
func RGBToYCbCr(img *RGB) (y, cb, cr *Plane) {
	w, h := img.W, img.H
	y = &Plane{W: w, H: h, V: make([]float64, w*h)}
	cb = &Plane{W: w, H: h, V: make([]float64, w*h)}
	cr = &Plane{W: w, H: h, V: make([]float64, w*h)}
	for i := 0; i < w*h; i++ {
		r := float64(img.Pix[3*i])
		g := float64(img.Pix[3*i+1])
		b := float64(img.Pix[3*i+2])
		yv := kr*r + kg*g + kb*b
		y.V[i] = yv
		cb.V[i] = clampf((b-yv)/(2*(1-kb)) + 128)
		cr.V[i] = clampf((r-yv)/(2*(1-kr)) + 128)
	}
	return y, cb, cr
}

// YCbCrToRGB 由全分辨率 Y 与重建后的 Cb/Cr 平面还原 RGB（结果钳制到 [0,255]）。
func YCbCrToRGB(y []uint8, cb, cr *Plane) *RGB {
	n := len(y)
	out := &RGB{W: cb.W, H: cb.H, Pix: make([]uint8, n*3)}
	for i := 0; i < n; i++ {
		yv := float64(y[i])
		db := cb.V[i] - 128
		dr := cr.V[i] - 128
		r := yv + 2*(1-kr)*dr
		b := yv + 2*(1-kb)*db
		g := (yv - kr*r - kb*b) / kg
		out.Pix[3*i] = sat8(r)
		out.Pix[3*i+1] = sat8(g)
		out.Pix[3*i+2] = sat8(b)
	}
	return out
}

// Subsample 执行 4:2:0 色度降采样：每个 2x2 亮度块的色度取四像素均值，
// 亮度全分辨率保留。返回的半分辨率色度平面尺寸为 W/2 x H/2。
func Subsample(img *RGB) (*YCbCr, error) {
	w, h := img.W, img.H
	if w <= 0 || h <= 0 || len(img.Pix) != w*h*3 {
		return nil, errors.New("chroma: 画幅或像素数不匹配")
	}
	if w&1 != 0 || h&1 != 0 {
		return nil, ErrOddSize
	}
	yf, cbf, crf := RGBToYCbCr(img)
	cw, ch := w/2, h/2
	ys := make([]uint8, w*h)
	for i, v := range yf.V {
		ys[i] = sat8(v)
	}
	cb := Plane{W: cw, H: ch, V: make([]float64, cw*ch)}
	cr := Plane{W: cw, H: ch, V: make([]float64, cw*ch)}
	for cy := 0; cy < ch; cy++ {
		for cx := 0; cx < cw; cx++ {
			var sb, sr float64
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					idx := (2*cy+dy)*w + (2*cx + dx)
					sb += cbf.V[idx]
					sr += crf.V[idx]
				}
			}
			k := cy*cw + cx
			cb.V[k] = sb / 4
			cr.V[k] = sr / 4
		}
	}
	return &YCbCr{W: w, H: h, Y: ys, Cb: cb, Cr: cr, CW: cw, CH: ch}, nil
}

func sat8(v float64) uint8 {
	v += 0.5 // 四舍五入，保证与全分辨率参考的量化一致
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func clampf(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}
