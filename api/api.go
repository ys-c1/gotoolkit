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
	Format  string // pgm | bmp | rle，默认 pgm
	Gain    int    // 亮度增益百分比，0 表示不调整，默认 0
	Reorder bool   // 仅 rle：重排索引序列使游程数最少（GTR2 流），默认 false
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
		if opt.Reorder {
			return codec.EncodeRLE2(src.W, src.H, src.Pix)
		}
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
		// rle：按魔数自动识别 GTR1 / GTR2，无需调用方指定 Reorder
		if len(data) >= 4 && string(data[:4]) == "GTR2" {
			w, h, pix, err = codec.DecodeRLE2(data)
		} else {
			w, h, pix, err = codec.DecodeRLE(data)
		}
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

// CountRuns 统计整个索引序列的游程数（整图统计，不抽样）。
func CountRuns(pix []uint8) int { return codec.CountRuns(pix) }

// ReorderSequence 返回按灰度值稳定重排后的索引序列与位置置换，
// 重排后游程数达到理论最优 R* = k（不同索引个数）。
// 时间 Θ(n + 256)，空间 Θ(n)。
func ReorderSequence(pix []uint8) (seq []uint8, perm []int) {
	return codec.ReorderByValue(pix)
}

// BatchWindowRuns 是分窗贪心近似算法重排后的游程数，
// 误差界 R <= ⌈n/W⌉ * R*；window <= 0 表示不分窗（精确）。
func BatchWindowRuns(pix []uint8, window int) int {
	return codec.BatchWindowRuns(pix, window)
}
