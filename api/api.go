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
	Format   string // pgm | bmp | rle，默认 pgm
	Gain     int    // 亮度增益百分比，0 表示不调整，默认 0
	BitDepth int    // 量化位深，取值 1..8；8 为无损（量化掩码关闭），默认 8
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
func DefaultOptions() Options { return Options{Format: "pgm", Gain: 0, BitDepth: 8} }

func (o Options) validate() error {
	switch o.Format {
	case "pgm", "bmp", "rle":
	default:
		return fmt.Errorf("%w: %q", ErrUnknownFormat, o.Format)
	}
	if o.Gain < -100 || o.Gain > 100 {
		return fmt.Errorf("%w: Gain=%d 超出 [-100,100]", ErrBadOption, o.Gain)
	}
	if o.BitDepth < 1 || o.BitDepth > 8 {
		return fmt.Errorf("%w: BitDepth=%d 必须在 [1,8]", ErrBadOption, o.BitDepth)
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
	if opt.BitDepth < 8 {
		src = quantize(src, opt.BitDepth)
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
// 量化在编码侧完成，码流里已是反量化后的 8 位灰度，
// 因此 BitDepth 在解码侧只参与校验，不改变像素。
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

// quantize 把 8 位灰度均匀量化到 bitDepth 位，再反量化回 8 位存储。
//
// 步长为解析反解，非试凑：M = 2^bitDepth 个量化级均匀铺满 [0,255]，
// 相邻级别间距（步长）
//
//	Δ = 255 / (2^bitDepth − 1)
//
// 四舍五入（mid-tread）均匀量化器的最大量化误差为 Δ/2，来源：
// Sayood《Introduction to Data Compression》标量量化一章；
// Gonzalez & Woods《数字图像处理》灰度均匀量化。
// 由目标最大误差 E 反解位深：Δ/2 ≤ E ⟺ 2^bitDepth ≥ 255/(2E) + 1。
//
// 量化 q = round(v·(2^b−1)/255)，反量化 v̂ = round(q·255/(2^b−1))，
// 全部整数运算；bitDepth=8 时逐像素恒等，掩码关闭即无损。
func quantize(img *Image, bitDepth int) *Image {
	m := (1 << bitDepth) - 1 // 量化级数减一
	out := &Image{W: img.W, H: img.H, Pix: make([]uint8, len(img.Pix))}
	for i, v := range img.Pix {
		q := (int(v)*m + 127) / 255
		out.Pix[i] = uint8((q*255 + m/2) / m)
	}
	return out
}
