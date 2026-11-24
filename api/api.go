// Package api 是 gotoolkit 唯一的对外入口。
// 调用方只能看到本包的类型与函数；具体格式实现放在 internal 下，外部不可引用。
package api

import (
	"errors"
	"fmt"

	"gotoolkit/internal/codec"
	"gotoolkit/internal/subband"
)

// Options 是所有编解码参数的公共入口。
// 新增可配参数必须加在这里并写明默认值，否则调用方无法设置。
type Options struct {
	Format string // pgm | bmp | rle | subband，默认 pgm
	Gain   int    // 亮度增益百分比，0 表示不调整，默认 0

	// 以下仅 Format=subband 时生效。
	SubLevels    int  // Haar 分解层数，默认 2
	SubBudget    int  // 系数比特总预算（不含固定头/边信息），默认每像素 2 比特
	SubWaterfill bool // true=反向注水分配（默认），false=平分（可切回的基线）
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
func DefaultOptions() Options {
	return Options{Format: "pgm", Gain: 0, SubLevels: 2, SubWaterfill: true}
}

func (o Options) validate() error {
	switch o.Format {
	case "pgm", "bmp", "rle", "subband":
	default:
		return fmt.Errorf("%w: %q", ErrUnknownFormat, o.Format)
	}
	if o.Gain < -100 || o.Gain > 100 {
		return fmt.Errorf("%w: Gain=%d 超出 [-100,100]", ErrBadOption, o.Gain)
	}
	if o.Format == "subband" {
		// 解码时层数/预算取自码流，Options 里允许零值占位；仅编码侧要求显式合法。
		if o.SubLevels < 0 || o.SubLevels > 8 {
			return fmt.Errorf("%w: SubLevels=%d 超出 [0,8]", ErrBadOption, o.SubLevels)
		}
		if o.SubBudget < 0 {
			return fmt.Errorf("%w: SubBudget 不能为负", ErrBadOption)
		}
	}
	return nil
}

// withSubbandDefaults 补齐子带参数的零值默认。
func (o Options) withSubbandDefaults(w, h int) Options {
	if o.SubLevels == 0 {
		o.SubLevels = 2
	}
	if o.SubBudget == 0 {
		o.SubBudget = w * h * 2 // 默认 2 bpp
	}
	return o
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
	case "rle":
		return codec.EncodeRLE(src.W, src.H, src.Pix)
	default:
		opt = opt.withSubbandDefaults(src.W, src.H)
		bands, err := subband.ForwardDWT(src.Pix, src.W, src.H, opt.SubLevels)
		if err != nil {
			return nil, err
		}
		stats := make([]subband.Stats, len(bands))
		for i, b := range bands {
			stats[i] = subband.ComputeStats(b)
		}
		var bits []int
		mode := subband.ModeWaterfill
		if opt.SubWaterfill {
			res := subband.Waterfill(stats, opt.SubBudget)
			bits = make([]int, len(res.Details))
			for i, d := range res.Details {
				bits[i] = d.Bits
			}
		} else {
			mode = subband.ModeEqual
			res := subband.EqualAllocation(stats, opt.SubBudget)
			bits = make([]int, len(res.Details))
			for i, d := range res.Details {
				bits[i] = d.Bits
			}
		}
		return subband.EncodeStream(src.Pix, src.W, src.H, opt.SubLevels,
			opt.SubBudget, mode, bits)
	}
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
	if opt.Format == "subband" {
		var levels, budget, mode int
		w, h, levels, budget, mode, pix, err = subband.DecodeStream(data)
		if err != nil {
			return nil, err
		}
		_ = levels
		_ = budget
		_ = mode
	} else {
		switch opt.Format {
		case "pgm":
			w, h, pix, err = codec.DecodePGM(data)
		case "bmp":
			w, h, pix, err = codec.DecodeBMP(data)
		default:
			w, h, pix, err = codec.DecodeRLE(data)
		}
		if err != nil {
			return nil, err
		}
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
