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

	// 以下两个开关仅对 pgm 生效（头结构支路），默认均为 false。
	ExtHeader bool // 扩展头开关：写入位深、行序标志、校验和三个固定偏移字段
	FlipRows  bool // 行序开关：像素底朝上存储，并在头中置行序标志
}

// Image 是解码后的灰度图。
type Image struct {
	W, H int
	Pix  []uint8
	// Meta 仅 pgm 解码时可能非 nil，记录按文件头还原出的两个开关状态。
	Meta *PGMMeta
}

// PGMMeta 是按 PGM 文件头还原出的开关状态。
type PGMMeta struct {
	ExtHeader bool
	FlipRows  bool
}

var (
	ErrUnknownFormat = errors.New("未知格式")
	ErrBadSize       = errors.New("画幅必须为正")
	ErrBadOption     = errors.New("参数越界")
	ErrTruncated     = codec.ErrTruncated

	// 头结构支路错误码（直接复用 internal/codec 的哨兵，errors.Is 可判定）。
	ErrFieldOutOfRange = codec.ErrFieldOutOfRange // 字段越界（位深非法）
	ErrFlagConflict    = codec.ErrFlagConflict    // 标志冲突（行序标志保留位非零）
	ErrChecksum        = codec.ErrChecksum        // 校验和不匹配
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
	if o.Format != "pgm" && (o.ExtHeader || o.FlipRows) {
		return fmt.Errorf("%w: ExtHeader/FlipRows 仅支持 pgm，当前格式 %q", ErrBadOption, o.Format)
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
		return codec.EncodePGMEx(src.W, src.H, src.Pix, codec.PGMOptions{
			ExtHeader: opt.ExtHeader,
			FlipRows:  opt.FlipRows,
		})
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
	var meta *PGMMeta
	var err error
	switch opt.Format {
	case "pgm":
		var m codec.PGMMeta
		w, h, pix, m, err = codec.DecodePGMEx(data)
		meta = &PGMMeta{ExtHeader: m.ExtHeader, FlipRows: m.FlipRows}
	case "bmp":
		w, h, pix, err = codec.DecodeBMP(data)
	default:
		w, h, pix, err = codec.DecodeRLE(data)
	}
	if err != nil {
		return nil, err
	}
	img := &Image{W: w, H: h, Pix: pix, Meta: meta}
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
