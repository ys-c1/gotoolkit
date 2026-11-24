package quantfb

import "math"

// Gray 是包内使用的灰度图（与 api.Image 同构，避免 internal 反向依赖）。
type Gray struct {
	W, H int
	Pix  []uint8 // 行优先，长度 W*H
}

// NewGray 创建并校验尺寸。
func NewGray(w, h int) *Gray {
	return &Gray{W: w, H: h, Pix: make([]uint8, w*h)}
}

func clampByte(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(math.Round(v))
}

// GammaGradient 是 256×128 的 gamma=2.2 水平渐变（平滑暗部最容易暴露低频带痕）。
func GammaGradient() *Gray {
	const W, H = 256, 128
	g := NewGray(W, H)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			u := float64(x) / (W - 1)
			g.Pix[y*W+x] = clampByte(math.Round(255 * math.Pow(u, 2.2)))
		}
	}
	return g
}

// LinearRamp 是 256×64 的线性水平灰阶。
func LinearRamp() *Gray {
	const W, H = 256, 64
	g := NewGray(W, H)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			g.Pix[y*W+x] = clampByte(math.Round(255 * float64(x) / (W - 1)))
		}
	}
	return g
}

// FlatField 是 W×H 的常数灰场（Floyd–Steinberg 蠕虫纹的经典试片）。
func FlatField(v uint8) *Gray {
	const W, H = 256, 128
	g := NewGray(W, H)
	for i := range g.Pix {
		g.Pix[i] = v
	}
	return g
}

// hash01 是确定性的无符号哈希噪声，返回 [0,1)，无需外部随机源。
func hash01(x, y int) float64 {
	n := uint32(x)*374761393 + uint32(y)*668265263 + 0x9e3779b9
	n = (n ^ (n >> 13)) * 1274126177
	n ^= n >> 16
	return float64(n&0xffffff) / float64(1<<24)
}

// Chart 是主评测试片：256×128 混合内容（缓变基线 + 二维周期 +
// 三个柔边斑 + ±2 确定性细噪声），水平与二维低频特性都能被考察。
// 所有数值在代码中冻结，保证报告可重复、测试可锁定。
func Chart() *Gray {
	const W, H = 256, 128
	g := NewGray(W, H)
	blobs := []struct {
		cx, cy, r, a float64
	}{
		{70, 50, 26, 60},
		{180, 70, 34, -50},
		{140, 30, 18, 40},
	}
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			xx, yy := float64(x), float64(y)
			v := 110 + 80*xx/(W-1)
			v += 30 * math.Sin(2*math.Pi*xx/90) * math.Cos(2*math.Pi*yy/70)
			for _, b := range blobs {
				dx, dy := xx-b.cx, yy-b.cy
				v += b.a * math.Exp(-(dx*dx+dy*dy)/(2*b.r*b.r))
			}
			v += 2 * (hash01(x, y)*2 - 1)
			g.Pix[y*W+x] = clampByte(v)
		}
	}
	return g
}

// PseudoRandom 生成 W×H 的确定性伪随机灰图，用于压力测试误差界。
func PseudoRandom(w, h int, seed uint32) *Gray {
	g := NewGray(w, h)
	s := seed
	for i := range g.Pix {
		s = s*1664525 + 1013904223
		g.Pix[i] = uint8((s >> 16) & 0xff)
	}
	return g
}
