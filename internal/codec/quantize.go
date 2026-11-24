package codec

import "math"

// Quantize 对灰度图做带 Floyd–Steinberg 误差扩散的均匀量化。
//
//   - buckets：量化桶数（重建电平数），取值 [2,256]；256 时为恒等映射，像素不变。
//   - dither：抖动强度 s∈[0,1]，s=0 关闭抖动（朴素四舍五入），s=1 全强度扩散。
//
// 量化步长 Δ = 256/buckets，量化器为 round((x+e)/Δ)·Δ 并夹到 [0,255]。
// 扩散核（Floyd–Steinberg，权重和为 1）：
//
//	      |      x  7/16 |
//	| 3/16 5/16 1/16 |
//
// 实际扩散量再乘 dither，因此 dither 是核强度的连续缩放旋钮。
func Quantize(w, h int, pix []uint8, buckets int, dither float64) []uint8 {
	if buckets <= 0 {
		buckets = 256
	}
	if buckets >= 256 {
		out := make([]uint8, len(pix))
		copy(out, pix)
		return out
	}
	delta := 256.0 / float64(buckets)
	buf := make([]float64, len(pix))
	for i, v := range pix {
		buf[i] = float64(v)
	}
	out := make([]uint8, len(pix))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			idx := y*w + x
			v := buf[idx]
			q := math.Round(v/delta) * delta
			if q < 0 {
				q = 0
			}
			if q > 255 {
				q = 255
			}
			out[idx] = uint8(q)
			err := (v - q) * dither
			if x+1 < w {
				buf[idx+1] += err * 7.0 / 16.0
			}
			if y+1 < h {
				if x > 0 {
					buf[idx+w-1] += err * 3.0 / 16.0
				}
				buf[idx+w] += err * 5.0 / 16.0
				if x+1 < w {
					buf[idx+w+1] += err / 16.0
				}
			}
		}
	}
	return out
}
