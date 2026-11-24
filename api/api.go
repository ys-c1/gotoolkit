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
	Format string // pgm | bmp | rle，默认 pgm
	Gain   int    // 亮度增益百分比，0 表示不调整，默认 0

	// KeyframeInterval 是多帧差分序列的关键帧间隔 K：第 0,K,2K,... 帧为关键帧。
	// K=1 表示全关键帧，逐帧走既有无损格式且产物与旧实现逐字节一致；0 按默认值 1 处理。
	KeyframeInterval int

	// QuantStep 是 P 帧相邻原始帧差分的最近邻量化步长 q，默认 1（恒等/无损）。
	// 每像素每阶的最大量化误差为 floor(q/2)。仅对 EncodeSequence 产生的 GTD1 负载生效。
	QuantStep int
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
	return Options{Format: "pgm", Gain: 0, KeyframeInterval: 1, QuantStep: 1}
}

func (o Options) validate() error {
	switch o.Format {
	case "pgm", "bmp", "rle":
	default:
		return fmt.Errorf("%w: %q", ErrUnknownFormat, o.Format)
	}
	if o.Gain < -100 || o.Gain > 100 {
		return fmt.Errorf("%w: Gain=%d 超出 [-100,100]", ErrBadOption, o.Gain)
	}
	if o.KeyframeInterval < 0 {
		return fmt.Errorf("%w: KeyframeInterval=%d 不能为负", ErrBadOption, o.KeyframeInterval)
	}
	if o.QuantStep < 0 || o.QuantStep > 65535 {
		return fmt.Errorf("%w: QuantStep=%d 超出 [0,65535]", ErrBadOption, o.QuantStep)
	}
	return nil
}

// chainOptions 把零值规格化为多帧链路默认值。
func (o Options) chainOptions() Options {
	if o.KeyframeInterval == 0 {
		o.KeyframeInterval = 1
	}
	if o.QuantStep == 0 {
		o.QuantStep = 1
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
	default:
		return codec.EncodeRLE(src.W, src.H, src.Pix)
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
