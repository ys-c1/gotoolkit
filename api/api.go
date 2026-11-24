// Package api 是 gotoolkit 唯一的对外入口。
// 调用方只能看到本包的类型与函数；具体格式实现放在 internal 下，外部不可引用。
package api

import (
	"errors"
	"fmt"
	"math"

	"gotoolkit/internal/codec"
	"gotoolkit/internal/quant"
)

// 重建电平的选择值，供 Options.Recon 使用。
const (
	// ReconMidpoint 用等分区间中点重建，不依赖样本分布。
	ReconMidpoint = "midpoint"
	// ReconExpectation 用条件期望（Lloyd 式不动点迭代）重建。
	ReconExpectation = "expectation"
)

// Options 是所有编解码参数的公共入口。
// 新增可配参数必须加在这里并写明默认值，否则调用方无法设置。
type Options struct {
	Format string // pgm | bmp | rle，默认 pgm
	Gain   int    // 亮度增益百分比，0 表示不调整，默认 0

	// 以下参数只在 Decode 时生效：解码后先按 Bits 位深重建电平，再加 Gain。
	Bits         int     // 重建位深，1..8；0 表示不重建（默认）
	Recon        string  // midpoint | expectation，默认 midpoint
	ReconTol     float64 // 不动点迭代收敛阈值，0 表示用 quant.DefaultTol
	ReconMaxIter int     // 不动点迭代上限，0 表示用 quant.DefaultMaxIter
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
func DefaultOptions() Options {
	return Options{
		Format:       "pgm",
		Gain:         0,
		Bits:         0,
		Recon:        ReconMidpoint,
		ReconTol:     quant.DefaultTol,
		ReconMaxIter: quant.DefaultMaxIter,
	}
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
	return o.validateRecon()
}

// validateRecon 只检查重建相关参数，供不经过格式编解码的 Reconstruct 使用。
func (o Options) validateRecon() error {
	if o.Bits < 0 || o.Bits > quant.MaxBits {
		return fmt.Errorf("%w: Bits=%d 超出 [0,%d]", ErrBadOption, o.Bits, quant.MaxBits)
	}
	if o.Bits == 0 {
		return nil
	}
	switch o.Recon {
	case "", ReconMidpoint, ReconExpectation:
	default:
		return fmt.Errorf("%w: Recon=%q", ErrBadOption, o.Recon)
	}
	if math.IsNaN(o.ReconTol) || math.IsInf(o.ReconTol, 0) || o.ReconTol < 0 {
		return fmt.Errorf("%w: ReconTol=%g", ErrBadOption, o.ReconTol)
	}
	if o.ReconMaxIter < 0 {
		return fmt.Errorf("%w: ReconMaxIter=%d", ErrBadOption, o.ReconMaxIter)
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
// 若 Options.Bits 非零，先按所选方式重建到该位深，再应用 Gain。
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
	if opt.Bits != 0 {
		if img, err = reconstruct(img, opt); err != nil {
			return nil, err
		}
	}
	if opt.Gain != 0 {
		img = applyGain(img, -opt.Gain)
	}
	return img, nil
}

// Reconstruct 返回按 opt 重建后的图像；不是从字节流解码时也可直接调用。
func Reconstruct(img *Image, opt Options) (*Image, error) {
	if img == nil {
		return nil, ErrBadSize
	}
	if img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return nil, ErrBadSize
	}
	if opt.Bits == 0 {
		out := &Image{W: img.W, H: img.H, Pix: make([]uint8, len(img.Pix))}
		copy(out.Pix, img.Pix)
		return out, nil
	}
	if err := opt.validateRecon(); err != nil {
		return nil, err
	}
	return reconstruct(img, opt)
}

// reconstruct 按 opt 生成重建电平并量化到 8 位输出。
func reconstruct(img *Image, opt Options) (*Image, error) {
	res, err := reconstruction(quant.FromPixels(img.Pix), opt)
	if err != nil {
		return nil, err
	}
	return &Image{W: img.W, H: img.H, Pix: quant.QuantizePixels(img.Pix, res.Levels)}, nil
}

// reconstruction 是电平构造的唯一入口，测试也经它取迭代次数与误差。
func reconstruction(h quant.Histogram, opt Options) (quant.Result, error) {
	tol := opt.ReconTol
	if tol == 0 {
		tol = quant.DefaultTol
	}
	maxIter := opt.ReconMaxIter
	if maxIter == 0 {
		maxIter = quant.DefaultMaxIter
	}
	if opt.Recon == ReconExpectation {
		return quant.Optimal(h, opt.Bits, tol, maxIter)
	}
	return quant.Midpoint(opt.Bits)
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
