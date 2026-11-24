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

	// 压缩链路三项开关，默认全部关闭；全关时输出字节流与基准完全一致。
	Predict      bool   // 预测编码，默认 false
	PredictOrder int    // 预测阶数：0=自动（按残差熵最小从候选集 {1,2,3} 选），默认 0；负值或 >3 报 ErrBadPredictOrder
	MergeRuns    bool   // 游程合并，在预测之后执行，默认 false
	Checksum     string // 体校验和算法：""=关闭（默认），"crc32"=写进文件头偏移 6 处；其他值报 ErrBadChecksum
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

	ErrBadPredictOrder  = errors.New("预测阶数非法")
	ErrBadChecksum      = errors.New("校验参数非法")
	ErrChecksumMismatch = errors.New("体校验和不匹配")
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
	if o.PredictOrder < 0 || o.PredictOrder > 3 {
		return fmt.Errorf("%w: PredictOrder=%d 不在 {0,1,2,3}", ErrBadPredictOrder, o.PredictOrder)
	}
	switch o.Checksum {
	case "", "crc32":
	default:
		return fmt.Errorf("%w: %q", ErrBadChecksum, o.Checksum)
	}
	return nil
}

// pipelineOn 报告压缩链路是否有任一项启用。
func (o Options) pipelineOn() bool {
	return o.Predict || o.MergeRuns || o.Checksum != ""
}

func formatID(format string) uint8 {
	switch format {
	case "bmp":
		return codec.FormatBMP
	case "rle":
		return codec.FormatRLE
	default:
		return codec.FormatPGM
	}
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
	var body []byte
	var err error
	switch opt.Format {
	case "pgm":
		body, err = codec.EncodePGM(src.W, src.H, src.Pix)
	case "bmp":
		body, err = codec.EncodeBMP(src.W, src.H, src.Pix)
	default:
		body, err = codec.EncodeRLE(src.W, src.H, src.Pix)
	}
	if err != nil {
		return nil, err
	}
	if !opt.pipelineOn() {
		return body, nil // 三项全关：字节流保持基准不变
	}
	return codec.EncodePipe(body, formatID(opt.Format), codec.PipeOptions{
		Predict:   opt.Predict,
		Order:     opt.PredictOrder,
		MergeRuns: opt.MergeRuns,
		Checksum:  opt.Checksum,
	})
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
	if codec.IsPipe(data) {
		// 带链路头的容器：内层格式以头部记录为准，忽略 opt.Format
		body, fid, perr := codec.DecodePipe(data)
		if perr != nil {
			if errors.Is(perr, codec.ErrChecksumMismatch) {
				return nil, fmt.Errorf("%w", ErrChecksumMismatch)
			}
			return nil, perr
		}
		switch fid {
		case codec.FormatBMP:
			w, h, pix, err = codec.DecodeBMP(body)
		case codec.FormatRLE:
			w, h, pix, err = codec.DecodeRLE(body)
		default:
			w, h, pix, err = codec.DecodePGM(body)
		}
	} else {
		switch opt.Format {
		case "pgm":
			w, h, pix, err = codec.DecodePGM(data)
		case "bmp":
			w, h, pix, err = codec.DecodeBMP(data)
		default:
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
