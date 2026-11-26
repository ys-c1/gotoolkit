package api

// NewGradientImage 生成一张确定性的平滑双线性渐变测试图，全画幅可整除三档块长。
// pix[y][x] = 32 + 2*x + y（64×64 时取值范围 32..221，不发生截断），
// 两个方向都有恒定梯度，因此不预测路径的缝宽在两个方向都可见、且随块长线性增大。
func NewGradientImage(w, h int) *Image {
	if w <= 0 || h <= 0 {
		return nil
	}
	pix := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pix[y*w+x] = uint8(32 + 2*x + y)
		}
	}
	return &Image{W: w, H: h, Pix: pix}
}

// Seam 是一次块边界缝宽度量结果，单位为灰度级（0..255）。
// 缝宽定义为块边界两侧相邻像素灰度差的绝对值的最大值：
//   - Vertical：竖直块边界（左右相邻块之间），横向像素对的最大跳变；
//   - Horizontal：水平块边界（上下相邻块之间），纵向像素对的最大跳变；
//   - Max：两者的最大值，作为该块长下的整体缝宽。
type Seam struct {
	Vertical   int
	Horizontal int
	Max        int
}

// MeasureSeam 按给定块长度量 img 的块间缝宽。尾块不足整块时以实际块边界为准。
func MeasureSeam(img *Image, blockSize int) Seam {
	if img == nil || blockSize <= 0 {
		return Seam{}
	}
	w, h, pix := img.W, img.H, img.Pix
	var s Seam
	// 竖直边界：x = bs, 2bs, ...，比较 (x-1) 与 x 两列。
	for x := blockSize; x < w; x += blockSize {
		for y := 0; y < h; y++ {
			d := absInt(int(pix[y*w+x]) - int(pix[y*w+x-1]))
			if d > s.Vertical {
				s.Vertical = d
			}
		}
	}
	// 水平边界：y = bs, 2bs, ...，比较 (y-1) 与 y 两行。
	for y := blockSize; y < h; y += blockSize {
		for x := 0; x < w; x++ {
			d := absInt(int(pix[y*w+x]) - int(pix[(y-1)*w+x]))
			if d > s.Horizontal {
				s.Horizontal = d
			}
		}
	}
	s.Max = s.Vertical
	if s.Horizontal > s.Max {
		s.Max = s.Horizontal
	}
	return s
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
