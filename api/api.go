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
	// Mask 是透明掩码的灰度值：编码时所有等于 Mask 的像素被标为透明，
	// 解码后这些像素的原始灰度零误差还原，透明标记放在 Image.Alpha 里。
	// 取值范围 [0,255]；-1（默认）表示不启用掩码。
	// 掩码与增益同时开启时以掩码为准：先按【原始像素】计算掩码，再施加增益，
	// 即“先掩码后增益”。这样透明判定不会被增益改变。
	Mask int
}

// Image 是解码后的灰度图。
type Image struct {
	W, H int
	Pix  []uint8
	// Alpha 是每像素透明度，长度与 Pix 相同：0 透明（该像素命中掩码值），255 不透明。
	// 未启用掩码（或流中没有掩码尾段）时为 nil，即缺省不带透明通道。
	Alpha []uint8
}

var (
	ErrUnknownFormat = errors.New("未知格式")
	ErrBadSize       = errors.New("画幅必须为正")
	ErrBadOption     = errors.New("参数越界")
	ErrTruncated     = errors.New("数据被截断")
	ErrMaskMismatch  = errors.New("掩码值与流中记录的不一致")
)

// DefaultOptions 返回默认参数。
// Mask 缺省为 -1：不启用掩码，此时编码出的字节流与未启用掩码的版本完全一致。
func DefaultOptions() Options { return Options{Format: "pgm", Gain: 0, Mask: -1} }

func (o Options) validate() error {
	switch o.Format {
	case "pgm", "bmp", "rle":
	default:
		return fmt.Errorf("%w: %q", ErrUnknownFormat, o.Format)
	}
	if o.Gain < -100 || o.Gain > 100 {
		return fmt.Errorf("%w: Gain=%d 超出 [-100,100]", ErrBadOption, o.Gain)
	}
	// 掩码只接受 -1（关闭）或 [0,255] 内的灰度值，其余一律拦下。
	if o.Mask < -1 || o.Mask > 255 {
		return fmt.Errorf("%w: Mask=%d 超出 {-1}∪[0,255]，合法取值为 -1（关闭）或 0..255", ErrBadOption, o.Mask)
	}
	return nil
}

// maskEnabled 报告掩码是否启用。
func (o Options) maskEnabled() bool { return o.Mask >= 0 }

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
	// 先掩码后增益（掩码与增益同时开启时以掩码为准）：
	// 第一步只依据原始像素生成透明位图，第二步再调整亮度，
	// 保证增益不会改变哪些像素属于透明掩码。
	var alpha []uint8
	if opt.maskEnabled() {
		alpha = buildAlpha(img.Pix, uint8(opt.Mask))
	}
	src := img
	if opt.Gain != 0 {
		src = applyGain(img, opt.Gain)
	}
	if !opt.maskEnabled() {
		// 掩码关闭：走与旧版本完全相同的编码路径，逐字节一致。
		switch opt.Format {
		case "pgm":
			return codec.EncodePGM(src.W, src.H, src.Pix)
		case "bmp":
			return codec.EncodeBMP(src.W, src.H, src.Pix)
		default:
			return codec.EncodeRLE(src.W, src.H, src.Pix)
		}
	}
	maskValue := uint8(opt.Mask)
	switch opt.Format {
	case "pgm":
		return codec.EncodePGMMasked(src.W, src.H, src.Pix, alpha, maskValue)
	case "bmp":
		return codec.EncodeBMPMasked(src.W, src.H, src.Pix, alpha, maskValue)
	default:
		return codec.EncodeRLEMasked(src.W, src.H, src.Pix, alpha, maskValue)
	}
}

// Decode 把字节流解码成灰度图。
// 启用掩码（Mask∈[0,255]）时要求流中带掩码尾段，且记录的掩码值必须与 Mask 一致；
// 掩码关闭（Mask=-1）时，无论流中是否带尾段都只返回灰度像素（Alpha 为 nil）。
func Decode(data []byte, opt Options) (*Image, error) {
	if len(data) == 0 {
		return nil, ErrTruncated
	}
	if err := opt.validate(); err != nil {
		return nil, err
	}
	var (
		w, h       int
		pix, alpha []uint8
		stored     uint8
		err        error
	)
	if !opt.maskEnabled() {
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
	} else {
		// 掩码开启：必须存在尾段，且尾段记录的掩码值与请求值一致。
		switch opt.Format {
		case "pgm":
			w, h, stored, pix, alpha, err = codec.DecodePGMMasked(data)
		case "bmp":
			w, h, stored, pix, alpha, err = codec.DecodeBMPMasked(data)
		default:
			w, h, stored, pix, alpha, err = codec.DecodeRLEMasked(data)
		}
		if err != nil {
			return nil, err
		}
		if int(stored) != opt.Mask {
			return nil, fmt.Errorf("%w: 请求 Mask=%d，流中记录 Mask=%d", ErrMaskMismatch, opt.Mask, stored)
		}
	}
	img := &Image{W: w, H: h, Pix: pix, Alpha: alpha}
	// 增益是编码时的逆操作；透明位图在增益前已确定，这里不随增益改变。
	if opt.Gain != 0 {
		img = applyGain(img, -opt.Gain)
	}
	return img, nil
}

// buildAlpha 依据掩码值生成每像素 alpha：等于掩码值的像素透明（0），其余不透明（255）。
func buildAlpha(pix []uint8, maskValue uint8) []uint8 {
	alpha := make([]uint8, len(pix))
	for i, v := range pix {
		if v == maskValue {
			alpha[i] = 0
		} else {
			alpha[i] = 255
		}
	}
	return alpha
}

func applyGain(img *Image, gain int) *Image {
	out := &Image{W: img.W, H: img.H, Pix: make([]uint8, len(img.Pix)), Alpha: img.Alpha}
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
