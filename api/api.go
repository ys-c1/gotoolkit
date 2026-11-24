// Package api 是 gotoolkit 唯一的对外入口。
// 调用方只能看到本包的类型与函数；具体格式实现放在 internal 下，外部不可引用。
package api

import (
	"errors"
	"fmt"

	"gotoolkit/internal/codec"
	"gotoolkit/internal/imgfx"
)

// Options 是所有编解码参数的公共入口。
// 新增可配参数必须加在这里并写明默认值，否则调用方无法设置。
type Options struct {
	Format string // pgm | bmp | rle | idx，默认 pgm

	Gain int // 亮度增益百分比，0 表示不调整，默认 0

	// 缩略图链路三开关，默认全部关闭（零值即关闭），关闭时产物与旧版逐字节一致。
	Palette bool // 是否做调色板索引（量化到 Buckets 个等间距灰度级）
	Dither  bool // 是否在索引时做 Floyd-Steinberg 抖动；仅在 Palette 打开时生效
	// ThumbDiv 为面积平均缩略的除数：>1 按 ThumbDiv×ThumbDiv 分块平均，
	// 0（默认）表示不缩略；负数非法，返回 ErrBadThumbDiv。
	ThumbDiv int
	// Buckets 为调色板桶数。Palette 打开时必须 >0，否则返回 ErrZeroBuckets；
	// 上限 255（GTID 头用一个字节记录桶数）。
	Buckets int
}

// Image 是解码后的灰度图。
type Image struct {
	W, H int
	Pix  []uint8
}

// PipelineReport 汇报缩略图链路的可复算指标。
// 缩略始终先于索引执行；索引不去重，因此索引数量等于被索引图像的像素总数。
type PipelineReport struct {
	ThumbApplied   bool
	ThumbDiv       int
	OrigW, OrigH   int // 缩略前画幅
	ThumbW, ThumbH int // 缩略后画幅（未缩略时与原值相同）

	PaletteApplied bool
	Buckets        int
	DitherApplied  bool
	// 抖动强度 = 1/DitherStrengthDen，分母取缩略除数（未缩略时为 1）。
	DitherStrengthNum int
	DitherStrengthDen int

	// IndexCountBefore 为缩略前（原图）索引数量，IndexCountAfter 为缩略后索引数量；
	// 差值 Delta = After - Before，可由两列直接复算。Palette 关闭时三者均为 0。
	IndexCountBefore int
	IndexCountAfter  int
	Delta            int
}

var (
	ErrUnknownFormat = errors.New("未知格式")
	ErrBadSize       = errors.New("画幅必须为正")
	ErrBadOption     = errors.New("参数越界")
	ErrTruncated     = errors.New("数据被截断")
	// ErrZeroBuckets：调色板打开但桶数为零。
	ErrZeroBuckets = errors.New("调色板桶数为零")
	// ErrBadThumbDiv：缩略除数为负（零表示不缩略，合法）。
	ErrBadThumbDiv = errors.New("缩略除数为负")
)

// DefaultOptions 返回默认参数。
func DefaultOptions() Options {
	return Options{Format: "pgm", Gain: 0}
}

func (o Options) validate() error {
	switch o.Format {
	case "pgm", "bmp", "rle", "idx":
	default:
		return fmt.Errorf("%w: %q", ErrUnknownFormat, o.Format)
	}
	if o.Gain < -100 || o.Gain > 100 {
		return fmt.Errorf("%w: Gain=%d 超出 [-100,100]", ErrBadOption, o.Gain)
	}
	if o.ThumbDiv < 0 {
		return fmt.Errorf("%w: ThumbDiv=%d", ErrBadThumbDiv, o.ThumbDiv)
	}
	if o.Palette && o.Buckets == 0 {
		return fmt.Errorf("%w: Buckets=%d", ErrZeroBuckets, o.Buckets)
	}
	if o.Buckets < 0 || o.Buckets > 255 {
		return fmt.Errorf("%w: Buckets=%d 超出 [0,255]", ErrBadOption, o.Buckets)
	}
	return nil
}

// validateForEncode 追加仅编码路径适用的约束：idx 容器必须携带调色板。
func (o Options) validateForEncode() error {
	if err := o.validate(); err != nil {
		return err
	}
	if o.Format == "idx" && !o.Palette {
		return fmt.Errorf("%w: idx 格式必须打开调色板", ErrBadOption)
	}
	return nil
}

// Encode 把灰度图编码成指定格式。行为与报告版一致，仅省略链路报告。
func Encode(img *Image, opt Options) ([]byte, error) {
	body, _, err := EncodeWithReport(img, opt)
	return body, err
}

// EncodeWithReport 按固定链路编码：增益 → 面积平均缩略 → 调色板索引（含抖动）→ 格式封装，
// 并返回链路报告。任何新开关为零值时，对应步骤整体跳过，产物与旧版一致。
func EncodeWithReport(img *Image, opt Options) ([]byte, *PipelineReport, error) {
	if img == nil {
		return nil, nil, ErrBadSize
	}
	if img.W <= 0 || img.H <= 0 {
		return nil, nil, ErrBadSize
	}
	if len(img.Pix) != img.W*img.H {
		return nil, nil, ErrBadSize
	}
	if err := opt.validateForEncode(); err != nil {
		return nil, nil, err
	}
	report := &PipelineReport{OrigW: img.W, OrigH: img.H}

	src := img
	if opt.Gain != 0 {
		src = applyGain(img, opt.Gain)
	}

	// 第一步：面积平均缩略（缩略后再索引）。
	if opt.ThumbDiv > 1 {
		nw, nh, pix, err := imgfx.Thumbnail(src.W, src.H, src.Pix, opt.ThumbDiv)
		if err != nil {
			return nil, nil, err
		}
		src = &Image{W: nw, H: nh, Pix: pix}
		report.ThumbApplied = true
		report.ThumbDiv = opt.ThumbDiv
	}
	report.ThumbW, report.ThumbH = src.W, src.H

	// 第二步：调色板索引（索引不去重），抖动强度按缩略比例调整。
	var idxRes *imgfx.IndexResult
	if opt.Palette {
		strengthDen := 1
		if report.ThumbApplied {
			strengthDen = opt.ThumbDiv
		}
		res, err := imgfx.IndexImage(src.W, src.H, src.Pix, opt.Buckets, opt.Dither, strengthDen)
		if err != nil {
			return nil, nil, err
		}
		idxRes = res
		report.PaletteApplied = true
		report.Buckets = opt.Buckets
		report.DitherApplied = opt.Dither
		report.DitherStrengthNum = 1
		report.DitherStrengthDen = strengthDen
		// 索引数量按缩略前/后两列对照，差值可复算：Delta = After - Before。
		report.IndexCountBefore = img.W * img.H
		report.IndexCountAfter = src.W * src.H
		report.Delta = report.IndexCountAfter - report.IndexCountBefore
	}

	// 第三步：格式封装。
	switch opt.Format {
	case "pgm":
		if idxRes != nil {
			body, err := codec.EncodePGM(src.W, src.H, idxRes.Quant)
			return body, report, err
		}
		body, err := codec.EncodePGM(src.W, src.H, src.Pix)
		return body, report, err
	case "bmp":
		if idxRes != nil {
			body, err := codec.EncodeBMP(src.W, src.H, idxRes.Quant)
			return body, report, err
		}
		body, err := codec.EncodeBMP(src.W, src.H, src.Pix)
		return body, report, err
	case "rle":
		if idxRes != nil {
			body, err := codec.EncodeRLE(src.W, src.H, idxRes.Quant)
			return body, report, err
		}
		body, err := codec.EncodeRLE(src.W, src.H, src.Pix)
		return body, report, err
	default: // idx，validate 已保证调色板打开
		body, err := codec.EncodeIDX(src.W, src.H, idxRes.Palette, idxRes.Index)
		return body, report, err
	}
}

// Decode 把字节流解码成灰度图。
func Decode(data []byte, opt Options) (*Image, error) {
	// 先做参数校验，保证非法选项（如负缩略除数）即使在空数据上也返回对应错误码。
	if err := opt.validate(); err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, ErrTruncated
	}
	var w, h int
	var pix []uint8
	var err error
	switch opt.Format {
	case "pgm":
		w, h, pix, err = codec.DecodePGM(data)
	case "bmp":
		w, h, pix, err = codec.DecodeBMP(data)
	case "idx":
		w, h, pix, err = codec.DecodeIDX(data)
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
