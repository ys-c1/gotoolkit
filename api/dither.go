package api

import (
	"fmt"

	"gotoolkit/internal/dither"
)

// DitherOptions 是误差反馈量化的对外参数声明。
// 新增可配参数必须加在这里，调用方无法直接引用 internal/dither。
type DitherOptions struct {
	Levels      int     // 量化级数 2..256，默认 16
	Beta        float64 // 误差反馈系数 [-1,1]；0 为原路径（无反馈）
	Seed        uint64  // RPDF 抖动种子，固定后结果可复现
	DitherScale float64 // 抖动幅度比例 (0,1]，1 为满幅；不足幅会留下红色噪声
}

// DefaultDitherOptions 返回默认量化参数。
func DefaultDitherOptions() DitherOptions {
	d := dither.DefaultOptions()
	return DitherOptions{
		Levels: d.Levels, Beta: d.Beta, Seed: d.Seed, DitherScale: d.DitherScale,
	}
}

// Quantize 对灰度图做带 RPDF 抖动与误差反馈的标量量化，返回新图。
// Beta=0 即未加反馈的原始抖动量化路径，原像素不被改写。
func Quantize(img *Image, opt DitherOptions) (*Image, error) {
	if img == nil || img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return nil, ErrBadSize
	}
	scale := opt.DitherScale
	if scale == 0 {
		scale = 1
	}
	pix, err := dither.Quantize(img.W, img.H, img.Pix, dither.Options{
		Levels:      opt.Levels,
		Beta:        opt.Beta,
		Seed:        opt.Seed,
		DitherScale: scale,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadOption, err)
	}
	return &Image{W: img.W, H: img.H, Pix: pix}, nil
}
