// Package fixture 提供确定性的合成灰度图，供测试与演示共用。
// 不读取 testdata/golden，也不改动任何基准字节；刻意不依赖 api/quant，
// 以免与 internal/quant 的包内测试形成导入环。
package fixture

// Width / Height 是合成图的画幅。
const (
	Width  = 160
	Height = 120
)

// Image 返回一张确定性合成灰度图的画幅与像素。
// 分布刻意做成三段偏斜：左侧渐变、上中部暗带、中下部亮带，
// 用来让条件期望重建明显优于中点重建。
func Image() (w, h int, pix []uint8) {
	w, h = Width, Height
	pix = make([]uint8, w*h)
	rng := uint32(0x12345678)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := gradient(x)
			switch {
			case y >= 18 && y < 34:
				v = 8 + (x*5+y)%23
			case y >= 52 && y < 70:
				v = 120 + (x*3+y)%37
			case y >= 92 && y < 112:
				v = 232 + (x*7+y)%24
			}
			rng = rng*1664525 + 1013904223
			if rng>>28 == 0 { // 约 1/16 的像素加 -2..+2 抖动
				v += int((rng>>16)%5) - 2
			}
			pix[y*w+x] = clamp(v)
		}
	}
	// 把理想斜坡按 2x2 块平均，制造大量非零计数（统计更细），
	// 同时消除 8 位斜坡每像素只取整一次的退化。
	return w, h, boxBlur(pix, w, h)
}

// boxBlur 对每个 2x2 块取均值，输出与输入等大；边缘像素保持原值。
func boxBlur(pix []uint8, w, h int) []uint8 {
	out := make([]uint8, len(pix))
	copy(out, pix)
	for y := 0; y+1 < h; y += 2 {
		for x := 0; x+1 < w; x += 2 {
			s := int(pix[y*w+x]) + int(pix[y*w+x+1]) +
				int(pix[(y+1)*w+x]) + int(pix[(y+1)*w+x+1])
			v := uint8((s + 2) / 4)
			out[y*w+x] = v
			out[y*w+x+1] = v
			out[(y+1)*w+x] = v
			out[(y+1)*w+x+1] = v
		}
	}
	return out
}

func gradient(x int) int {
	return x * 255 / (Width - 1)
}

func clamp(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}
