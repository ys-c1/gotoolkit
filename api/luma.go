package api

import (
	"fmt"

	"gotoolkit/internal/expand"
)

// LumaCoefficients 返回三通道展开系数 k_c = Num/Den，按 R、G、B 排列。
// 调用方可以用 Num、Den 独立复算：q_c = round(g·Num_c/Den)，再截到 255。
// Value 只是同值的浮点展示，不参与计算。
func LumaCoefficients() [3]Coefficient {
	cs := expand.Coefficients()
	r, g, b := expand.CoefficientsFloat()
	return [3]Coefficient{
		{Channel: cs[0].Channel, Num: cs[0].Num, Den: cs[0].Den, Value: r},
		{Channel: cs[1].Channel, Num: cs[1].Num, Den: cs[1].Den, Value: g},
		{Channel: cs[2].Channel, Num: cs[2].Num, Den: cs[2].Den, Value: b},
	}
}

// Coefficient 是一个通道的展开系数。
type Coefficient struct {
	Channel string
	Num     int
	Den     int
	Value   float64
}

// String 返回如 kR=299000/446966=0.668955 的复算形式。
func (c Coefficient) String() string {
	return fmt.Sprintf("k%s=%d/%d=%.6f", c.Channel, c.Num, c.Den, c.Value)
}

// LumaWeights 返回亮度权重千分值 (299, 587, 114)。
func LumaWeights() (r, g, b int) { return expand.LumaR, expand.LumaG, expand.LumaB }

// LumaBound 返回灰度 g 的往返误差上界，单位：级。
func LumaBound(g uint8) int { return expand.Bound(g) }

// LumaCollapse 按亮度权重把三通道收回灰度，供调用方复算。
func LumaCollapse(r, g, b uint8) uint8 { return expand.Collapse(r, g, b) }

// LumaMaxBound 返回全 8 位域上的最大上界（= LumaBound(255) = 47）。
func LumaMaxBound() int { return expand.MaxBound }

// LumaUnclippedMax 返回不触发通道截断的最大灰度（194）。
func LumaUnclippedMax() uint8 { return expand.UnclippedMax }

// LumaTier 是一档灰度的验证结果。
type LumaTier struct {
	Lo, Hi    uint8
	Bound     int // 档内逐级上界的最大值
	MaxErr    int // 实测最大绝对误差
	WorstGray uint8
	Samples   int
	OverOne   int // |误差| > 1 的样本数
}

// LumaTiers 对一张图按三档灰度分别统计实测误差，并附上档内理论上界。
func LumaTiers(img *Image) [3]LumaTier {
	type acc struct {
		maxErr  int
		worst   uint8
		samples int
		overOne int
	}
	var a [3]acc
	for _, p := range img.Pix {
		t := tierOf(p)
		r, g, b, _ := expand.Expand(p)
		e := int(expand.Collapse(r, g, b)) - int(p)
		ae := e
		if ae < 0 {
			ae = -ae
		}
		a[t].samples++
		if ae > a[t].maxErr {
			a[t].maxErr, a[t].worst = ae, p
		}
		if ae > 1 {
			a[t].overOne++
		}
	}
	tiers := expand.Tiers()
	var out [3]LumaTier
	for i := range out {
		out[i] = LumaTier{
			Lo: tiers[i].Lo, Hi: tiers[i].Hi, Bound: tiers[i].Bound,
			MaxErr: a[i].maxErr, WorstGray: a[i].worst,
			Samples: a[i].samples, OverOne: a[i].overOne,
		}
	}
	return out
}

func tierOf(g uint8) int {
	switch {
	case g <= 85:
		return 0
	case g <= 170:
		return 1
	default:
		return 2
	}
}

// LumaOffender 是一个往返误差超限（|误差| > 1）的像素。
type LumaOffender struct {
	X, Y     int
	Gray     uint8
	R, G, B  uint8
	Decoded  uint8
	Err      int
	Bound    int
	ClippedR bool
	ClippedG bool
	ClippedB bool
}

// ClipNote 说明该像素哪些通道发生了截断。
func (o LumaOffender) ClipNote() string {
	s := ""
	if o.ClippedR {
		s += "R"
	}
	if o.ClippedG {
		s += "G"
	}
	if o.ClippedB {
		s += "B"
	}
	if s == "" {
		return "无"
	}
	return s
}

// LumaOffenders 返回整张图上误差超限的像素，按行优先排列。
// 上界按像素的灰度单独取 expand.Bound，而不是整档最大值，这样判定
// 不会因为相邻灰度而上纲上线；同时给出整档上界供对照。
func LumaOffenders(img *Image) []LumaOffender {
	var out []LumaOffender
	for y := 0; y < img.H; y++ {
		for x := 0; x < img.W; x++ {
			p := img.Pix[y*img.W+x]
			r, g, b, clip := expand.Expand(p)
			dec := expand.Collapse(r, g, b)
			e := int(dec) - int(p)
			ae := e
			if ae < 0 {
				ae = -ae
			}
			if ae <= 1 {
				continue
			}
			out = append(out, LumaOffender{
				X: x, Y: y, Gray: p, R: r, G: g, B: b, Decoded: dec,
				Err: e, Bound: expand.Bound(p),
				ClippedR: clip&1 != 0, ClippedG: clip&2 != 0, ClippedB: clip&4 != 0,
			})
		}
	}
	return out
}

// LumaConservation 返回一张图上的亮度守恒实测：
//
//	Σ_pix Y(展开后的 RGB) / N  vs  Σ_pix 灰度 / N
//
// 前者按整数千分权重精确累加（分子/1000），不用浮点，便于复算。
func LumaConservation(img *Image) (graySum, lumaSum int64, n int) {
	for _, p := range img.Pix {
		r, g, b, _ := expand.Expand(p)
		graySum += int64(p)
		lumaSum += int64(expand.LumaR)*int64(r) + int64(expand.LumaG)*int64(g) + int64(expand.LumaB)*int64(b)
	}
	return graySum, lumaSum, len(img.Pix)
}

// 熵的口径：按符号出现次数计算经验熵，单位东西见 EntropyBitPerPixel。
func EntropyBitPerPixel(samples []uint8) float64 { return expand.EntropySamples(samples) }

// FormatHeaderOverhead 返回指定格式在给定画幅下的头部字节数，
// 以及不计像素数据时该格式相对裸像素多出的全部字节（头 + 行补齐）。
//
// 口径：
//   - PGM：ASCII 头 "P5\n<w> <h>\n255\n"，无对齐；
//   - BMP：54 字节头 + 每行补齐到 4 的倍数；
//   - RLE：8 字节头（GTR1 + 宽高），另计游程对。
func FormatHeaderOverhead(format string, w, h int) (header, overhead int) {
	n := w * h
	switch format {
	case "pgm":
		head := len(fmt.Sprintf("P5\n%d %d\n255\n", w, h))
		return head, head
	case "bmp":
		row := (w*3 + 3) &^ 3
		return 54, 54 + row*h - n
	case "rle":
		return 8, 8
	default:
		return 0, 0
	}
}
