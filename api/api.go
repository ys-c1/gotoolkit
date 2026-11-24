// Package api 是 gotoolkit 唯一的对外入口。
// 调用方只能看到本包的类型与函数；具体格式实现放在 internal 下，外部不可引用。
package api

import (
	"errors"
	"fmt"

	"gotoolkit/internal/codec"
)

// Options 是所有编解码参数的公共入口。
// 新增可配参数必须加在这里并写明默认值，否则调用方无法设置。
type Options struct {
	Format string // pgm | bmp | rle | rle2，默认 pgm
	Gain   int    // 亮度增益百分比，0 表示不调整，默认 0

	// rle2Tier 仅在 Format=="rle2" 时生效，取 1..3，对应三档亮度系数 K。
	// 0 视为默认档 1。该字段小写：档位语义由 codec 内部定义，不进入公共契约。
	rle2Tier int
}

// WithRLE2Tier 返回带上 rle2 档位的 Options（1..3）。
func (o Options) WithRLE2Tier(tier int) Options {
	o.rle2Tier = tier
	return o
}

// RLE2Tier 返回当前 rle2 档位（未设置时为 0）。
func (o Options) RLE2Tier() int { return o.rle2Tier }

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
	case "pgm", "bmp", "rle", "rle2":
	default:
		return fmt.Errorf("%w: %q", ErrUnknownFormat, o.Format)
	}
	if o.Gain < -100 || o.Gain > 100 {
		return fmt.Errorf("%w: Gain=%d 超出 [-100,100]", ErrBadOption, o.Gain)
	}
	if o.rle2Tier < 0 || o.rle2Tier > 3 {
		return fmt.Errorf("%w: rle2Tier=%d 超出 [0,3]", ErrBadOption, o.rle2Tier)
	}
	return nil
}

// rle2TierOr 返回有效档位：未设置（0）时取默认档 1。
func (o Options) rle2TierOr() int {
	if o.rle2Tier == 0 {
		return 1
	}
	return o.rle2Tier
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
		body, err := codec.EncodeRLE2(src.W, src.H, src.Pix, opt.rle2TierOr(), nil)
		return body, err
	}
}

// RLE2Report 是 rle2 编码的分块统计，字段语义与 codec.RLE2Stats 对应。
type RLE2Report struct {
	Tier            int
	K               int
	BlockCount      int
	EscapeBlocks    int
	RLEBlocks       int
	SplitCount      int
	MaxBlock        int
	EntropyBoundRaw int
	PayloadBytes    int
	TotalBytes      int
	WorstRatio      float64
}

// EncodeRLE2WithStats 以指定档位（1..3）编码并返回完整流与分块统计。
func EncodeRLE2WithStats(img *Image, tier int) ([]byte, RLE2Report, error) {
	if img == nil || img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return nil, RLE2Report{}, ErrBadSize
	}
	if tier < 1 || tier > 3 {
		return nil, RLE2Report{}, fmt.Errorf("%w: tier=%d 超出 [1,3]", ErrBadOption, tier)
	}
	var st codec.RLE2Stats
	body, err := codec.EncodeRLE2(img.W, img.H, img.Pix, tier, &st)
	if err != nil {
		return nil, RLE2Report{}, err
	}
	return body, RLE2Report{
		Tier:            st.Tier,
		K:               st.K,
		BlockCount:      st.BlockCount,
		EscapeBlocks:    st.EscapeBlocks,
		RLEBlocks:       st.RLEBlocks,
		SplitCount:      st.SplitCount,
		MaxBlock:        st.MaxBlock,
		EntropyBoundRaw: st.EntropyBoundRaw,
		PayloadBytes:    st.PayloadBytes,
		TotalBytes:      st.TotalBytes,
		WorstRatio:      st.WorstRatio,
	}, nil
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
	case "rle":
		w, h, pix, err = codec.DecodeRLE(data)
	default:
		w, h, pix, err = codec.DecodeRLE2(data)
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
