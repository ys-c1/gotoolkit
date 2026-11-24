package recon

import "math"

// Lab 是 CIE L*a*b*（D65 白点）。
type Lab struct{ L, A, B float64 }

// srgbToLinear 实现 sRGB 传递函数的反函数。
func srgbToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// RGBToLab 把归一化 sRGB 转为 CIE Lab（D65）。
func RGBToLab(c RGB) Lab {
	r := srgbToLinear(c.R)
	g := srgbToLinear(c.G)
	b := srgbToLinear(c.B)

	// sRGB D65 -> XYZ
	x := 0.4124564*r + 0.3575761*g + 0.1804375*b
	y := 0.2126729*r + 0.7151522*g + 0.0721750*b
	z := 0.0193339*r + 0.1191920*g + 0.9503041*b

	// D65 参考白点
	x /= 0.95047
	y /= 1.00000
	z /= 1.08883

	f := func(t float64) float64 {
		if t > 0.008856 {
			return math.Cbrt(t)
		}
		return 7.787*t + 16.0/116.0
	}
	fx, fy, fz := f(x), f(y), f(z)
	return Lab{
		L: 116*fy - 16,
		A: 500 * (fx - fy),
		B: 200 * (fy - fz),
	}
}

// DeltaE76 是 CIE76 色差，即 Lab 空间欧氏距离。
func DeltaE76(a, b Lab) float64 {
	dl := a.L - b.L
	da := a.A - b.A
	db := a.B - b.B
	return math.Sqrt(dl*dl + da*da + db*db)
}

// RGBDistance 是归一化 RGB 欧氏距离（放大到 0..255 尺度，便于阅读）。
func RGBDistance(a, b RGB) float64 {
	dr := (a.R - b.R) * 255
	dg := (a.G - b.G) * 255
	db := (a.B - b.B) * 255
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

// Stats 是一组像素色差的平均值与最大值。
type Stats struct {
	Mean float64
	Max  float64
	N    int
}

type statAcc struct {
	sum, max float64
	n        int
}

func (a *statAcc) add(v float64) {
	a.sum += v
	a.n++
	if v > a.max {
		a.max = v
	}
}

func (a *statAcc) stats() Stats {
	if a.n == 0 {
		return Stats{}
	}
	return Stats{Mean: a.sum / float64(a.n), Max: a.max, N: a.n}
}
