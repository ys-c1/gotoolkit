package chroma

import "math"

// Metrics 是一幅重建图相对全分辨率参考图的色差统计。
//
// 色差在两个空间分别报告：
//   - CbCr：色度平面欧氏距离 sqrt(ΔCb²+ΔCr²)，直接反映色度重建质量；
//   - RGB：重建后 RGB 的欧氏距离 sqrt(ΔR²+ΔG²+ΔB²)，反映最终可见色差。
type Metrics struct {
	EdgePixels int // 参与统计的边缘像素数
	CbCrMean   float64
	CbCrMax    float64
	RGBMean    float64
	RGBMax     float64
}

// edgeMask 用亮度梯度标记边缘像素：3x3 Sobel 幅值超过整图均值+阈值即视为边缘。
// 色度误差在边缘处最显著（跨边缘混色），故色差只在边缘像素上统计。
func edgeMask(y []uint8, w, h int) []bool {
	mask := make([]bool, w*h)
	mag := make([]float64, w*h)
	var sum float64
	for y0 := 1; y0 < h-1; y0++ {
		for x0 := 1; x0 < w-1; x0++ {
			gx := -float64(y[(y0-1)*w+x0-1]) + float64(y[(y0-1)*w+x0+1]) +
				-2*float64(y[y0*w+x0-1]) + 2*float64(y[y0*w+x0+1]) +
				-float64(y[(y0+1)*w+x0-1]) + float64(y[(y0+1)*w+x0+1])
			gy := -float64(y[(y0-1)*w+x0-1]) - 2*float64(y[(y0-1)*w+x0]) - float64(y[(y0-1)*w+x0+1]) +
				float64(y[(y0+1)*w+x0-1]) + 2*float64(y[(y0+1)*w+x0]) + float64(y[(y0+1)*w+x0+1])
			m := math.Hypot(gx, gy)
			mag[y0*w+x0] = m
			sum += m
		}
	}
	mean := sum / float64(w*h)
	thr := mean * 1.5
	for i, m := range mag {
		if m > thr {
			mask[i] = true
		}
	}
	return mask
}

// ColorDiff 在边缘像素上比较参考图与重建图，返回平均/最大色差（CbCr 与 RGB 两空间）。
// ref 为未经降采样的原始全分辨率图，c 为其 4:2:0 降采样结果，recon 为重建 RGB。
func ColorDiff(ref *RGB, c *YCbCr, recon *RGB) Metrics {
	w, h := ref.W, ref.H
	mask := edgeMask(c.Y, w, h)

	// 参考图的全分辨率色度，作为色差基准。
	_, refCb, refCr := RGBToYCbCr(ref)
	_, rcCb, rcCr := RGBToYCbCr(recon)

	var m Metrics
	var cbSum, rgbSum float64
	for i := 0; i < w*h; i++ {
		if !mask[i] {
			continue
		}
		m.EdgePixels++

		dcb := refCb.V[i] - rcCb.V[i]
		dcr := refCr.V[i] - rcCr.V[i]
		cbErr := math.Hypot(dcb, dcr)
		cbSum += cbErr
		if cbErr > m.CbCrMax {
			m.CbCrMax = cbErr
		}

		dr := float64(ref.Pix[3*i]) - float64(recon.Pix[3*i])
		dg := float64(ref.Pix[3*i+1]) - float64(recon.Pix[3*i+1])
		db := float64(ref.Pix[3*i+2]) - float64(recon.Pix[3*i+2])
		rgbErr := math.Sqrt(dr*dr + dg*dg + db*db)
		rgbSum += rgbErr
		if rgbErr > m.RGBMax {
			m.RGBMax = rgbErr
		}
	}
	if m.EdgePixels > 0 {
		m.CbCrMean = cbSum / float64(m.EdgePixels)
		m.RGBMean = rgbSum / float64(m.EdgePixels)
	}
	return m
}
