package codec

import "math"

// Quantize 做带 Floyd–Steinberg 误差扩散的均匀量化实验。
//
// buckets 是桶数档；delta 是本次联合调参实际使用的量化步长（允许在
// 256/buckets 附近做亚桶级修正）。dither 是扩散强度 s∈[0,1]。
//
// 为了让“输出像素—误差统计—扩散误差”三者一致，连续电平 k*delta 会先
// 四舍五入成真正写出的 uint8 重建值，再以 v-qByte 作为扩散误差。
func Quantize(w, h int, pix []uint8, buckets int, delta, dither float64) []uint8 {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil
	}
	if buckets <= 0 {
		buckets = 256
	}
	if delta <= 0 {
		delta = 256.0 / float64(buckets)
	}
	if dither < 0 {
		dither = 0
	}
	if dither > 1 {
		dither = 1
	}

	buf := make([]float64, len(pix))
	for i, v := range pix {
		buf[i] = float64(v)
	}
	out := make([]uint8, len(pix))

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			v := buf[i]

			k := math.Round(v / delta)
			if k < 0 {
				k = 0
			}
			if k > float64(buckets) {
				k = float64(buckets)
			}
			level := math.Max(0, math.Min(255, k*delta))
			q := byte(math.Round(level))
			out[i] = q

			err := (v - float64(q)) * dither
			if x+1 < w {
				buf[i+1] += err * 7.0 / 16.0
			}
			if y+1 < h {
				if x > 0 {
					buf[i+w-1] += err * 3.0 / 16.0
				}
				buf[i+w] += err * 5.0 / 16.0
				if x+1 < w {
					buf[i+w+1] += err / 16.0
				}
			}
		}
	}
	return out
}
