// Package imgfx 实现缩略图链路上的像素级处理：面积平均缩略与调色板索引。
// 只在 internal 内使用，外部调用方只能通过 api 包间接触发。
package imgfx

import "errors"

// ErrBadDiv 表示缩略除数非法（调用方应在 api 层提前拦截，这里兜底）。
var ErrBadDiv = errors.New("缩略除数必须为正")

// Thumbnail 按 div×div 分块做面积平均（块内像素求和后除以块内像素数，四舍五入）。
// 画幅不能被 div 整除时，边缘块按实际大小平均；输出尺寸为 ceil(W/div) × ceil(H/div)。
// div==1 时逐块 1×1，输出与输入逐像素一致。
func Thumbnail(w, h int, pix []uint8, div int) (int, int, []uint8, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return 0, 0, nil, errors.New("画幅或像素数不匹配")
	}
	if div <= 0 {
		return 0, 0, nil, ErrBadDiv
	}
	nw, nh := (w+div-1)/div, (h+div-1)/div
	out := make([]uint8, nw*nh)
	for by := 0; by < nh; by++ {
		y0 := by * div
		y1 := y0 + div
		if y1 > h {
			y1 = h
		}
		for bx := 0; bx < nw; bx++ {
			x0 := bx * div
			x1 := x0 + div
			if x1 > w {
				x1 = w
			}
			sum, n := 0, 0
			for y := y0; y < y1; y++ {
				row := y * w
				for x := x0; x < x1; x++ {
					sum += int(pix[row+x])
					n++
				}
			}
			out[by*nw+bx] = uint8((sum + n/2) / n)
		}
	}
	return nw, nh, out, nil
}
