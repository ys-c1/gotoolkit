// Package api 是 gotoolkit 唯一的对外入口。
// 调用方只能看到本包的类型与函数；具体格式实现放在 internal 下，外部不可引用。
package api

import (
	"errors"
	"fmt"

	"gotoolkit/internal/codec"
	"gotoolkit/internal/sharpen"
)

// Options 是所有编解码参数的公共入口。
// 新增可配参数必须加在这里并写明默认值，否则调用方无法设置。
type Options struct {
	Format string // pgm | bmp | rle，默认 pgm
	Gain   int    // 亮度增益百分比，0 表示不调整，默认 0

	// Sharpen 开启锐化，默认 false（字节不变）。
	Sharpen bool
	// SharpenK 指定核宽；0 表示由 api.SolveSharpen 联合反解。
	SharpenK int
	// SharpenGain 为 0 表示采用反解出的「过冲为零」最大增益；
	// 取 >0 时按该倍数覆盖（可用来超限观察白边）。
	SharpenGain float64
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
	if o.SharpenGain < 0 {
		return fmt.Errorf("%w: SharpenGain 必须非负", ErrBadOption)
	}
	if o.SharpenK != 0 && (o.SharpenK < 1 || o.SharpenK%2 == 0) {
		return fmt.Errorf("%w: SharpenK=%d 必须是正奇数", ErrBadOption, o.SharpenK)
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
	if opt.Sharpen {
		sharpened, err := applySharpen(img, opt)
		if err != nil {
			return nil, err
		}
		src = sharpened
	}
	if opt.Gain != 0 {
		src = applyGain(src, opt.Gain)
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

// SharpenResult 是 api 层反解与实测的汇总，供命令行与报告使用。
type SharpenResult struct {
	K       int // 解出的核宽
	Kernel  []float64
	Gain    float64 // 实际使用的增益
	D       float64 // D_k
	Boost   float64 // 边缘增幅 g*max|H|
	Over    float64 // 实测过冲（灰阶）
	OverPix int     // 实测过冲像素数
	Bind    int     // 起约束的像素下标
	Pix     []uint8 // 锐化后的图像
}

// SolveSharpen 在给定灰度图上反解「过冲为零」下的最大增益与核宽。
// k 为 0 时在 3/5/7/9 中联解；非 0 时只在该核宽上求解。
func SolveSharpen(img *Image, k int) (SharpenResult, error) {
	if img == nil || img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return SharpenResult{}, ErrBadSize
	}
	var candidates []int
	if k != 0 {
		if k < 1 || k%2 == 0 {
			return SharpenResult{}, fmt.Errorf("%w: k=%d", ErrBadOption, k)
		}
		candidates = []int{k}
	}
	sol, err := sharpen.Solve(img.W, img.H, img.Pix, candidates)
	if err != nil {
		return SharpenResult{}, err
	}
	return SharpenResult{
		K: sol.K, Kernel: sol.W, Gain: sol.Gain, D: sol.D,
		Boost: sol.Boost, Over: sol.PeakG, OverPix: sol.Clipped, Bind: sol.Bind,
		Pix: sharpen.Apply(img.W, img.H, img.Pix, sol.W, sol.Gain),
	}, nil
}

// MeasureSharpen 在给定增益下实测过冲（灰阶之和、像素数）与边缘增幅。
// gain<=0 时采用解出的增益。用于覆盖增益时核对是否超限。
func MeasureSharpen(img *Image, res SharpenResult, gain float64) (over float64, overPix int, boost float64) {
	if gain <= 0 {
		gain = res.Gain
	}
	vals := sharpen.Unclamped(img.W, img.H, img.Pix, res.Kernel, gain)
	over, overPix = sharpen.Clipping(vals)
	boost = gain * sharpen.Peak(sharpen.HighPass(img.W, img.H, img.Pix, res.Kernel))
	return over, overPix, boost
}

func applySharpen(img *Image, opt Options) (*Image, error) {
	sol, err := SolveSharpen(img, opt.SharpenK)
	if err != nil {
		return nil, err
	}
	gain := sol.Gain
	if opt.SharpenGain > 0 {
		gain = opt.SharpenGain
	}
	return &Image{W: img.W, H: img.H, Pix: sharpen.Apply(img.W, img.H, img.Pix, sol.Kernel, gain)}, nil
}
