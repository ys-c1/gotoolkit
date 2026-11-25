// Package testimg 生成三张确定性的 320×320 灰度测试图。
//
// 全部运算是整数定点（Q8）与 splitmix64 哈希，不使用浮点，因此同一份源码在
// 任何平台、任何 Go 版本上都产生逐字节相同的结果，可作为基准输入。
// 三张图覆盖“平坦区与纹理区混合”的典型形态：
//   - Split   左半缓慢渐变、右半强纹理
//   - Checker 80×80 棋盘，平坦格与纹理格各占一半
//   - Scene   天空/山丘/墙面/道路为平坦区，草地纹理幅度从左到右跨过阈值
package testimg

const Size = 320

type rng struct{ s uint64 }

func newRNG(seed uint64) rng { return rng{s: seed} }

func (r *rng) next() uint64 {
	r.s += 0x9E3779B97F4A7C15
	z := r.s
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// noise8 返回 [0,255] 的均匀整数。
func (r *rng) noise8() int { return int(r.next() >> 56) }

// smoothstep8 把 t∈[0,256) 映射为 t²(3−2t̂)/256² 的 Q8 权重。
func smoothstep8(t int) int {
	if t < 0 {
		t = 0
	}
	if t > 255 {
		t = 255
	}
	// 3t²·256 − 2t³，结果在 [0,256³)；再除以 256²。
	return (t*t*(3*256-2*t) + 128) >> 8
}

func clamp8(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// valueNoise8 生成格点值噪声，双线性 + smoothstep，输出 Q8（0..255）。
func valueNoise8(w, h, cell int, seed uint64) []uint8 {
	gw, gh := w/cell+2, h/cell+2
	g := make([]uint8, gw*gh)
	r := newRNG(seed)
	for i := range g {
		g[i] = uint8(r.noise8())
	}
	out := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		y0 := y / cell
		sy := smoothstep8((y % cell) * 256 / cell)
		for x := 0; x < w; x++ {
			x0 := x / cell
			sx := smoothstep8((x % cell) * 256 / cell)
			a := int(g[y0*gw+x0])
			b := int(g[y0*gw+x0+1])
			c := int(g[(y0+1)*gw+x0])
			d := int(g[(y0+1)*gw+x0+1])
			top := (a*(256-sx) + b*sx) >> 8
			bot := (c*(256-sx) + d*sx) >> 8
			out[y*w+x] = uint8((top*(256-sy) + bot*sy) >> 8)
		}
	}
	return out
}

func hashNoise8(seed uint64, n int) []uint8 {
	r := newRNG(seed)
	out := make([]uint8, n)
	for i := range out {
		out[i] = uint8(r.noise8())
	}
	return out
}

// sgn 把 0..255 的噪声映射到 −128..127。
func sgn(v uint8) int { return int(v) - 128 }

// Split 左半平坦渐变、右半强纹理。
func Split() []uint8 {
	const w, h = Size, Size
	mid := valueNoise8(w, h, 4, 0x5eed01)
	hn := hashNoise8(0x5eed02, w*h)
	pix := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			v := 0
			if x < w/2 {
				// 40→130 的缓坡，16×16 块内只有 4~5 个灰度级。
				v = 40 + 90*x/(w/2)
				v += sgn(hn[i]) / 210 // 约 ±0.6 的逐像素噪声
			} else {
				// 值噪声与逐像素噪声叠加，σ 远大于阈值。
				v = 128 + sgn(mid[i])*140/255 + sgn(hn[i])*90/255
			}
			pix[i] = clamp8(v)
		}
	}
	return pix
}

// Checker 80×80 棋盘格，平坦格与纹理格交替。
func Checker() []uint8 {
	const w, h = Size, Size
	cell := 80
	vn := valueNoise8(w, h, 5, 0xc0ffee01)
	hn := hashNoise8(0xc0ffee02, w*h)
	pix := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			bx, by := x/cell, y/cell
			if (bx+by)%2 == 1 {
				pix[i] = clamp8(128 + sgn(vn[i])*150/255 + sgn(hn[i])*95/255)
			} else {
				// 每个平坦格一个固定电平，只留极轻的传感器噪声。
				pix[i] = clamp8(55 + 30*((bx*3+by*5)%4) + sgn(hn[i])/320)
			}
		}
	}
	return pix
}

// Scene 天空/远山/墙面/道路平坦，草地纹理幅度自左向右增大。
func Scene() []uint8 {
	const w, h = Size, Size
	hill := valueNoise8(w, h, 32, 0x5ceee01)
	grass := valueNoise8(w, h, 3, 0x5ceee02)
	grass2 := valueNoise8(w, h, 2, 0x5ceee03)
	hn := hashNoise8(0x5ceee04, w*h)
	pix := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			v := 0
			switch {
			case y < 130: // 天空：极缓渐变
				v = 205 - 45*y/130 + sgn(hn[i])/260
			case y < 210: // 远山：低频起伏
				v = 100 + int(hill[i])*70/255 + sgn(hn[i])/128
			default: // 地面
				a := (x - 70) * 256 / (w - 70)
				if a < 0 {
					a = 0
				}
				if a > 256 {
					a = 256
				}
				tex := sgn(grass[i])*132/255 + sgn(grass2[i])*88/255 + sgn(hn[i])*80/255
				v = 88 + a*tex/256
				if road := x - (55*y/100 + 40); road > -18 && road < 18 {
					v = 168 + sgn(hn[i])/210
				}
				// 左侧墙面与两排窗
				if x < 120 && y < 300 {
					v = 145 + sgn(hn[i])/128
					for _, wx := range []int{18, 56, 94} {
						for _, wy := range []int{225, 265} {
							if x >= wx && x < wx+18 && y >= wy && y < wy+20 {
								v = 42
							}
						}
					}
				}
			}
			if dx, dy := x-250, y-42; dx*dx+dy*dy < 26*26 {
				v = 236 // 太阳
			}
			pix[i] = clamp8(v)
		}
	}
	return pix
}

// All 返回三张图，键为文件名主干。
func All() map[string][]uint8 {
	return map[string][]uint8{"split": Split(), "checker": Checker(), "scene": Scene()}
}
