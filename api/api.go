// Package api 是 gotoolkit 唯一的对外入口。
// 调用方只能看到本包的类型与函数；具体格式实现放在 internal 下，外部不可引用。
package api

import (
	"errors"
	"fmt"

	"gotoolkit/internal/codec"
	"gotoolkit/internal/target"
)

// Options 是所有编解码参数的公共入口。
// 新增可配参数必须加在这里并写明默认值，否则调用方无法设置。
type Options struct {
	Format string // pgm | bmp | rle | blk，默认 pgm
	Gain   int    // 亮度增益百分比，0 表示不调整，默认 0

	// 按目标体积反解质量参数（仅 blk 分块位深格式 / SolveQuality 使用）。
	// 不指定任何目标字段时，编码路径与现在完全一致，既有三格式字节不变。
	Quality        int // 质量参数 1..100，越大越清晰、体积单调不减；0 按 100（最高质量，位深 8）处理
	BlockSize      int // 分块边长 B；0 用 codec.DefaultBlock(=8)，合法区间 [1,codec.MaxBlock]
	TargetBytes    int // 点目标体积（字节）；未给 Min/Max 时按闭区间 [T,T] 处理，通常走最接近兜底
	TargetMinBytes int // 目标体积区间下界（含，字节）
	TargetMaxBytes int // 目标体积区间上界（含，字节）
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
	ErrBadTarget     = errors.New("目标体积区间非法")
)

// DefaultOptions 返回默认参数。
func DefaultOptions() Options { return Options{Format: "pgm", Gain: 0} }

// effectiveQuality 把零值质量归一成最高质量，保证零值 Options 编码 blk 时最清晰。
func (o Options) effectiveQuality() int {
	if o.Quality <= 0 {
		return target.MaxQuality
	}
	return o.Quality
}

// effectiveDepth 是质量参数对应的 BLK 位深。
func (o Options) effectiveDepth() int {
	return target.DepthForQuality(o.effectiveQuality())
}

// effectiveBlockSize 返回归一化后的分块边长。
func (o Options) effectiveBlockSize() int {
	return codec.NormalizeBlockSize(o.BlockSize)
}

func (o Options) validate() error {
	switch o.Format {
	case "pgm", "bmp", "rle", "blk":
	default:
		return fmt.Errorf("%w: %q", ErrUnknownFormat, o.Format)
	}
	if o.Gain < -100 || o.Gain > 100 {
		return fmt.Errorf("%w: Gain=%d 超出 [-100,100]", ErrBadOption, o.Gain)
	}
	if o.Quality < 0 || o.Quality > 100 {
		return fmt.Errorf("%w: Quality=%d 超出 [1,100]（0=默认最高）", ErrBadOption, o.Quality)
	}
	if o.BlockSize < 0 || o.BlockSize > codec.MaxBlock {
		return fmt.Errorf("%w: BlockSize=%d 超出 [1,%d]（0=默认 %d）",
			ErrBadOption, o.BlockSize, codec.MaxBlock, codec.DefaultBlock)
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
	case "blk":
		return codec.EncodeBLK(src.W, src.H, src.Pix, opt.effectiveBlockSize(), opt.effectiveDepth())
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
	case "blk":
		w, h, pix, err = codec.DecodeBLK(data)
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
