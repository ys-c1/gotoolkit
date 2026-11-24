package subband

import "math"

// SourceImage 生成确定性的 size×size 灰度图（size 应能被 8 整除）。
// 内容刻意同时覆盖：
//   - 纵向缓变梯度 + 径向柔光 + 大块平坦矩形：粗低频带一旦粗量化就出块状条纹/色带；
//   - 硬边矩形、对角线、棋盘格、细条：高频带一旦粗量化/截断就出边缘毛刺与锯齿；
//   - ±3 的确定性弱噪声：让最细高频带方差非零，分配结果不退化。
func SourceImage(size int) (int, int, []uint8) {
	w, h := size, size
	pix := make([]uint8, w*h)
	seed := uint32(0x5eed1234)
	rand := func() float64 {
		seed = seed*1103515245 + 12345
		return float64(seed>>16)/65536.0 // [0,1)
	}
	set := func(x, y int, v float64) {
		if x < 0 || x >= w || y < 0 || y >= h {
			return
		}
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		pix[y*w+x] = uint8(v + 0.5)
	}
	get := func(x, y int) float64 {
		if x < 0 || x >= w || y < 0 || y >= h {
			return 0
		}
		return float64(pix[y*w+x])
	}
	fillRect := func(x0, y0, x1, y1 int, v float64) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				set(x, y, v)
			}
		}
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// 基础纵向渐变 40→215。
			v := 40 + 175*float64(y)/float64(h-1)
			// 径向柔光。
			dx, dy := float64(x-34), float64(y-38)
			v += 55 * math.Exp(-(dx*dx+dy*dy)/(2*13*13))
			// 弱噪声 ±3。
			v += 6 * (rand() - 0.5)
			set(x, y, v)
		}
	}

	// 对角硬边直线（约 1.6px 粗），制造方向性能量与 HH 分量。
	for x := 6; x < 66; x++ {
		yLine := 72 - int(float64(x-6)*0.95)
		for t := -1; t <= 1; t++ {
			if math.Abs(float64(t)) <= 1.6 {
				set(x, yLine+t, 12)
			}
		}
	}

	// 右上：亮平坦矩形 + 深色描边。
	fillRect(70, 12, 122, 48, 214)
	for x := 70; x < 122; x++ {
		set(x, 12, 20)
		set(x, 47, 20)
	}
	for y := 12; y < 48; y++ {
		set(70, y, 20)
		set(121, y, 20)
	}

	// 左下：暗平坦矩形 + 亮描边。
	fillRect(8, 80, 54, 120, 30)
	for x := 8; x < 54; x++ {
		set(x, 80, 235)
		set(x, 119, 235)
	}
	for y := 80; y < 120; y++ {
		set(8, y, 235)
		set(53, y, 235)
	}

	// 右下：6px 格棋盘，强高频纹理。
	cx0, cy0, cell := 70, 66, 6
	for y := cy0; y < cy0+48; y++ {
		for x := cx0; x < cx0+48; x++ {
			if ((x-cx0)/cell+(y-cy0)/cell)%2 == 0 {
				set(x, y, 238)
			} else {
				set(x, y, 18)
			}
		}
	}
	// 棋盘外框。
	for x := cx0 - 1; x <= cx0+48; x++ {
		set(x, cy0-1, 0)
		set(x, cy0+48, 0)
	}
	for y := cy0 - 1; y <= cy0+48; y++ {
		set(cx0-1, y, 0)
		set(cx0+48, y, 0)
	}

	// 中部三条细横杠（类文字笔画）。
	for k, yy := range []int{54, 60, 66} {
		for x := 56; x < 98-k*8; x++ {
			set(x, yy, 10)
			set(x, yy+1, 10)
		}
	}

	// 用一次轻微混合消除描边锯齿之外的孤立噪声点影响不需要；
	// get 保留给未来局部对比度校验。
	_ = get
	return w, h, pix
}
