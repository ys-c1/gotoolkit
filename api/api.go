// Package api 是 gotoolkit 唯一的对外入口。
// 调用方只能看到本包的类型与函数；具体格式实现放在 internal 下，外部不可引用。
package api

import (
	"errors"
	"fmt"

	"gotoolkit/internal/codec"
	"gotoolkit/internal/dither"
)

// Options 是所有编解码参数的公共入口。
// 新增可配参数必须加在这里并写明默认值，否则调用方无法设置。
type Options struct {
	Format string // pgm | bmp | rle，默认 pgm
	Gain   int    // 亮度增益百分比，0 表示不调整，默认 0
	// Dither 选择有序抖动阈值矩阵："" 或 "off" 表示不抖动；
	// "bayer" | "void-cluster" | "spectral" 选择三种 8×8 矩阵之一。
	// 默认（零值）不抖动，保持原有编解码行为不变。
	Dither string
}

// 抖动矩阵名，与 internal/dither 的选择器对应。
const (
	DitherOff         = "off"
	DitherBayer       = dither.NameBayer
	DitherVoidCluster = dither.NameVoidCluster
	DitherSpectral    = dither.NameSpectral
)

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
	switch o.Dither {
	case "", DitherOff, DitherBayer, DitherVoidCluster, DitherSpectral:
	default:
		return fmt.Errorf("%w: Dither=%q 非法", ErrBadOption, o.Dither)
	}
	return nil
}

// DitherMatrix 返回指定名称的 8×8 阈值矩阵（0..63 的排列）。
// name 取 DitherBayer / DitherVoidCluster / DitherSpectral。
func DitherMatrix(name string) ([dither.Size * dither.Size]int, bool) {
	return dither.Matrix(name)
}

// Dither 对灰度图做有序抖动二值化，返回新图（不改写输入）。
// name 取三种矩阵名之一；矩阵周期平铺，输出像素为 0 或 255。
func Dither(img *Image, name string) (*Image, error) {
	if img == nil || img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return nil, ErrBadSize
	}
	matrix, ok := dither.Matrix(name)
	if !ok {
		return nil, fmt.Errorf("%w: Dither=%q 非法", ErrBadOption, name)
	}
	pix, err := dither.Dither(img.W, img.H, img.Pix, matrix)
	if err != nil {
		return nil, err
	}
	return &Image{W: img.W, H: img.H, Pix: pix}, nil
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
	if opt.Dither != "" && opt.Dither != DitherOff {
		dithered, err := Dither(src, opt.Dither)
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
