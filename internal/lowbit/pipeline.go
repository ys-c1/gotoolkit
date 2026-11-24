package lowbit

import "sort"

// State 是整条流水线对外暴露的中间结果，全部是 0..255 字节，便于直接落盘查看。
type State struct {
	W, H int
	Bits int
	Q    int
	// Kernel 是抖动核名称；DitherOn 说明本轮是否真的抖动。
	Kernel   string
	DitherOn bool

	// Quant 是重建后的灰度（无抖动时为纯量化，有抖动时含抖动后的量化电平）。
	Quant []uint8
	// Codes 是每个像素的码值（0..2^d-1），与 Quant 一一对应。
	Codes []int

	// ThumbW/ThumbH 是 4:1 缩略尺寸；Thumb 是缩略后重新量化的灰度。
	ThumbW, ThumbH int
	Thumb          []uint8
	ThumbCodes     []int

	// PaletteBefore 是缩略前（原图）聚出的调色板，PaletteAfter 是缩略后聚出的。
	// 索引表在缩略后重建，见 IndicesAfter。
	PaletteBefore []uint8
	PaletteAfter  []uint8

	// IndicesBefore 是原图逐像素对 PaletteBefore 的索引（宽 W）。
	// IndicesAfter 是缩略图逐像素对 PaletteAfter 的索引（宽 ThumbW）。
	IndicesBefore []uint8
	IndicesAfter  []uint8

	// IndexBits 是索引表每项的位宽，等于 Bits（条目数 2^d）。
	IndexBits int
}

// Image 是流水线的输入。
type Image struct {
	W, H int
	Pix  []uint8
}

// Pipeline 执行三处联动，返回全部中间结果。
// 全部关闭时应由调用方走原路径；这里假定 at least one 打开。
func Pipeline(img *Image, p Params, k *Kernel, ditherOn bool) *State {
	st := &State{
		W: img.W, H: img.H, Bits: p.Bits, Q: p.Q, DitherOn: ditherOn,
		Quant:     make([]uint8, len(img.Pix)),
		Codes:     make([]int, len(img.Pix)),
		IndexBits: p.Bits,
	}
	renorm := p
	switch {
	case ditherOn && k != nil && k.Kind == Ordered:
		st.Kernel = k.Name
		st.Quant, st.Codes = ordered(img, renorm, k)
	case ditherOn && k != nil:
		st.Kernel = k.Name
		// 抖动核先归一到 1，使乘法不会把误差放大或缩小。
		q8, err := k.Q8()
		if err != nil {
			// 归一失败只能在 api 层被拦下；这里退化为纯量化以保持总函数性。
			st.Quant, st.Codes = quantize(img, renorm)
			break
		}
		st.Quant, st.Codes = diffuse(img, renorm, k, q8)
	default:
		st.Quant, st.Codes = quantize(img, renorm)
	}

	st.ThumbW, st.ThumbH = (img.W+3)/4, (img.H+3)/4
	st.Thumb, st.ThumbCodes = thumbnail(st.Quant, img.W, img.H, renorm)

	st.PaletteBefore = paletteOf(st.Quant, p.PaletteSize)
	st.PaletteAfter = paletteOf(st.Thumb, p.PaletteSize)
	st.IndicesBefore = indexOf(st.Quant, st.PaletteBefore)
	st.IndicesAfter = indexOf(st.Thumb, st.PaletteAfter)
	return st
}

// BaseQuant 是纯量化，供全关时的对照统计使用。
func BaseQuant(img *Image, p Params) ([]uint8, []int) {
	return quantize(img, p)
}

func quantize(img *Image, p Params) ([]uint8, []int) {
	out := make([]uint8, len(img.Pix))
	codes := make([]int, len(img.Pix))
	for i, v := range img.Pix {
		c := p.Code(int(v))
		codes[i] = c
		out[i] = uint8(p.ReconstructCode(c))
	}
	return out, codes
}

// diffuse 按核把量化误差扩散到尚未处理的邻居。核已按 Q8 归一。
func diffuse(img *Image, p Params, k *Kernel, q8 []int) ([]uint8, []int) {
	work := make([]int, len(img.Pix))
	for i, v := range img.Pix {
		work[i] = int(v)
	}
	w, h := img.W, img.H
	c := k.N / 2
	out := make([]uint8, len(img.Pix))
	codes := make([]int, len(img.Pix))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			code := p.Code(work[i])
			rec := p.ReconstructCode(code)
			codes[i] = code
			out[i] = uint8(rec)
			e := work[i] - rec
			if e == 0 {
				continue
			}
			for dy := 0; dy < k.N; dy++ {
				for dx := 0; dx < k.N; dx++ {
					wt := q8[dy*k.N+dx]
					if wt == 0 {
						continue
					}
					nx, ny := x+dx-c, y+dy-c
					if nx < 0 || nx >= w || ny < 0 || ny >= h {
						continue
					}
					work[ny*w+nx] += e * wt / Q8One
				}
			}
		}
	}
	return out, codes
}

// ordered 按有序阈值矩阵抖动：阈值在 (-0.5,0.5) 之间，单位为 q。
// Bayer 4x4 的原始值 0..15 先减 7.5 再除以 16，得到均值 0 的归一阈值。
func ordered(img *Image, p Params, k *Kernel) ([]uint8, []int) {
	n := k.N
	sum := k.Sum()
	out := make([]uint8, len(img.Pix))
	codes := make([]int, len(img.Pix))
	for y := 0; y < img.H; y++ {
		for x := 0; x < img.W; x++ {
			i := y*img.W + x
			raw := k.W[(y%n)*n+(x%n)]
			// 阈值 t = (raw+0.5)/sum - 0.5 ∈ (-0.5,0.5)；以 q 为单位。
			// 用整数运算：偏移 = ((2*raw+1)*256/sum - 256) * q / 256
			frac := (2*raw+1)*Q8One/sum - Q8One // ∈ [-255,255] 近似 2t*256
			adj := int(img.Pix[i]) + frac*p.Q/(2*Q8One)
			code := p.Code(adj)
			rec := p.ReconstructCode(code)
			codes[i] = code
			out[i] = uint8(rec)
		}
	}
	return out, codes
}

// thumbnail 以 4x4 块均值缩略，然后再走一遍同一量化口径。
func thumbnail(pix []uint8, w, h int, p Params) ([]uint8, []int) {
	tw, th := (w+3)/4, (h+3)/4
	out := make([]uint8, tw*th)
	codes := make([]int, tw*th)
	for ty := 0; ty < th; ty++ {
		for tx := 0; tx < tw; tx++ {
			sum, cnt := 0, 0
			for y := ty * 4; y < ty*4+4 && y < h; y++ {
				for x := tx * 4; x < tx*4+4 && x < w; x++ {
					sum += int(pix[y*w+x])
					cnt++
				}
			}
			mean := 0
			if cnt > 0 {
				mean = (sum + cnt/2) / cnt
			}
			c := p.Code(mean)
			codes[ty*tw+tx] = c
			out[ty*tw+tx] = uint8(p.ReconstructCode(c))
		}
	}
	return out, codes
}

// paletteOf 用直方图贪心取至多 n 个条目。
func paletteOf(pix []uint8, n int) []uint8 {
	pal := GreedyPalette(pix, n)
	out := make([]uint8, len(pal))
	for i, v := range pal {
		out[i] = uint8(v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// indexOf 把每个像素映射成最近调色板条目下标。
func indexOf(pix []uint8, pal []uint8) []uint8 {
	pi := make([]int, len(pal))
	for i, v := range pal {
		pi[i] = int(v)
	}
	out := make([]uint8, len(pix))
	for i, v := range pix {
		out[i] = uint8(NearestIndex(pi, int(v)))
	}
	return out
}
