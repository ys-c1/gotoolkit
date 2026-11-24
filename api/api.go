// Package api 是 gotoolkit 唯一的对外入口。
// 调用方只能看到本包的类型与函数；具体格式实现放在 internal 下，外部不可引用。
package api

import (
	"errors"
	"fmt"
	"math"

	"gotoolkit/internal/codec"
)

// Options 是所有编解码参数的公共入口。
// 新增可配参数必须加在这里并写明默认值，否则调用方无法设置。
type Options struct {
	Format string // pgm | bmp | rle | dict，默认 pgm
	Gain   int    // 亮度增益百分比，0 表示不调整，默认 0
}

// Image 是解码后的灰度图。
type Image struct {
	W, H int
	Pix  []uint8
}

// DictOverhead 是 GTD1 编码的开销拆分，单位字节；bit 级字段保留原始位数。
type DictOverhead struct {
	HeaderBytes      int
	DictBytes        int
	BitstreamBytes   int
	BitstreamPadBits int
	PayloadBits      int
	DictCodeBits     int
	EscapeBits       int
	EscapeEvents     int
	TotalBytes       int
	DictEntries      int
	EscapedSymbols   int
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
	case "pgm", "bmp", "rle", "dict":
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
	case "rle":
		return codec.EncodeRLE(src.W, src.H, src.Pix)
	default:
		body, _, err := codec.EncodeDict(src.W, src.H, src.Pix)
		return body, err
	}
}

// EncodeDictWithStats 用 GTD1 字典格式编码，并返回可对账的开销拆分。
func EncodeDictWithStats(img *Image) ([]byte, DictOverhead, error) {
	if img == nil || img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return nil, DictOverhead{}, ErrBadSize
	}
	body, st, err := codec.EncodeDict(img.W, img.H, img.Pix)
	if err != nil {
		return nil, DictOverhead{}, err
	}
	return body, DictOverhead{
		HeaderBytes:      st.HeaderBytes,
		DictBytes:        st.DictBytes,
		BitstreamBytes:   st.BitstreamBytes,
		BitstreamPadBits: st.BitstreamPadBits,
		PayloadBits:      st.PayloadBits,
		DictCodeBits:     st.DictCodeBits,
		EscapeBits:       st.EscapeBits,
		EscapeEvents:     st.EscapeEvents,
		TotalBytes:       st.TotalBytes,
		DictEntries:      st.DictEntries,
		EscapedSymbols:   st.EscapedSymbols,
	}, nil
}

// Entropy 按符号出现次数计算一阶信息熵，单位比特/像素；
// 同时返回实际出现的符号种类数。空图返回错误。
func Entropy(pix []uint8) (bitsPerPixel float64, distinct int, err error) {
	if len(pix) == 0 {
		return 0, 0, ErrBadSize
	}
	var count [256]int
	for _, v := range pix {
		count[v]++
	}
	n := float64(len(pix))
	var h float64
	for _, c := range count {
		if c > 0 {
			distinct++
			p := float64(c) / n
			h -= p * math.Log2(p)
		}
	}
	return h, distinct, nil
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
		w, h, pix, err = codec.DecodeDict(data)
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
