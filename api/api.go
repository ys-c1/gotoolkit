// Package api 是 gotoolkit 唯一的对外入口。
// 调用方只能看到本包的类型与函数；具体格式实现放在 internal 下，外部不可引用。
package api

import (
	"errors"
	"fmt"
	"math"

	"gotoolkit/internal/codec"
	"gotoolkit/internal/rd"
)

// Options 是所有编解码参数的公共入口。
// 新增可配参数必须加在这里并写明默认值，否则调用方无法设置。
type Options struct {
	Format string // pgm | bmp | rle，默认 pgm
	Gain   int    // 亮度增益百分比，0 表示不调整，默认 0

	// 率失真能力（默认关闭，关闭时三格式输出与基准逐字节一致）。
	EnableRD  bool    // 是否启用率失真编码
	TargetBPP float64 // 目标每像素比特；>0 且有限才合法
	Step      int     // 手动量化步长；0 表示按 TargetBPP 自动反解+搜索
}

// Image 是解码后的灰度图。
type Image struct {
	W, H int
	Pix  []uint8
}

// RDReport 是一次率失真编码的对账记录。
// 目标不可达时不报错：Reachable=false，字段给出最接近的步长与差距。
type RDReport struct {
	Format string
	W, H   int

	TargetBPP  float64 // 目标每像素比特
	TargetBits int     // 目标比特 = round(TargetBPP × 像素数)
	ActualBPP  float64 // 实际每像素比特
	ActualBits int     // 实际比特 = 编码字节 × 8
	Bytes      int     // 实际编码字节数

	Step        int     // 最终采用的量化步长
	InitialStep int     // 按目标反解出的初始步长（手动指定时等于 Step）
	Reachable   bool    // 实际比特是否恰好等于目标比特
	GapBits     int     // 实际与目标的比特差绝对值
	GapBPP      float64 // 实际与目标的 bpp 差绝对值
	GapRatio    float64 // 差距占目标的比例

	Body []byte // 最终编码字节
}

var (
	ErrUnknownFormat = errors.New("未知格式")
	ErrBadSize       = errors.New("画幅必须为正")
	ErrBadOption     = errors.New("参数越界")
	ErrTruncated     = errors.New("数据被截断")
	ErrBadTarget     = errors.New("目标比特率非法")
	ErrZeroStep      = errors.New("量化步长为零")
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
	if o.EnableRD {
		if o.TargetBPP <= 0 || math.IsNaN(o.TargetBPP) || math.IsInf(o.TargetBPP, 0) {
			return fmt.Errorf("%w: TargetBPP=%v 必须为正且有限", ErrBadTarget, o.TargetBPP)
		}
		if o.Step < 0 || o.Step > rd.MaxStep {
			return fmt.Errorf("%w: Step=%d 超出 [0,%d]", ErrBadOption, o.Step, rd.MaxStep)
		}
	}
	return nil
}

// Encode 把灰度图编码成指定格式。
// 启用率失真时走量化重建路径；关闭时与基准实现完全一致。
func Encode(img *Image, opt Options) ([]byte, error) {
	if img == nil || img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return nil, ErrBadSize
	}
	if err := opt.validate(); err != nil {
		return nil, err
	}
	src := img
	if opt.Gain != 0 {
		src = applyGain(img, opt.Gain)
	}
	if !opt.EnableRD {
		return encodePlain(src, opt)
	}
	report, err := encodeRD(src, opt)
	if err != nil {
		return nil, err
	}
	return report.Body, nil
}

// EncodeRD 执行率失真编码并返回完整对账报告。
// 目标不可达时报告中 Reachable=false 并给出最接近结果，不返回错误。
func EncodeRD(img *Image, opt Options) (*RDReport, error) {
	if img == nil || img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return nil, ErrBadSize
	}
	if err := opt.validate(); err != nil {
		return nil, err
	}
	src := img
	if opt.Gain != 0 {
		src = applyGain(img, opt.Gain)
	}
	return encodeRD(src, opt)
}

func encodePlain(img *Image, opt Options) ([]byte, error) {
	switch opt.Format {
	case "pgm":
		return codec.EncodePGM(img.W, img.H, img.Pix)
	case "bmp":
		return codec.EncodeBMP(img.W, img.H, img.Pix)
	default:
		return codec.EncodeRLE(img.W, img.H, img.Pix)
	}
}

func encodeRD(img *Image, opt Options) (*RDReport, error) {
	enc, ok := rd.Encoders(opt.Format)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownFormat, opt.Format)
	}

	// 手动步长：直接量化重建并对账，不做搜索。
	if opt.Step != 0 {
		recon, _ := rd.Reconstruct(img.W, img.H, img.Pix, opt.Step)
		body, err := enc(img.W, img.H, recon)
		if err != nil {
			return nil, err
		}
		return buildReport(opt, img, rd.SearchResult{
			Best: rd.Candidate{
				Step:  opt.Step,
				Bytes: len(body),
				Bits:  len(body) * 8,
				BPP:   float64(len(body)*8) / float64(img.W*img.H),
				Recon: recon,
				Body:  body,
			},
			TargetBits:  int(math.Round(opt.TargetBPP * float64(img.W*img.H))),
			TargetBPP:   opt.TargetBPP,
			InitialStep: opt.Step,
			Recon:       recon,
			Body:        body,
		}), nil
	}

	// 自动步长：先反解，反解为 0 说明目标比最细步长还高，按错误码报出。
	raw := rd.InverseStep(img.W, img.H, img.Pix, opt.TargetBPP)
	if raw == 0 {
		return nil, fmt.Errorf("%w: TargetBPP=%v 反解步长为 0（最细步长 %d 仍达不到目标）",
			ErrZeroStep, opt.TargetBPP, rd.MinStep)
	}

	res, err := rd.Search(img.W, img.H, img.Pix, opt.TargetBPP, enc)
	if err != nil {
		return nil, err
	}
	return buildReport(opt, img, *res), nil
}

func buildReport(opt Options, img *Image, res rd.SearchResult) *RDReport {
	n := img.W * img.H
	targetBits := int(math.Round(opt.TargetBPP * float64(n)))
	gapBits := res.Best.Bits - targetBits
	if gapBits < 0 {
		gapBits = -gapBits
	}
	report := &RDReport{
		Format:      opt.Format,
		W:           img.W,
		H:           img.H,
		TargetBPP:   opt.TargetBPP,
		TargetBits:  targetBits,
		ActualBPP:   res.Best.BPP,
		ActualBits:  res.Best.Bits,
		Bytes:       res.Best.Bytes,
		Step:        res.Best.Step,
		InitialStep: res.InitialStep,
		Reachable:   res.Best.Bits == targetBits,
		GapBits:     gapBits,
		GapBPP:      math.Abs(res.Best.BPP - opt.TargetBPP),
		Body:        res.Body,
	}
	if targetBits > 0 {
		report.GapRatio = float64(gapBits) / float64(targetBits)
	} else {
		report.GapRatio = report.GapBPP
	}
	return report
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
