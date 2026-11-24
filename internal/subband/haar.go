// Package subband 实现多级二维 Haar 子带分解、按子带方差的反向注水比特分配、
// 逐带重建与两类失真（低频块状条纹 / 高频边缘毛刺）统计。
//
// 变换采用正交归一 Haar：正变换与逆变换是同一个正交矩阵，
// 因此 Parseval 恒等式成立——像素域误差总能量等于全部系数误差能量之和，
// 按 n_i/N 加权的子带 MSE 之和严格等于整图 MSE（浮点误差范围内）。
package subband

import (
	"errors"
	"fmt"
)

// Band 是一个子带。Kind 取 LL/LH/HL/HH；Level 从 1（最细）到 Levels（最粗）。
// Label 全局唯一，形如 "LL3"、"LH1"。Coef 长度 N=BW*BH。
type Band struct {
	Label string
	Kind  string
	Level int
	BW    int
	BH    int
	N     int
	Coef  []float64
}

// Rect 返回该子带系数在全幅金字塔缓冲中的偏移与宽高。
// 第 level 级变换作用在 w/2^(level-1) 见方的左上区域，其四个四分块各为 w/2^level。
func Rect(w, h, level int, kind string) (off, bw, bh int, err error) {
	bw = w >> uint(level)
	bh = h >> uint(level)
	if bw <= 0 || bh <= 0 {
		return 0, 0, 0, fmt.Errorf("第 %d 级对 %dx%d 画幅过深", level, w, h)
	}
	var x0, y0 int
	switch kind {
	case "LL":
		x0, y0 = 0, 0
	case "LH": // 行（x 方向）高通：右上
		x0, y0 = bw, 0
	case "HL": // 列（y 方向）高通：左下
		x0, y0 = 0, bh
	case "HH":
		x0, y0 = bw, bh
	default:
		return 0, 0, 0, errors.New("未知子带类型: " + kind)
	}
	return y0*w + x0, bw, bh, nil
}

// Decompose 对灰度像素做 levels 级正交归一二维 Haar。
// 宽高必须能被 2^levels 整除。返回顺序为粗到细：
// LL,LH,HL,HH（最粗一级），随后各级 LH,HL,HH。
func Decompose(pix []uint8, w, h, levels int) ([]Band, error) {
	if w <= 0 || h <= 0 || len(pix) != w*h {
		return nil, errors.New("画幅或像素数不匹配")
	}
	if levels <= 0 {
		return nil, errors.New("变换层数必须为正")
	}
	if w%(1<<uint(levels)) != 0 || h%(1<<uint(levels)) != 0 {
		return nil, fmt.Errorf("画幅 %dx%d 不能被 2^%d 整除", w, h, levels)
	}
	buf := make([]float64, w*h)
	for i, v := range pix {
		buf[i] = float64(v)
	}
	cw, ch := w, h
	for l := 1; l <= levels; l++ {
		haar2DNorm(buf, w, cw, ch, false)
		cw, ch = cw/2, ch/2
	}
	return extractBands(buf, w, h, levels), nil
}

// haar2DNorm 对 buf 左上角 stride 行宽下的 cw*ch 区域做一级正交归一 Haar。
// inverse=false 为正变换（相邻 2×2 块 → 四个四分块），
// inverse=true 为逆变换（四个四分块 → 相邻 2×2 块）。
// 归一化 Haar 的正逆数值公式相同（同一正交矩阵自逆），但读写布局相反。
func haar2DNorm(buf []float64, stride, cw, ch int, inverse bool) {
	qhw, qhh := cw/2, ch/2
	tmp := make([]float64, cw*ch)
	if !inverse {
		for by := 0; by < qhh; by++ {
			for bx := 0; bx < qhw; bx++ {
				x0, x1 := 2*bx, 2*bx+1
				y0, y1 := 2*by, 2*by+1
				a := buf[y0*stride+x0]
				b := buf[y0*stride+x1]
				c := buf[y1*stride+x0]
				d := buf[y1*stride+x1]
				vll := (a + b + c + d) * 0.5
				vlh := (a - b + c - d) * 0.5
				vhl := (a + b - c - d) * 0.5
				vhh := (a - b - c + d) * 0.5
				tmp[by*cw+bx] = vll
				tmp[by*cw+qhw+bx] = vlh
				tmp[(qhh+by)*cw+bx] = vhl
				tmp[(qhh+by)*cw+qhw+bx] = vhh
			}
		}
	} else {
		for by := 0; by < qhh; by++ {
			for bx := 0; bx < qhw; bx++ {
				// 从四个四分块的对应位置取 LL/LH/HL/HH。
				vll := buf[by*stride+bx]
				vlh := buf[by*stride+qhw+bx]
				vhl := buf[(qhh+by)*stride+bx]
				vhh := buf[(qhh+by)*stride+qhw+bx]
				a := (vll + vlh + vhl + vhh) * 0.5
				b := (vll - vlh + vhl - vhh) * 0.5
				c := (vll + vlh - vhl - vhh) * 0.5
				d := (vll - vlh - vhl + vhh) * 0.5
				x0, x1 := 2*bx, 2*bx+1
				y0, y1 := 2*by, 2*by+1
				tmp[y0*cw+x0] = a
				tmp[y0*cw+x1] = b
				tmp[y1*cw+x0] = c
				tmp[y1*cw+x1] = d
			}
		}
	}
	for y := 0; y < ch; y++ {
		copy(buf[y*stride:y*stride+cw], tmp[y*cw:y*cw+cw])
	}
}

// Reconstruct 由子带系数逆变换回浮点像素（不裁剪）。
func Reconstruct(bands []Band, w, h, levels int) ([]float64, error) {
	buf := make([]float64, w*h)
	for _, b := range bands {
		off, bw, bh, err := Rect(w, h, b.Level, b.Kind)
		if err != nil {
			return nil, err
		}
		if bw != b.BW || bh != b.BH || len(b.Coef) != b.N {
			return nil, fmt.Errorf("子带 %s 尺寸与画幅不符", b.Label)
		}
		for y := 0; y < bh; y++ {
			copy(buf[off+y*w:off+y*w+bw], b.Coef[y*bw:(y+1)*bw])
		}
	}
	cw, ch := w>>uint(levels-1), h>>uint(levels-1)
	for l := levels; l >= 1; l-- {
		haar2DNorm(buf, w, cw, ch, true)
		cw, ch = cw*2, ch*2
	}
	return buf, nil
}

// ClampPixels 把浮点像素最近取整并夹到 [0,255]。
func ClampPixels(f []float64) []uint8 {
	out := make([]uint8, len(f))
	for i, v := range f {
		x := int(v + 0.5)
		if x < 0 {
			x = 0
		}
		if x > 255 {
			x = 255
		}
		out[i] = uint8(x)
	}
	return out
}

// BandOnlyImage 只保留第 only 个带的系数（其余为零），逆变换出全幅浮点图。
// 用于“各子带单独重建出图”。
func BandOnlyImage(bands []Band, only int, w, h, levels int) ([]float64, error) {
	sparse := make([]Band, len(bands))
	for i, b := range bands {
		nb := b
		nb.Coef = make([]float64, b.N)
		if i == only {
			copy(nb.Coef, b.Coef)
		}
		sparse[i] = nb
	}
	return Reconstruct(sparse, w, h, levels)
}

// BandErrorImage 给定第 i 带的系数量化误差 delta（长度等于该带 N），
// 其余带误差为零，逆变换回像素域误差图。
func BandErrorImage(bands []Band, i int, delta []float64, w, h, levels int) ([]float64, error) {
	sparse := make([]Band, len(bands))
	for j, b := range bands {
		nb := b
		nb.Coef = make([]float64, b.N)
		if j == i {
			if len(delta) != b.N {
				return nil, fmt.Errorf("误差长度 %d 与带 %s 的 %d 不符", len(delta), b.Label, b.N)
			}
			copy(nb.Coef, delta)
		}
		sparse[j] = nb
	}
	return Reconstruct(sparse, w, h, levels)
}

func extractBands(buf []float64, w, h, levels int) []Band {
	total := w * h
	bands := make([]Band, 0, 1+3*levels)
	for level := levels; level >= 1; level-- {
		kinds := []string{"LH", "HL", "HH"}
		if level == levels {
			kinds = []string{"LL", "LH", "HL", "HH"}
		}
		for _, kind := range kinds {
			off, bw, bh, _ := Rect(w, h, level, kind)
			coef := make([]float64, bw*bh)
			for y := 0; y < bh; y++ {
				copy(coef[y*bw:(y+1)*bw], buf[off+y*w:off+y*w+bw])
			}
			bands = append(bands, Band{
				Label: fmt.Sprintf("%s%d", kind, level),
				Kind:  kind,
				Level: level,
				BW:    bw,
				BH:    bh,
				N:     bw * bh,
				Coef:  coef,
			})
		}
	}
	_ = total
	return bands
}

// SizeWeights 返回各带的尺寸权重 n_i/(w*h)。正交归一变换下，
// Σ_i Weight_i * MSE_i 就是整图像素 MSE。
func SizeWeights(bands []Band, w, h int) []float64 {
	out := make([]float64, len(bands))
	for i, b := range bands {
		out[i] = float64(b.N) / float64(w*h)
	}
	return out
}
