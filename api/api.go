// Package api 是 gotoolkit 唯一的对外入口。
// 调用方只能看到本包的类型与函数；具体格式实现放在 internal 下，外部不可引用。
package api

import (
	"errors"
	"fmt"

	"gotoolkit/internal/checksum"
	"gotoolkit/internal/codec"
)

// Options 是所有编解码参数的公共入口。
// 新增可配参数必须加在这里并写明默认值，否则调用方无法设置。
type Options struct {
	Format     string // pgm | bmp | rle，默认 pgm
	Gain       int    // 亮度增益百分比，0 表示不调整，默认 0
	Checksum   bool   // 是否在文件头写入并按文件体验证多项式校验，默认 false
	Polynomial string // 校验多项式名字：crc32-ieee（32 位档，默认）| crc16-ibm（16 位档）
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
func DefaultOptions() Options { return Options{Format: "pgm", Gain: 0, Checksum: false, Polynomial: ""} }

func (o Options) validate() error {
	switch o.Format {
	case "pgm", "bmp", "rle":
	default:
		return fmt.Errorf("%w: %q", ErrUnknownFormat, o.Format)
	}
	if o.Gain < -100 || o.Gain > 100 {
		return fmt.Errorf("%w: Gain=%d 超出 [-100,100]", ErrBadOption, o.Gain)
	}
	if o.Polynomial != "" {
		if _, err := checksum.ByName(o.Polynomial); err != nil {
			return fmt.Errorf("%w: %v", ErrBadOption, err)
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
	var body []byte
	var err error
	switch opt.Format {
	case "pgm":
		body, err = codec.EncodePGM(src.W, src.H, src.Pix)
	case "bmp":
		body, err = codec.EncodeBMP(src.W, src.H, src.Pix)
	default:
		body, err = codec.EncodeRLE(src.W, src.H, src.Pix)
	}
	if err != nil {
		return nil, err
	}
	if opt.Checksum {
		c, err := resolveCRC(opt)
		if err != nil {
			return nil, err
		}
		body = seal(body, opt.Format, c)
	}
	return body, nil
}

// Decode 把字节流解码成灰度图。
func Decode(data []byte, opt Options) (*Image, error) {
	if len(data) == 0 {
		return nil, ErrTruncated
	}
	if err := opt.validate(); err != nil {
		return nil, err
	}
	if opt.Checksum {
		c, err := resolveCRC(opt)
		if err != nil {
			return nil, err
		}
		data, err = unseal(data, opt.Format, c)
		if err != nil {
			return nil, err
		}
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
