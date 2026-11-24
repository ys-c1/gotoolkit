package api

import (
	"errors"
	"fmt"
	"io"

	"gotoolkit/internal/codec"
)

// 两档内存预算（字节）：
//
//	BudgetTight    紧张档，强制走分块流式路径；
//	BudgetGenerous 充足档，允许原路径整图解码。
//
// 0 表示不限预算，保持历史默认行为。
const (
	BudgetTight    int64 = 512 << 10 // 512 KiB
	BudgetGenerous int64 = 8 << 20   // 8 MiB
)

// ErrBudget 是解码因内存预算被拒绝的哨兵错误，可用 errors.Is 识别。
var ErrBudget = errors.New("内存预算不足")

// DecodeStats 是一次解码的核算与实测信息，所有字节数均含固定余量口径。
type DecodeStats struct {
	Path      string // whole | chunked
	BandRows  int
	BandBytes int64
	ModelPeak int64 // 模型峰值（字节）
	W, H      int
	Pixels    int64
}

// MaxDecodablePixels 按预算反解最大可解码像素数：
// whole 为原路径上限，chunked 为分块流式路径的线性保守上限。
func MaxDecodablePixels(format string, budget int64) (whole, chunked int64, err error) {
	if budget < 0 {
		return 0, 0, fmt.Errorf("%w: 预算不能为负", ErrBadOption)
	}
	switch format {
	case "pgm", "bmp", "rle":
	default:
		return 0, 0, fmt.Errorf("%w: %q", ErrUnknownFormat, format)
	}
	w, c := codec.MaxDecodablePixels(format, budget)
	return w, c, nil
}

// MaxChunkedSize 是宽度感知的分块路径反解：给定宽度 w 与预算，
// 返回可流式解码的最大像素数与最大高度（行数）。
// 对窄而高的画幅，该上限可远超 MaxDecodablePixels 的整图上限。
func MaxChunkedSize(format string, w int, budget int64) (pixels, height int64, err error) {
	if budget < 0 {
		return 0, 0, fmt.Errorf("%w: 预算不能为负", ErrBadOption)
	}
	if w <= 0 {
		return 0, 0, fmt.Errorf("%w: 宽度必须为正", ErrBadSize)
	}
	switch format {
	case "pgm", "bmp", "rle":
	default:
		return 0, 0, fmt.Errorf("%w: %q", ErrUnknownFormat, format)
	}
	p, h := codec.MaxChunkedSize(format, w, budget)
	return p, h, nil
}

// memSink 把行带合并进预分配的整图像素缓冲，供测试与逐块比对使用。
type memSink struct {
	pix []uint8
	w   int
}

func (s *memSink) WriteBand(y0, rows int, band []uint8) error {
	copy(s.pix[y0*s.w:(y0+rows)*s.w], band)
	return nil
}

// pgmSink 把行带以 P5 灰度直接写入 w，头部只写一次，避免再持有整图缓冲。
type pgmSink struct {
	w          io.Writer
	wImg, hImg int
	headerDone bool
}

func (s *pgmSink) WriteBand(_, _ int, band []uint8) error {
	if !s.headerDone {
		if _, err := fmt.Fprintf(s.w, "P5\n%d %d\n255\n", s.wImg, s.hImg); err != nil {
			return err
		}
		s.headerDone = true
	}
	_, err := s.w.Write(band)
	return err
}

// DecodeTo 把编码流按预算解码并逐块写入 sink。
// 预算充足时走原路径（与 Decode 完全相同的解码器，输出按行带切片交给 sink）；
// 预算紧张时走 codec.DecodeBands 分块流式路径。两条路径的产物逐字节相同。
// 返回的 DecodeStats 记录实际路径与模型峰值。
func DecodeTo(data []byte, opt Options, sink codec.BandSink) (*DecodeStats, error) {
	if len(data) == 0 {
		return nil, ErrTruncated
	}
	if err := opt.validate(); err != nil {
		return nil, err
	}
	plan, err := codec.PlanDecode(opt.Format, data, opt.MemBudget, opt.BandRows)
	if err != nil {
		return nil, err
	}
	stats := &DecodeStats{
		BandRows: plan.BandRows, BandBytes: plan.BandBytes,
		W: plan.W, H: plan.H, Pixels: plan.Pixels,
	}
	applyBandGain := func(band []uint8) {
		if opt.Gain == 0 {
			return
		}
		g := -opt.Gain // 与 Decode 的逆增益口径一致
		for i, v := range band {
			x := int(v) + int(v)*g/100
			if x < 0 {
				x = 0
			}
			if x > 255 {
				x = 255
			}
			band[i] = uint8(x)
		}
	}
	// 原路径：预算为 0（不限）或整图峰值在预算内。
	if plan.CheckWhole(opt.MemBudget) == nil {
		var (
			w, h int
			pix  []uint8
			derr error
		)
		switch opt.Format {
		case "pgm":
			w, h, pix, derr = codec.DecodePGM(data)
		case "bmp":
			w, h, pix, derr = codec.DecodeBMP(data)
		default:
			w, h, pix, derr = codec.DecodeRLE(data)
		}
		if derr != nil {
			return nil, derr
		}
		if opt.Gain != 0 {
			applyBandGain(pix)
		}
		br := plan.BandRows
		if br <= 0 {
			br = h
		}
		for y0 := 0; y0 < h; y0 += br {
			rows := br
			if y0+rows > h {
				rows = h - y0
			}
			if err := sink.WriteBand(y0, rows, pix[y0*w:(y0+rows)*w]); err != nil {
				return nil, err
			}
		}
		stats.Path = "whole"
		stats.ModelPeak = plan.WholePeak
		return stats, nil
	}
	// 分块路径：先确认分块路径自身也在预算内，否则带结构化原因拒绝。
	if err := plan.CheckChunk(opt.MemBudget); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBudget, err)
	}
	// 分块解码内置逐块双实现比对与边界抽查，比对通过才回调 sink。
	// 若调用方要求增益，用包装 sink 在合并前对行带就地调整。
	chain := codec.BandSink(sink)
	if opt.Gain != 0 {
		chain = &gainSink{next: sink, gain: -opt.Gain}
	}
	if err := codec.DecodeBands(opt.Format, data, plan.BandRows, chain); err != nil {
		return nil, err
	}
	stats.Path = "chunked"
	stats.ModelPeak = plan.ChunkPeak
	return stats, nil
}

type gainSink struct {
	next codec.BandSink
	gain int
}

func (s *gainSink) WriteBand(y0, rows int, band []uint8) error {
	for i, v := range band {
		x := int(v) + int(v)*s.gain/100
		if x < 0 {
			x = 0
		}
		if x > 255 {
			x = 255
		}
		band[i] = uint8(x)
	}
	return s.next.WriteBand(y0, rows, band)
}

// DecodeToPGM 是 DecodeTo 的流式便捷封装：直接产出 P5 字节到 w，
// 全程不分配整图像素缓冲，适合超大画幅。
func DecodeToPGM(data []byte, opt Options, w io.Writer) (*DecodeStats, error) {
	plan, err := codec.PlanDecode(opt.Format, data, opt.MemBudget, opt.BandRows)
	if err != nil {
		return nil, err
	}
	sink := &pgmSink{w: w, wImg: plan.W, hImg: plan.H}
	return DecodeTo(data, opt, sink)
}
