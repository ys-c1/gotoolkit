// Package api 是 gotoolkit 唯一的对外入口。
// 调用方只能看到本包的类型与函数；具体格式实现放在 internal 下，外部不可引用。
package api

import (
	"errors"
	"fmt"

	"gotoolkit/internal/codec"
	"gotoolkit/internal/palette"
)

// MaxPaletteBuckets 是 8 位灰度调色板的最大桶数。
const MaxPaletteBuckets = palette.MaxBuckets

// Options 是所有编解码参数的公共入口。
// 新增可配参数必须加在这里并写明默认值，否则调用方无法设置。
type Options struct {
	Format string // pgm | bmp | rle，默认 pgm
	Gain   int    // 亮度增益百分比，0 表示不调整，默认 0

	// Palette 启用中位切分调色板量化。默认 false：像素直通，
	// 三种格式与 testdata/golden 逐字节一致。
	Palette bool
	// Buckets 是调色板桶数上限 K，Palette=true 时必须在 [1,256]。
	// Palette=false 时该字段被忽略，零值仍是默认配置。
	Buckets int
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
	ErrBadBuckets    = palette.ErrBuckets
	// ErrBadPaletteBuckets 是同一哨兵错误的描述性别名。
	ErrBadPaletteBuckets = palette.ErrBuckets
)

// DefaultOptions 返回默认参数。
func DefaultOptions() Options {
	return Options{Format: "pgm", Gain: 0, Palette: false, Buckets: 0}
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
	return o.validatePalette()
}

func (o Options) validatePalette() error {
	if o.Palette && (o.Buckets < 1 || o.Buckets > MaxPaletteBuckets) {
		return fmt.Errorf("%w: Buckets=%d 必须在 [1,%d]",
			ErrBadBuckets, o.Buckets, MaxPaletteBuckets)
	}
	return nil
}

// PaletteSplit 是一次中位切分的可复算记录。
type PaletteSplit struct {
	Step     int
	Axis     int
	Variance float64
	Pivot    uint8
	LeftN    int
	RightN   int
}

// PaletteStats 是调色板量化结果与误差。
type PaletteStats struct {
	Requested int
	Actual    int
	Palette   []uint8
	Splits    []PaletteSplit
	MSE       float64
	MAE       float64
	RMSE      float64
	MaxAbs    int
}

// QuantizePalette 用与 Encode 相同的中位切分实现量化图像，不修改入参。
func QuantizePalette(img *Image, opt Options) (*Image, *PaletteStats, error) {
	if img == nil {
		return nil, nil, ErrBadSize
	}
	if img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return nil, nil, ErrBadSize
	}
	if err := opt.validatePalette(); err != nil {
		return nil, nil, err
	}
	if !opt.Palette {
		return nil, nil, fmt.Errorf("%w: 未启用 Palette", ErrBadOption)
	}
	res, err := palette.Quantize(img.Pix, opt.Buckets)
	if err != nil {
		return nil, nil, err
	}
	splits := make([]PaletteSplit, 0, len(res.Splits))
	for _, s := range res.Splits {
		splits = append(splits, PaletteSplit{
			Step: s.Step, Axis: s.Axis, Variance: s.Variance,
			Pivot: s.Pivot, LeftN: s.LeftN, RightN: s.RightN,
		})
	}
	return &Image{W: img.W, H: img.H, Pix: res.Pix}, &PaletteStats{
		Requested: opt.Buckets,
		Actual:    res.Actual,
		Palette:   res.Palette,
		Splits:    splits,
		MSE:       res.MSE,
		MAE:       res.MAE,
		RMSE:      res.RMSE,
		MaxAbs:    res.MaxAbs,
	}, nil
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
	if opt.Palette {
		quantized, _, err := QuantizePalette(src, opt)
		if err != nil {
			return nil, err
		}
		src = quantized
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
