package api

import "gotoolkit/internal/quant"

// quantizeImage 用给定单调量化表逐像素重建，返回新图（不改输入）。
func quantizeImage(img *Image, tab *quant.Table) *Image {
	out := &Image{W: img.W, H: img.H, Pix: make([]uint8, len(img.Pix))}
	for i, v := range img.Pix {
		out.Pix[i] = tab.Quantize(v)
	}
	return out
}
