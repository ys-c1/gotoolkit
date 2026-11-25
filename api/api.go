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
		if opt.Gain == -100 {
			// 正变换斜率 a=(100+g)/100=0，全域被压成 0，代数上不可逆。
			return nil, fmt.Errorf("%w: Gain=-100 正变换不可逆，无法还原", ErrBadOption)
		}
		img = applyGainInverse(img, opt.Gain)
	}
	return img, nil
}

// applyGain 是编码侧的正变换：y = round(x·(100+g)/100)，钳到 [0,255]。
func applyGain(img *Image, gain int) *Image {
	out := &Image{W: img.W, H: img.H, Pix: make([]uint8, len(img.Pix))}
	for i, v := range img.Pix {
		out.Pix[i] = uint8(gainFwd(int(v), gain, 255))
	}
	return out
}

// applyGainInverse 是解码侧的逆变换：x̂ = round(y·100/(100+g))。
// 与 applyGain 互为代数逆：无量化且不饱和时严格恒等；
// 量化后误差 |Δ| ≤ ⌊(b+1)/2⌋ LSB，b = 100/(100+g)。
// 证明与位深关系见 docs/gain-roundtrip.md。
func applyGainInverse(img *Image, gain int) *Image {
	out := &Image{W: img.W, H: img.H, Pix: make([]uint8, len(img.Pix))}
	for i, v := range img.Pix {
		out.Pix[i] = uint8(gainInv(int(v), gain, 255))
	}
	return out
}

// gainFwd 正变换标量形式。max 为满量程：8 位取 255，n 位取 2^n-1。
func gainFwd(x, g, max int) int {
	y := (x*(100+g) + 50) / 100 // 四舍五入，不用截断
	return clampInt(y, max)
}

// gainInv 逆变换标量形式。要求 g > -100（调用方保证）。
func gainInv(y, g, max int) int {
	den := 100 + g
	x := (y*100 + den/2) / den // 四舍五入
	return clampInt(x, max)
}

func clampInt(v, max int) int {
	if v < 0 {
		return 0
	}
	if v > max {
		return max
	}
	return v
}
