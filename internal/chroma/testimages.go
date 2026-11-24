package chroma

// 三档验证图：尺寸递增、边缘密度与纹理复杂度递增。
// 每张图都把亮度边缘与色度边缘对齐放置（真实彩色边缘的特征），
// 以暴露双线性重建在跨边缘处的混色发虚。

// Tier 描述一档验证图。
type Tier struct {
	Name string
	W, H int
}

// Tiers 返回三档画幅，全部为偶数以满足 4:2:0 对齐。
func Tiers() []Tier {
	return []Tier{
		{Name: "小档 64x64", W: 64, H: 64},
		{Name: "中档 128x128", W: 128, H: 128},
		{Name: "大档 256x256", W: 256, H: 256},
	}
}

// GenTierImage 生成第 idx 档合成彩图。图案由三部分构成：
//   - 左/右两个高对比纯色半区，制造一条贯穿的竖直强边缘；
//   - 上/下半区不同色温，制造一条水平边缘；
//   - 随档位增多的彩色矩形与对角条带，增加多方向边缘密度。
func GenTierImage(idx int) *RGB {
	t := Tiers()[idx]
	w, h := t.W, t.H
	img := &RGB{W: w, H: h, Pix: make([]uint8, w*h*3)}

	// 四个象限的基底色（冷暖、明暗均不同，保证 Y 与 Cb/Cr 同时跳变）。
	quad := [4][3]uint8{
		{220, 60, 60},  // 左上 亮红
		{40, 80, 220},  // 右上 深蓝
		{60, 200, 90},  // 左下 翠绿
		{230, 220, 70}, // 右下 亮黄
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			qi := 0
			if x >= w/2 {
				qi++
			}
			if y >= h/2 {
				qi += 2
			}
			base := quad[qi]
			// 叠加缓慢亮度渐变，使平坦区不是死值（检验方差退化是否正确回到双线性）。
			shade := uint8((x + y) % 24)
			p := 3 * (y*w + x)
			img.Pix[p] = sat8(float64(base[0]) - float64(shade))
			img.Pix[p+1] = sat8(float64(base[1]) - float64(shade))
			img.Pix[p+2] = sat8(float64(base[2]) - float64(shade))
		}
	}

	// 随档位增加的彩色矩形块（与背景强对比），数量随档位变多。
	blocks := 2 + idx*2
	for b := 0; b < blocks; b++ {
		bw := w / (4 + b)
		bh := h / (5 + b)
		bx := (b * w / blocks) % (w - bw)
		by := (b * h / (blocks + 1)) % (h - bh)
		col := [3]uint8{uint8(40 * b), uint8(120 + 20*b), uint8(200 - 30*b)}
		fillRect(img, bx, by, bw, bh, col)
	}

	// 对角彩色条带，制造 45 度边缘（双线性在斜边上混色最明显）。
	thick := 2 + idx
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			d := x - y
			if d >= 0 && d < thick {
				p := 3 * (y*w + x)
				img.Pix[p], img.Pix[p+1], img.Pix[p+2] = 255, 255, 255
			}
			d2 := (w - 1 - x) - y
			if d2 >= 0 && d2 < thick {
				p := 3 * (y*w + x)
				img.Pix[p], img.Pix[p+1], img.Pix[p+2] = 10, 10, 10
			}
		}
	}
	return img
}

func fillRect(img *RGB, x0, y0, w, h int, col [3]uint8) {
	for y := y0; y < y0+h && y < img.H; y++ {
		for x := x0; x < x0+w && x < img.W; x++ {
			p := 3 * (y*img.W + x)
			img.Pix[p], img.Pix[p+1], img.Pix[p+2] = col[0], col[1], col[2]
		}
	}
}
