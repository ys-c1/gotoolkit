package imgfx

import "errors"

// ErrZeroBuckets 表示调色板桶数为零（api 层负责带哨兵错误返回，这里兜底）。
var ErrZeroBuckets = errors.New("调色板桶数必须为正")

// buildPalette 生成 buckets 个等间距灰度级，首项 0、末项 255，线性插值后四舍五入。
// 桶数为 1 时调色板只有一个灰度级 0。
func buildPalette(buckets int) []uint8 {
	pal := make([]uint8, buckets)
	for i := 0; i < buckets; i++ {
		if buckets == 1 {
			pal[i] = 0
			continue
		}
		v := (255*i + (buckets-1)/2) / (buckets - 1)
		pal[i] = uint8(v)
	}
	return pal
}

// nearestIndex 返回 pal 中与 v 欧氏距离（灰度即绝对差）最近的下标，
// 并列时取较小下标。pal 由 buildPalette 保证单调不减。
func nearestIndex(pal []uint8, v int) int {
	best, bestDist := 0, abs(int(pal[0])-v)
	for i := 1; i < len(pal); i++ {
		if d := abs(int(pal[i]) - v); d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// IndexResult 是调色板索引的结果。
type IndexResult struct {
	W, H    int
	Palette []uint8 // 等间距调色板，长度等于桶数
	Index   []uint8 // 每像素一个调色板下标；索引不去重，长度恒为 W*H
	Quant   []uint8 // 按下标回填的灰度像素，供 PGM/BMP/RLE 等常规格式编码
}

// IndexImage 把灰度图量化到 buckets 个等间距灰度级。
// dither 为 true 时使用 Floyd-Steinberg 误差扩散，误差整体乘以 1/strengthDen
// （strengthDen 即缩略除数），因此抖动强度与缩略比例成反比：强度 = 1/strengthDen，
// strengthDen 为 1（未缩略）时强度为 1。
// 索引阶段不做任何去重：Index 每个像素占一项，长度恒等于像素总数。
func IndexImage(w, h int, pix []uint8, buckets int, dither bool, strengthDen int) (*IndexResult, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	if buckets <= 0 {
		return nil, ErrZeroBuckets
	}
	if strengthDen <= 0 {
		strengthDen = 1
	}
	pal := buildPalette(buckets)
	idx := make([]uint8, w*h)
	quant := make([]uint8, w*h)

	if !dither {
		for i, v := range pix {
			k := nearestIndex(pal, int(v))
			idx[i] = uint8(k)
			quant[i] = pal[k]
		}
		return &IndexResult{W: w, H: h, Palette: pal, Index: idx, Quant: quant}, nil
	}

	// Floyd-Steinberg：扩散权重 7/16、3/16、5/16、1/16，
	// 再整体除以 strengthDen（用整数定点，errBuf 单位为 1/16 灰度级）。
	errBuf := make([]int, w*h)
	for y := 0; y < h; y++ {
		row := y * w
		for x := 0; x < w; x++ {
			p := row + x
			v := int(pix[p]) + errBuf[p]/16
			if v < 0 {
				v = 0
			} else if v > 255 {
				v = 255
			}
			k := nearestIndex(pal, v)
			qv := int(pal[k])
			idx[p] = uint8(k)
			quant[p] = uint8(qv)
			e := (v - qv) / strengthDen // 强度随缩略比例缩放的误差项
			spread := func(sx, sy, weight int) {
				nx, ny := x+sx, y+sy
				if nx < 0 || nx >= w || ny < 0 || ny >= h {
					return
				}
				errBuf[ny*w+nx] += e * weight
			}
			spread(1, 0, 7)
			spread(-1, 1, 3)
			spread(0, 1, 5)
			spread(1, 1, 1)
		}
	}
	return &IndexResult{W: w, H: h, Palette: pal, Index: idx, Quant: quant}, nil
}
