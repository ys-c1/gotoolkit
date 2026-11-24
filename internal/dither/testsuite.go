package dither

import "math"

// BuildTestSuite 生成量化误差评估用的确定性测试图集（边长 testSize=256）：
//   - 7 张均匀灰场，灰度 32,64,...,224；
//   - 1 张水平线性渐变 0..255；
//   - 1 张径向缓坡（到中心距离 ×1.4 截断到 255）；
//   - 1 张三频正弦合成图（确定性，无随机数）。
//
// 全部图像逐次可复现，三种矩阵在同一图集上比较量化误差。
func BuildTestSuite() ([][]uint8, int, int) {
	w, h := testSize, testSize
	suite := make([][]uint8, 0, 10)

	flatGrays := []uint8{32, 64, 96, 128, 160, 192, 224}
	for _, g := range flatGrays {
		img := make([]uint8, w*h)
		for i := range img {
			img[i] = g
		}
		suite = append(suite, img)
	}

	// 水平渐变。
	grad := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			grad[y*w+x] = uint8(x * 255 / (w - 1))
		}
	}
	suite = append(suite, grad)

	// 径向缓坡。
	radial := make([]uint8, w*h)
	cx, cy := w/2, h/2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			d := math.Hypot(float64(x-cx), float64(y-cy)) * 1.4
			if d > 255 {
				d = 255
			}
			radial[y*w+x] = uint8(d)
		}
	}
	suite = append(suite, radial)

	// 确定性多频合成图：三个不同频率/相位的正弦叠加，覆盖缓变与中频结构，
	// 不使用随机数，保证三种矩阵的误差比较结论与 RNG 无关、逐次可复现。
	blobImg := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fx, fy := float64(x), float64(y)
			v := 128.0 +
				45.0*math.Sin(2*math.Pi*fx/64.0) +
				30.0*math.Sin(2*math.Pi*(fx+fy)/96.0+1.3) +
				20.0*math.Sin(2*math.Pi*fy/48.0+2.1)
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			blobImg[y*w+x] = uint8(v)
		}
	}
	suite = append(suite, blobImg)
	return suite, w, h
}
