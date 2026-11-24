package quality

import "math"

// DefaultSizes 是“五档尺寸”的固定画幅：16/32/64/128/256 见方，逐档翻倍。
// 全部为 4 的倍数，BMP 行宽恰好无需填充，闭式体积可被直接核对；
// 小到能瞬间跑完，大到足以看出体积随像素数的缩放。
var DefaultSizes = [][2]int{
	{16, 16},
	{32, 32},
	{64, 64},
	{128, 128},
	{256, 256},
}

// PatternName 是规范样本图样的名字，写进报告便于复跑说明。
const PatternName = "ramp-shapes-v1"

// Pattern 生成确定性的分辨率无关灰度样本“ramp-shapes-v1”：
// 对角渐变底 + 亮圆 + 明暗两个矩形 + 一条 16 级灰阶条。
// 不使用任何随机源，任何机器、任何时刻生成的字节都相同。
// 图样含有纯 0 像素（左上角黑块与灰阶条端点），因此各量化档的实测最大误差
// 恰好等于 ErrorBound 的最坏值，反解与实测可以严格对账。
func Pattern(w, h int) []uint8 {
	pix := make([]uint8, w*h)
	r := float64(minInt(w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fx := (float64(x) + 0.5) / float64(w)
			fy := (float64(y) + 0.5) / float64(h)

			// 1) 平滑对角渐变底，铺满大部分动态范围。
			v := 46 + 168*(0.62*fx+0.38*fy)

			// 2) 亮圆：圆心 (0.32,0.40)，半径约为短边的 0.20。
			dx := (fx - 0.32) * float64(w) / r
			dy := (fy - 0.40) * float64(h) / r
			if d := math.Hypot(dx, dy); d < 0.20 {
				v = 228 - 260*d
			}

			// 3) 右侧亮矩形（带横向渐变）。
			if fx >= 0.64 && fx <= 0.88 && fy >= 0.18 && fy <= 0.40 {
				v = 120 + 110*fx
			}

			// 4) 底部暗矩形。
			if fx >= 0.12 && fx <= 0.46 && fy >= 0.70 && fy <= 0.90 {
				v = 36 + 60*fy
			}

			// 5) 左上角纯黑方块：保证出现 0 值，且让小画幅也有平坦段。
			if fx < 0.10 && fy < 0.10 {
				v = 0
			}

			// 6) 底部 16 级灰阶条（含纯 0 与近 255），逐档量化时色阶合并清晰可见。
			barTop, barBottom := 0.93, 0.985
			if fy >= barTop && fy <= barBottom {
				level := int(fx * 16)
				if level > 15 {
					level = 15
				}
				v = float64(level * 17) // 0,17,...,255
			}

			if v < 0 {
				v = 0
			} else if v > 255 {
				v = 255
			}
			pix[y*w+x] = uint8(math.Round(v))
		}
	}
	return pix
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
