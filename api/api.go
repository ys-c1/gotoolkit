// Package api 是 gotoolkit 唯一的对外入口。
// 调用方只能看到本包的类型与函数；具体格式实现放在 internal 下，外部不可引用。
package api

import (
	"errors"
	"fmt"

	"gotoolkit/internal/codec"
	"gotoolkit/internal/quant"
)

// Options 是所有编解码参数的公共入口。
// 新增可配参数必须加在这里并写明默认值，否则调用方无法设置。
type Options struct {
	Format string // pgm | bmp | rle，默认 pgm
	Gain   int    // 亮度增益百分比，0 表示不调整，默认 0

	// Levels 是量化调色板的桶数，取值 2..256；0 表示不量化（默认 0）。
	// Levels 不为 0 时，Encode 先按 Gain 调亮度，再量化到 Levels 级调色板。
	Levels int
	// Dither 是抖动强度百分比，取值 0..100；0 表示不抖动（默认 0）。
	// 强度 τ = Dither/100，详见 internal/quant 的包注释。
	Dither int
	// AutoDither 为真时忽略 Dither，改用 OptimalDither(Levels) 给出的
	// 加权误差最小配比。要求 Levels 在 2..256 且 Dither 为 0。
	AutoDither bool
}

// Image 是解码后的灰度图。
type Image struct {
	W, H int
	Pix  []uint8
}

// ErrorParts 是量化误差分解，Banding 为色块、Noise 为噪声。
// 两者量纲相同：灰度级²/像素；二者之和等于逐像素均方误差。
type ErrorParts = quant.Parts

// DitherPlan 是某个桶数下的最优抖动配比。
type DitherPlan = quant.Plan

// 加权误差的默认权重。两部分量纲相同，可直接加权；取值依据见
// OptimalDither 与 docs/dither.md。
const (
	BandingWeight = quant.WeightBanding
	NoiseWeight   = quant.WeightNoise

	MinLevels = quant.MinLevels
	MaxLevels = quant.MaxLevels
)

var (
	ErrUnknownFormat = errors.New("未知格式")
	ErrBadSize       = errors.New("画幅必须为正")
	ErrBadOption     = errors.New("参数越界")
	ErrTruncated     = errors.New("数据被截断")
)

// DefaultOptions 返回默认参数：不量化、不抖动，产物与历史版本一致。
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
	if o.Levels != 0 && (o.Levels < MinLevels || o.Levels > MaxLevels) {
		return fmt.Errorf("%w: Levels=%d 超出 [%d,%d] 或应为 0", ErrBadOption, o.Levels, MinLevels, MaxLevels)
	}
	if o.Dither < 0 || o.Dither > 100 {
		return fmt.Errorf("%w: Dither=%d 超出 [0,100]", ErrBadOption, o.Dither)
	}
	if o.AutoDither {
		if o.Levels < MinLevels {
			return fmt.Errorf("%w: AutoDither 需要 Levels>=%d", ErrBadOption, MinLevels)
		}
		if o.Dither != 0 {
			return fmt.Errorf("%w: AutoDither 与显式 Dither=%d 不能同时设置", ErrBadOption, o.Dither)
		}
	}
	if o.Levels == 0 && (o.Dither != 0 || o.AutoDither) {
		return fmt.Errorf("%w: 未设 Levels 时不能抖动", ErrBadOption)
	}
	return nil
}

// ditherTau 返回实际使用的抖动强度 τ∈[0,1]；调用前必须已通过 validate。
func (o Options) ditherTau() float64 {
	if o.AutoDither {
		return quant.Optimize(o.Levels).Tau
	}
	return float64(o.Dither) / 100
}

// Encode 把灰度图编码成指定格式。
// Levels 不为 0 时先量化（可带抖动），否则产物与不含量化参数时逐字节一致。
func Encode(img *Image, opt Options) ([]byte, error) {
	src, err := prepare(img, opt)
	if err != nil {
		return nil, err
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

// prepare 校验参数、应用增益与量化，返回真正要编码的图。
func prepare(img *Image, opt Options) (*Image, error) {
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
		src = applyGain(src, opt.Gain)
	}
	if opt.Levels != 0 {
		pix, err := quant.Quantize(src.W, src.H, src.Pix, opt.Levels, opt.ditherTau())
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrBadOption, err)
		}
		src = &Image{W: src.W, H: src.H, Pix: pix}
	}
	return src, nil
}

// Decode 把字节流解码成灰度图。
// 量化与抖动是有损的，Decode 不做逆变换；Levels/Dither/AutoDither 只做校验。
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

// OptimalDither 返回 Levels 桶下使加权误差 W = 4·Banding + Noise 最小的
// 抖动配比与模型误差。配比只取决于桶数（以均匀灰度分布为先验），与图像内容
// 无关，因此同一桶数的结果可复用。桶数越界时报错。
//
// 权重 4:1 的依据：色块是低频、空间相干的伪影，缩放和拉远都无法把它平均掉；
// 抖动噪声是高频、逐像素近独立的，2×2 视觉积分能把噪声功率摊薄约 4 倍。
// 同能量下色块的可见度约为噪声的 4 倍。在连续近似下最优强度有闭式
// τ* = 1 - NoiseWeight/BandingWeight，权重比小于 1 时最优解退化为 τ=0。
func OptimalDither(levels int) (DitherPlan, error) {
	if levels < MinLevels || levels > MaxLevels {
		return DitherPlan{}, fmt.Errorf("%w: Levels=%d 超出 [%d,%d]", ErrBadOption, levels, MinLevels, MaxLevels)
	}
	return quant.Optimize(levels), nil
}

// OptimalDitherFor 在给定输入直方图上搜索最优抖动配比，用于内容已知时
// 比 OptimalDither 的均匀先验更贴合实际。hist[v] 是灰度 v 的权重。
func OptimalDitherFor(hist [256]float64, levels int) (DitherPlan, error) {
	if levels < MinLevels || levels > MaxLevels {
		return DitherPlan{}, fmt.Errorf("%w: Levels=%d 超出 [%d,%d]", ErrBadOption, levels, MinLevels, MaxLevels)
	}
	return quant.OptimizeHist(hist, levels), nil
}

// MeasureQuantization 把 src 到 out 的逐像素误差拆成色块与噪声。
// 两图尺寸必须一致，levels 必须在 2..256。
func MeasureQuantization(src, out *Image, levels int) (ErrorParts, bool) {
	if src == nil || out == nil || src.W != out.W || src.H != out.H {
		return ErrorParts{}, false
	}
	return quant.Measure(src.Pix, out.Pix, levels)
}

// Histogram 返回灰度直方图，计数作为权重，供 OptimalDitherFor 使用。
func Histogram(img *Image) [256]float64 {
	var h [256]float64
	if img == nil {
		return h
	}
	for _, v := range img.Pix {
		h[v]++
	}
	return h
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
