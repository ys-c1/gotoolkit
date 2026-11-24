// Package api 是 gotoolkit 唯一的对外入口。
// 调用方只能看到本包的类型与函数；具体格式实现放在 internal 下，外部不可引用。
package api

import (
	"errors"
	"fmt"
	"math"

	"gotoolkit/internal/codec"
)

// Options 是所有编解码参数的公共入口（即本仓库的 Parameters 结构体）。
// 新增可配参数必须加在这里并写明默认值，否则调用方无法设置。
type Options struct {
	Format string // pgm | bmp | rle，默认 pgm
	Gain   int    // 亮度增益百分比，0 表示不调整，默认 0

	// DitherStrength 是误差扩散强度（乘在每条误差上的系数），默认 0。
	// 0 表示不做抖动，编码输出与三份基准逐字节一致；负数、NaN、Inf 报错。
	// 抖动只在编码侧发生，解码侧不可逆，故 Decode 传非零值会报错。
	DitherStrength float64
	// DitherKernel 是误差扩散核，默认零值（未设置）。
	// 强度为 0 时可以不设；强度为正时必须设置且通过 Validate。
	// 内置核用 FloydSteinbergKernel() / AtkinsonKernel() 或 LookupDitherKernel。
	DitherKernel DitherKernel
	// DitherLevels 是抖动的目标色阶数，默认 0 表示取 DitherLevelsDefault(8)。
	// 显式给值必须在 [2,256]；输出仍是 8 位容器，色阶数只影响画质不影响体积。
	DitherLevels int
}

// ditherLevels 返回实际使用的色阶数，0 落到缺省值。
func (o Options) ditherLevels() int {
	if o.DitherLevels == 0 {
		return DitherLevelsDefault
	}
	return o.DitherLevels
}

// Parameters 是 Options 的同义名。任务里说的“参数结构体”就是它；
// 保留 Options 这个名字是为了不破坏既有调用方。
type Parameters = Options

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
	case "pgm", "bmp", "rle":
	default:
		return fmt.Errorf("%w: %q", ErrUnknownFormat, o.Format)
	}
	if o.Gain < -100 || o.Gain > 100 {
		return fmt.Errorf("%w: Gain=%d 超出 [-100,100]", ErrBadOption, o.Gain)
	}
	if math.IsNaN(o.DitherStrength) || math.IsInf(o.DitherStrength, 0) || o.DitherStrength < 0 {
		return fmt.Errorf("%w: DitherStrength=%v 必须是非负有限数", ErrDitherStrength, o.DitherStrength)
	}
	if o.DitherLevels == 0 {
		// 缺省值，合法；非零时校验区间。
	} else if o.DitherLevels < 2 || o.DitherLevels > 256 {
		return fmt.Errorf("%w: DitherLevels=%d 必须在 [2,256]", ErrBadOption, o.DitherLevels)
	}
	if o.DitherStrength > 0 {
		if o.DitherKernel.IsZero() {
			return fmt.Errorf("%w: DitherStrength=%v > 0 但未设置 DitherKernel", ErrDitherKernel, o.DitherStrength)
		}
		if err := o.DitherKernel.Validate(); err != nil {
			return err
		}
	} else if !o.DitherKernel.IsZero() {
		// 强度为 0 但给了核：核本身仍须合法，免得坏参数被静默忽略。
		if err := o.DitherKernel.Validate(); err != nil {
			return err
		}
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
	if opt.DitherStrength > 0 {
		dithered, err := DitherWithLevels(src, opt.DitherStrength, opt.DitherKernel, opt.ditherLevels())
		if err != nil {
			return nil, err
		}
		src = dithered
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
	// 抖动是有损的编码侧变换，没有可逆形式；解码时传非零强度是调用方
	// 弄错了方向，直接拦下，免得“看起来生效了其实没做”。
	if opt.DitherStrength != 0 {
		return nil, fmt.Errorf("%w: 抖动只在编码侧生效，解码不支持 DitherStrength=%v",
			ErrDitherStrength, opt.DitherStrength)
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
