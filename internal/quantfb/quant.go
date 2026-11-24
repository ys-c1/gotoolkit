package quantfb

import "math"

// Result 是一次量化/扩散处理的完整记录。
//
// Out 是最终交付的 8-bit 像素（已夹取到 [0,255]）；
// Level 是回路内部未夹取的量化电平 ỹ；
// U 是量化器输入 u；QErr 是回路误差 e=ỹ−u（恒有 |e|≤q/2）；
// OutErr 是交付残差 Out−x。
type Result struct {
	W, H   int
	Q      float64
	C1, C2 float64
	Out    []uint8
	Xf     []float64
	U      []float64
	Level  []float64
	QErr   []float64
	OutErr []float64
}

// FeedbackQuantize 执行二阶误差反馈量化。
//
// rowReset=true 时每行开始把 e_{-1},e_{-2} 清零（标准逐行处理，
// 也使两行延迟的证明在左边界天然成立）；false 时按展平光栅跨行列递推。
func FeedbackQuantize(img *Gray, q, c1, c2 float64, rowReset bool) *Result {
	W, H := img.W, img.H
	n := W * H
	r := &Result{
		W: W, H: H, Q: q, C1: c1, C2: c2,
		Out: make([]uint8, n), Xf: make([]float64, n),
		U: make([]float64, n), Level: make([]float64, n),
		QErr: make([]float64, n), OutErr: make([]float64, n),
	}

	step := func(idx int, x float64, e1, e2 float64) float64 {
		u := x + c1*e1 + c2*e2
		level := q * math.Floor(u/q+0.5) // 不夹取的 mid-tread 电平
		e := level - u
		r.Xf[idx] = x
		r.U[idx] = u
		r.Level[idx] = level
		r.QErr[idx] = e
		r.Out[idx] = clampByte(level) // 仅交付时夹取
		r.OutErr[idx] = float64(r.Out[idx]) - x
		return e
	}

	if rowReset {
		for y := 0; y < H; y++ {
			e1, e2 := 0.0, 0.0
			for x := 0; x < W; x++ {
				idx := y*W + x
				e := step(idx, float64(img.Pix[idx]), e1, e2)
				e2, e1 = e1, e
			}
		}
	} else {
		e1, e2 := 0.0, 0.0
		for idx := 0; idx < n; idx++ {
			e := step(idx, float64(img.Pix[idx]), e1, e2)
			e2, e1 = e1, e
		}
	}
	return r
}

// PlainQuantize 是无反馈的朴素步长量化（参照项，低频占比极高）。
func PlainQuantize(img *Gray, q float64) *Result {
	res := FeedbackQuantize(img, q, 0, 0, true)
	return res
}

// FloydSteinberg 用指定 palette 跑 A 组原核二维误差扩散。
// palette 为 nil 时使用 Palette8。返回与 FeedbackQuantize 同构的 Result
// （Q 记为 palette 最大半格距 18.5，C1/C2 不用）。
func FloydSteinberg(img *Gray, palette []float64) *Result {
	if palette == nil {
		palette = Palette8
	}
	W, H := img.W, img.H
	n := W * H
	buf := make([]float64, n)
	for i, v := range img.Pix {
		buf[i] = float64(v)
	}
	r := &Result{
		W: W, H: H, Q: 18.5,
		Out: make([]uint8, n), Xf: make([]float64, n),
		U: make([]float64, n), Level: make([]float64, n),
		QErr: make([]float64, n), OutErr: make([]float64, n),
	}
	k := OriginalFloydSteinberg()
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			idx := y*W + x
			old := buf[idx]
			v := nearestPalette(old, palette)
			e := old - v // FS 约定误差方向与本包 e=level−u 相反；统计用绝对值不受影响
			r.Xf[idx] = float64(img.Pix[idx])
			r.U[idx] = old
			r.Level[idx] = v
			r.QErr[idx] = -e // 统一成 level−u 口径
			r.Out[idx] = clampByte(v)
			r.OutErr[idx] = float64(r.Out[idx]) - r.Xf[idx]
			for _, t := range k.Taps {
				nx, ny := x+t.DX, y+t.DY
				if nx < 0 || nx >= W || ny < 0 || ny >= H {
					continue
				}
				buf[ny*W+nx] += e * (t.W / k.Div)
			}
		}
	}
	return r
}

func nearestPalette(u float64, pal []float64) float64 {
	best := pal[0]
	bd := math.Abs(u - best)
	for _, v := range pal[1:] {
		if d := math.Abs(u - v); d < bd {
			bd, best = d, v
		}
	}
	return best
}
