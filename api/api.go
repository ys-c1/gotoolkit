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

	// 以下三个排布参数互相正交，取显式值时组成 2×2×2 = 8 种组合。
	// 空串 / 0 表示“沿用该格式旧文件的行为”，因此零值 Options 与旧版本逐字节一致：
	//   pgm、rle 旧行为 = down / le / 1
	//   bmp      旧行为 = up   / le / 4
	RowOrder  string // "down" | "up"；up = 先存逻辑上的最后一行
	ByteOrder string // "le" | "be"；只作用于格式头里的多字节整数
	RowAlign  int    // 1 = 紧凑 | 4 = 每行补到 4 字节
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

// DefaultOptions 返回默认参数。排布字段留空，等价于各格式的旧行为。
func DefaultOptions() Options {
	return Options{Format: "pgm", Gain: 0}
}

// legacyLayout 返回某格式不带排布标志时的旧行为。
func legacyLayout(format string) codec.Layout {
	if format == "bmp" {
		return codec.Layout{BottomUp: true, BigEndian: false, Align: 4}
	}
	return codec.Layout{BottomUp: false, BigEndian: false, Align: 1}
}

// Layout 把排布参数归一化成 codec.Layout；空值沿用 legacyLayout。
func (o Options) Layout() (codec.Layout, error) {
	base := legacyLayout(o.Format)
	l := base
	switch o.RowOrder {
	case "":
		// 沿用旧行为
	case "down":
		l.BottomUp = false
	case "up":
		l.BottomUp = true
	default:
		return l, fmt.Errorf("%w: RowOrder=%q（取 down|up）", ErrBadOption, o.RowOrder)
	}
	switch o.ByteOrder {
	case "":
	case "le":
		l.BigEndian = false
	case "be":
		l.BigEndian = true
	default:
		return l, fmt.Errorf("%w: ByteOrder=%q（取 le|be）", ErrBadOption, o.ByteOrder)
	}
	switch o.RowAlign {
	case 0:
	case 1:
		l.Align = 1
	case 4:
		l.Align = 4
	default:
		return l, fmt.Errorf("%w: RowAlign=%d（取 1|4）", ErrBadOption, o.RowAlign)
	}
	return l, nil
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
	_, err := o.Layout()
	return err
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
	l, _ := opt.Layout()
	src := img
	if opt.Gain != 0 {
		src = applyGain(img, opt.Gain)
	}
	switch opt.Format {
	case "pgm":
		return codec.EncodePGMLayout(src.W, src.H, src.Pix, l)
	case "bmp":
		return codec.EncodeBMPLayout(src.W, src.H, src.Pix, l)
	default:
		return codec.EncodeRLELayout(src.W, src.H, src.Pix, l)
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
	l, _ := opt.Layout()
	var w, h int
	var pix []uint8
	var err error
	switch opt.Format {
	case "pgm":
		w, h, pix, err = codec.DecodePGMLayout(data, l)
	case "bmp":
		w, h, pix, err = codec.DecodeBMPLayout(data, l)
	default:
		w, h, pix, err = codec.DecodeRLELayout(data, l)
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
