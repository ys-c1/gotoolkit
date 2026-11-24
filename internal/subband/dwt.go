// Package subband 实现基于多级二维 Haar 小波变换的子带编码。
// 变换只负责把图像拆成子带并能无损（浮点意义上）重构；
// 比特分配（注水/平分）、量化与比特流打包在同包其它文件中。
//
// 内存布局采用标准 Mallat 原地金字塔：缓冲始终为全幅 w*h，
// 第 l 级只在左上角 cw*ch（cw=w/2^(l-1)）上做一级 Haar，
// 做完后 LL 占左上 cw/2*ch/2，三个高频带占该四分块的其余三块；
// 各级高频带因此落在互不重叠的环形区，逆变换逐级放大即可。
package subband

import "errors"

// Band 是一个子带。
// Name 取 LL/LH/HL/HH，Level 从 1 开始（LL 记为最粗一级 levels）。
// Coef 为该带的变换系数；N 为系数个数；Weight=N/Nimg 是该带占全图的能量权重。
type Band struct {
	Name   string
	Level  int
	Coef   []float64
	N      int
	Weight float64
}

// ForwardDWT 对 w*h 的灰度像素做 levels 级二维 Haar 变换。
// 宽高必须能被 2^levels 整除，否则返回错误。
func ForwardDWT(pix []uint8, w, h, levels int) ([]Band, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	if levels <= 0 {
		return nil, errors.New("变换层数必须为正")
	}
	if w%(1<<uint(levels)) != 0 || h%(1<<uint(levels)) != 0 {
		return nil, errors.New("画幅不能被 2^levels 整除")
	}
	buf := make([]float64, w*h)
	for i, v := range pix {
		buf[i] = float64(v)
	}
	cw, ch := w, h
	for l := 1; l <= levels; l++ {
		haar2D(buf, w, cw, ch)
		cw, ch = cw/2, ch/2
	}
	return extractBands(buf, w, h, levels), nil
}

// InverseDWT 把子带系数重构成像素。bands 必须来自同一画幅同一层数的 ForwardDWT。
func InverseDWT(bands []Band, w, h, levels int) ([]uint8, error) {
	buf := make([]float64, w*h)
	for _, b := range bands {
		off, bw, bh, err := bandRect(w, h, b.Level, b.Name)
		if err != nil {
			return nil, err
		}
		if len(b.Coef) != bw*bh {
			return nil, errors.New("子带系数数与画幅不符")
		}
		for y := 0; y < bh; y++ {
			copy(buf[off+y*w:off+y*w+bw], b.Coef[y*bw:(y+1)*bw])
		}
	}
	// 最粗一级的四分块边长为 w/2^(L-1)，逐级逆变换放大到全图。
	cw, ch := w>>uint(levels-1), h>>uint(levels-1)
	for l := levels; l >= 1; l-- {
		invHaar2D(buf, w, cw, ch)
		cw, ch = cw*2, ch*2
	}
	pix := make([]uint8, w*h)
	for i, v := range buf {
		x := int(v + 0.5) // 最近取整回到 0..255
		if x < 0 {
			x = 0
		}
		if x > 255 {
			x = 255
		}
		pix[i] = uint8(x)
	}
	return pix, nil
}

// haar2D 对 buf 左上角 cw*ch 的区域做一级二维 Haar，按 2×2 块直接拆成四分块：
//
//	LL 左上（行列均低通）、LH 右上（行高通）、HL 左下（列高通）、HH 右下。
//
// 四分块边长 cw/2、ch/2，互不重叠，便于多级原地金字塔与逐块逆变换。
func haar2D(buf []float64, stride, cw, ch int) {
	halfW, halfH := cw/2, ch/2
	tmp := make([]float64, cw*ch)
	for by := 0; by < halfH; by++ {
		for bx := 0; bx < halfW; bx++ {
			x0, x1 := 2*bx, 2*bx+1
			y0, y1 := 2*by, 2*by+1
			a := buf[y0*stride+x0]
			b := buf[y0*stride+x1]
			c := buf[y1*stride+x0]
			d := buf[y1*stride+x1]
			ll := (a + b + c + d) * 0.25
			lh := (a - b + c - d) * 0.25 // 行方向高通
			hl := (a + b - c - d) * 0.25 // 列方向高通
			hh := (a - b - c + d) * 0.25
			// 行主序四分块：LL 左上、LH 右上、HL 左下、HH 右下。
			tmp[by*cw+bx]                 = ll
			tmp[by*cw+halfW+bx]           = lh
			tmp[(halfH+by)*cw+bx]         = hl
			tmp[(halfH+by)*cw+halfW+bx]   = hh
		}
	}
	// 四分块写回缓冲左上角，保持全幅 stride。
	for y := 0; y < ch; y++ {
		copy(buf[y*stride:y*stride+cw], tmp[y*cw:y*cw+cw])
	}
}

// invHaar2D 是 haar2D 的逆，按 2×2 块从四个四分块逐块还原，作用于同一大小区域。
func invHaar2D(buf []float64, stride, cw, ch int) {
	halfW, halfH := cw/2, ch/2
	src := quadrants(buf, stride, cw, ch)
	out := make([]float64, cw*ch)
	for by := 0; by < halfH; by++ {
		for bx := 0; bx < halfW; bx++ {
			ll := src[by*cw+bx]
			lh := src[by*cw+halfW+bx]
			hl := src[(halfH+by)*cw+bx]
			hh := src[(halfH+by)*cw+halfW+bx]
			x0, x1 := 2*bx, 2*bx+1
			y0, y1 := 2*by, 2*by+1
			out[y0*cw+x0] = ll + lh + hl + hh // a
			out[y0*cw+x1] = ll - lh + hl - hh // b
			out[y1*cw+x0] = ll + lh - hl - hh // c
			out[y1*cw+x1] = ll - lh - hl + hh // d
		}
	}
	for y := 0; y < ch; y++ {
		copy(buf[y*stride:y*stride+cw], out[y*cw:y*cw+cw])
	}
}

// quadrants 把缓冲左上角 cw*ch 的四分块连续读出。
func quadrants(buf []float64, stride, cw, ch int) []float64 {
	out := make([]float64, cw*ch)
	for y := 0; y < ch; y++ {
		copy(out[y*cw:y*cw+cw], buf[y*stride:y*stride+cw])
	}
	return out
}

// bandRect 返回第 level 级某子带在全幅缓冲中的偏移与宽高。
// 第 level 级四分块的边长为 w/2^(level-1)，其左上原点为 (0,0)。
// 行变换使列方向左低右高，列变换使行方向上低下高，因此：
// LL 左上、LH（行高通）右上、HL（列高通）左下、HH 右下。
// LL 只存在于最粗一级（level=levels）。
func bandRect(w, h, level int, name string) (off, bw, bh int, err error) {
	bw = w >> uint(level-1)
	bh = h >> uint(level-1)
	halfW, halfH := bw/2, bh/2
	var x0, y0 int
	switch name {
	case "LL":
		x0, y0 = 0, 0
		bw, bh = halfW, halfH
	case "LH": // 行高通：右上
		x0, y0 = halfW, 0
		bw, bh = halfW, halfH
	case "HL": // 列高通：左下
		x0, y0 = 0, halfH
		bw, bh = halfW, halfH
	case "HH": // 右下
		x0, y0 = halfW, halfH
		bw, bh = halfW, halfH
	default:
		return 0, 0, 0, errors.New("未知子带名")
	}
	return y0*w + x0, bw, bh, nil
}

// extractBands 按自然顺序（最粗 LL 起，到最细高频）取出子带。
func extractBands(buf []float64, w, h, levels int) []Band {
	total := w * h
	bands := make([]Band, 0, 1+3*levels)
	for level := levels; level >= 1; level-- {
		names := []string{"LH", "HL", "HH"}
		if level == levels {
			names = []string{"LL", "LH", "HL", "HH"}
		}
		for _, name := range names {
			off, bw, bh, _ := bandRect(w, h, level, name)
			coef := make([]float64, bw*bh)
			for y := 0; y < bh; y++ {
				copy(coef[y*bw:(y+1)*bw], buf[off+y*w:off+y*w+bw])
			}
			bands = append(bands, Band{
				Name:   name,
				Level:  level,
				Coef:   coef,
				N:      bw * bh,
				Weight: float64(bw*bh) / float64(total),
			})
		}
	}
	return bands
}
