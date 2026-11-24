// Package api 是 gotoolkit 唯一的对外入口。
// 调用方只能看到本包的类型与函数；具体格式实现放在 internal 下，外部不可引用。
package api

import (
	"errors"
	"fmt"
	"math"

	"gotoolkit/internal/codec"
	"gotoolkit/internal/palette"
)

// MaxPaletteBuckets 是 8 位灰度调色板的桶数上限（=256）。
const MaxPaletteBuckets = palette.MaxBuckets

// Options 是所有编解码参数的公共入口。
// 新增可配参数必须加在这里并写明默认值，否则调用方无法设置。
type Options struct {
	Format string // pgm | bmp | rle，默认 pgm
	Gain   int    // 亮度增益百分比，0 表示不调整，默认 0

	// Palette 是调色板量化开关。默认 false：像素原样直通，三种格式的输出
	// 与 testdata/golden 逐字节一致，且文件头的长度与内容都不变。
	Palette bool
	// Buckets 是调色板桶数上限 K。仅在 Palette=true 时生效，必须落在
	// [1, MaxPaletteBuckets]；K=0 或越界报错（不会静默改成默认值）。
	Buckets int
	// VarianceThreshold 是桶内方差停止阈值 τ（灰度阶²）的覆盖值。
	// 默认 0 表示由 K 自动推导：τ(K)=(256/K)²/12（见 PaletteThreshold）。
	// 取正值时手动指定，必须落在 (0, 16256.25]；负值、NaN、Inf 或超过上界
	// 视为“倒置”并报错。
	VarianceThreshold float64
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
	// ErrBadPaletteBuckets 表示调色板桶数非法（含桶数为 0）。
	ErrBadPaletteBuckets = palette.ErrBuckets
	// ErrBadPaletteThreshold 表示桶内方差阈值倒置（负值、非有限或超过上界）。
	ErrBadPaletteThreshold = palette.ErrThreshold
)

// DefaultOptions 返回默认参数。
// 调色板默认关闭：Palette=false、Buckets=0、VarianceThreshold=0，
// 此时三种格式的输出与既有基准逐字节一致。
func DefaultOptions() Options {
	return Options{Format: "pgm", Gain: 0, Palette: false, Buckets: 0, VarianceThreshold: 0}
}

// PaletteThreshold 由桶数 K 推出桶内方差停止阈值 τ(K)=(256/K)²/12。
// 这是 Options.VarianceThreshold 取默认 0 时实际使用的值，公开给调用方复算。
func PaletteThreshold(k int) (float64, error) {
	return palette.DeriveThreshold(k)
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
	// 阈值为非法值（负、非有限、超过理论最大）时，无论开关开合都拒绝，
	// 避免调用方误以为它生效。0 始终合法，表示“按 K 自动推导”。
	if o.VarianceThreshold != 0 {
		if math.IsNaN(o.VarianceThreshold) || math.IsInf(o.VarianceThreshold, 0) ||
			o.VarianceThreshold < 0 || o.VarianceThreshold > palette.VarianceCeiling {
			return fmt.Errorf("%w: VarianceThreshold=%v 必须为 0（自动）或落在 (0,%v]",
				ErrBadPaletteThreshold, o.VarianceThreshold, palette.VarianceCeiling)
		}
	}
	// 桶数只在开关打开时才有意义：关闭时零值合法（表示未启用），
	// 打开时必须是 [1,256]，0 不能静默当作默认值。
	if o.Palette && (o.Buckets < 1 || o.Buckets > MaxPaletteBuckets) {
		return fmt.Errorf("%w: Buckets=%d 必须在 [1,%d]",
			ErrBadPaletteBuckets, o.Buckets, MaxPaletteBuckets)
	}
	return nil
}

// PaletteStats 是一次调色板量化的可核对数据。
// 切分维与阈值的判定都可据此复算：Buckets[i].Variance 的 argmax 即
// Buckets[i].Axis（见 AxisOf），Splits 记录每次切分的轴、方差与左右像素数。
type PaletteStats struct {
	Requested      int     // 请求的桶数上限 K
	Actual         int     // 实际桶数
	Delta          float64 // Δ = 256/K
	Threshold      float64 // 实际使用的 τ
	Stop           string  // threshold | bucket-cap | no-splittable
	MaxVariance    float64 // 终止时最大桶内方差
	WithinVariance float64 // W = Σ n_b σ_b² / N
	MSE            float64 // 量化后与输入的均方误差
	MSEBound       float64 // W + 1/4，见 internal/palette 的证明
	ParamBound     float64 // 仅由参数与停止原因推出的上界
	Palette        []uint8 // 去重升序调色板
	Buckets        []palette.Bucket
	Splits         []palette.Split
}

// QuantizePalette 对灰度图做中位切分量化，返回新图与可核对统计，不修改入参。
// 与 Encode 使用同一实现；公开它是为了让调用方能直接复算切分维与阈值。
func QuantizePalette(img *Image, opt Options) (*Image, *PaletteStats, error) {
	if img == nil {
		return nil, nil, ErrBadSize
	}
	if img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return nil, nil, ErrBadSize
	}
	if err := opt.validate(); err != nil {
		return nil, nil, err
	}
	if !opt.Palette {
		return nil, nil, fmt.Errorf("%w: 未启用调色板", ErrBadOption)
	}
	res, err := palette.Quantize(img.Pix, palette.Options{
		Buckets:           opt.Buckets,
		VarianceThreshold: opt.VarianceThreshold,
	})
	if err != nil {
		return nil, nil, err
	}
	out := &Image{W: img.W, H: img.H, Pix: res.Pix}
	return out, &PaletteStats{
		Requested:      res.Requested,
		Actual:         res.Actual,
		Delta:          res.Delta,
		Threshold:      res.Threshold,
		Stop:           res.Stop.String(),
		MaxVariance:    res.MaxVariance,
		WithinVariance: res.WithinVariance,
		MSE:            res.MSE,
		MSEBound:       res.MSEBound,
		ParamBound:     res.ParamBound,
		Palette:        res.Palette,
		Buckets:        res.Buckets,
		Splits:         res.Splits,
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
	// 调色板量化只改像素值，交给编码器后三种格式的头字段与长度都不变。
	if opt.Palette {
		q, _, err := QuantizePalette(src, opt)
		if err != nil {
			return nil, err
		}
		src = q
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
// 调色板量化不可逆，Palette 字段只影响 Encode；Decode 只校验参数、不做反量化，
// 因此“调色板编码后解码”的结果就是量化后的像素，可直接与灰度直通结果比 MSE。
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
