// Package api 是 gotoolkit 唯一的对外入口。
// 调用方只能看到本包的类型与函数；具体格式实现放在 internal 下，外部不可引用。
package api

import (
	"errors"
	"fmt"

	"gotoolkit/internal/codec"
	"gotoolkit/internal/hybrid"
)

// Options 是所有编解码参数的公共入口。
// 新增可配参数必须加在这里并写明默认值，否则调用方无法设置。
type Options struct {
	Format string // pgm | bmp | rle | hybrid，默认 pgm
	Gain   int    // 亮度增益百分比，0 表示不调整，默认 0
}

// Image 是解码后的灰度图。
type Image struct {
	W, H int
	Pix  []uint8
}

var (
	ErrUnknownFormat = errors.New("未知格式")
	ErrBadSize       = errors.New("画幅必须为正")
	ErrBadOption     = errors.New("参数越界")
	ErrTruncated     = errors.New("数据被截断")
)

// DefaultOptions 返回默认参数。
func DefaultOptions() Options { return Options{Format: "pgm", Gain: 0} }

func (o Options) validate() error {
	switch o.Format {
	case "pgm", "bmp", "rle", "hybrid":
	default:
		return fmt.Errorf("%w: %q", ErrUnknownFormat, o.Format)
	}
	if o.Gain < -100 || o.Gain > 100 {
		return fmt.Errorf("%w: Gain=%d 超出 [-100,100]", ErrBadOption, o.Gain)
	}
	return nil
}

// Encode 把灰度图编码成指定格式。
func Encode(img *Image, opt Options) ([]byte, error) {
	if img == nil {
		return nil, ErrBadSize
	}
	if img.W <= 0 || img.H <= 0 {
		return nil, ErrBadSize
	}
	if len(img.Pix) != img.W*img.H {
		return nil, ErrBadSize
	}
	if err := opt.validate(); err != nil {
		return nil, err
	}
	src := img
	if opt.Gain != 0 {
		src = applyGain(img, opt.Gain)
	}
	switch opt.Format {
	case "pgm":
		return codec.EncodePGM(src.W, src.H, src.Pix)
	case "bmp":
		return codec.EncodeBMP(src.W, src.H, src.Pix)
	case "hybrid":
		// 混合编码的分区按模型阈值自动决定，不接受外部阈值。
		if src.W%hybrid.Block != 0 || src.H%hybrid.Block != 0 {
			return nil, fmt.Errorf("%w: 混合编码要求宽高为 %d 的正整数倍", ErrBadSize, hybrid.Block)
		}
		return hybrid.Encode(src.W, src.H, src.Pix)
	default:
		return codec.EncodeRLE(src.W, src.H, src.Pix)
	}
}

// EncodeHybrid 是带体积约束的混合编码。
//
// 它先尝试 GTH1 分区码流；若其长度没有严格小于三种单一格式中的最小者，则回退为
// 该单一格式的原始字节。因此返回体的长度对任意输入都 ≤ min(pgm, bmp, rle)。
// used 为实际采用的形式："hybrid" | "pgm" | "bmp" | "rle"。
//
// 结构性说明：16×16 块下 GTH1 最坏为 9+⌈N/8⌉+F+128T ≤ 9+128.125·N 字节，
// 而 PGM 为 15+256·N、BMP 更大，故混合码流恒小于 PGM/BMP；只有 RLE 可能更小，
// 因此回退比较是必要的。
func EncodeHybrid(img *Image, opt Options) (body []byte, used string, err error) {
	if img == nil || img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return nil, "", ErrBadSize
	}
	if img.W%hybrid.Block != 0 || img.H%hybrid.Block != 0 {
		return nil, "", fmt.Errorf("%w: 混合编码要求宽高为 %d 的正整数倍", ErrBadSize, hybrid.Block)
	}
	if opt.Gain < -100 || opt.Gain > 100 {
		return nil, "", fmt.Errorf("%w: Gain=%d 超出 [-100,100]", ErrBadOption, opt.Gain)
	}
	src := img
	if opt.Gain != 0 {
		src = applyGain(img, opt.Gain)
	}
	hyb, err := hybrid.Encode(src.W, src.H, src.Pix)
	if err != nil {
		return nil, "", err
	}
	best, bestName := hyb, "hybrid"
	for _, f := range []string{"pgm", "bmp", "rle"} {
		var b []byte
		switch f {
		case "pgm":
			b, err = codec.EncodePGM(src.W, src.H, src.Pix)
		case "bmp":
			b, err = codec.EncodeBMP(src.W, src.H, src.Pix)
		default:
			b, err = codec.EncodeRLE(src.W, src.H, src.Pix)
		}
		if err != nil {
			return nil, "", err
		}
		if len(b) < len(best) {
			best, bestName = b, f
		}
	}
	return best, bestName, nil
}

// HybridAnalysis 是混合编码的分区与失真报告。
type HybridAnalysis = hybrid.Analysis

// AnalyzeHybrid 对灰度图做混合分区分析（不写码流）：
// 逐块统计、按模型阈值分区、给出阈值扫描曲线与两类区域的失真/色块数。
func AnalyzeHybrid(img *Image) (*HybridAnalysis, error) {
	if img == nil || img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return nil, ErrBadSize
	}
	if img.W%hybrid.Block != 0 || img.H%hybrid.Block != 0 {
		return nil, fmt.Errorf("%w: 混合编码要求宽高为 %d 的正整数倍", ErrBadSize, hybrid.Block)
	}
	return hybrid.Analyze(img.W, img.H, img.Pix), nil
}

// Decode 把字节流解码成灰度图。
func Decode(data []byte, opt Options) (*Image, error) {
	if len(data) == 0 {
		return nil, ErrTruncated
	}
	if err := opt.validate(); err != nil {
		return nil, err
	}
	var w, h int
	var pix []uint8
	var err error
	switch opt.Format {
	case "pgm":
		w, h, pix, err = codec.DecodePGM(data)
	case "bmp":
		w, h, pix, err = codec.DecodeBMP(data)
	case "hybrid":
		// 混合模式可能因体积约束回退为单一格式，解码端按魔数分派。
		// 四种前缀互不冲突：GTH1 / GTR1 为 4 字节，P5 / BM 为 2 字节。
		switch {
		case len(data) >= 4 && string(data[:4]) == "GTH1":
			w, h, pix, err = hybrid.Decode(data)
		case len(data) >= 4 && string(data[:4]) == "GTR1":
			w, h, pix, err = codec.DecodeRLE(data)
		case len(data) >= 2 && string(data[:2]) == "P5":
			w, h, pix, err = codec.DecodePGM(data)
		case len(data) >= 2 && string(data[:2]) == "BM":
			w, h, pix, err = codec.DecodeBMP(data)
		default:
			err = fmt.Errorf("%w: 混合模式无法识别输入", ErrUnknownFormat)
		}
	default:
		w, h, pix, err = codec.DecodeRLE(data)
	}
	if err != nil {
		return nil, err
	}
	img := &Image{W: w, H: h, Pix: pix}
	if opt.Gain != 0 {
		img = applyGain(img, -opt.Gain)
	}
	return img, nil
}

func applyGain(img *Image, gain int) *Image {
	out := &Image{W: img.W, H: img.H, Pix: make([]uint8, len(img.Pix))}
	for i, v := range img.Pix {
		x := int(v) + int(v)*gain/100
		if x < 0 {
			x = 0
		}
		if x > 255 {
			x = 255
		}
		out.Pix[i] = uint8(x)
	}
	return out
}
