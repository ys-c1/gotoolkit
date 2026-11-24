package api

import (
	"fmt"
	"math"

	"gotoolkit/internal/codec"
)

// FrameMetric 是一帧相对原始输入的累积误差实测。
type FrameMetric struct {
	Frame            int     `json:"frame"`
	Keyframe         bool    `json:"keyframe"`
	Depth            int     `json:"depth"`
	QuantMaxAbs      int     `json:"quant_max_abs"`
	TheoryBound      int     `json:"theory_bound"`
	TriangleBound    int     `json:"triangle_bound"`
	ActualMaxAbs     int     `json:"actual_max_abs"`
	ActualMeanAbs    float64 `json:"actual_mean_abs"`
	ActualRMSE       float64 `json:"actual_rmse"`
	BoundExceeded    bool    `json:"bound_exceeded"`
	TriangleExceeded bool    `json:"triangle_exceeded"`
}

// PixelError 是某一帧中单个像素的实测误差。
// SignedError = 解码像素 - 原始像素；上界检查使用 AbsError。
type PixelError struct {
	Frame       int `json:"frame"`
	Index       int `json:"index"`
	X           int `json:"x"`
	Y           int `json:"y"`
	SignedError int `json:"signed_error"`
	AbsError    int `json:"abs_error"`
	QuantError  int `json:"quant_error"`
}

// ChainReport 是一条固定关键帧间隔的多帧差分链路实测报告。
type ChainReport struct {
	Format             string        `json:"format"`
	Frames             int           `json:"frames"`
	Width              int           `json:"width"`
	Height             int           `json:"height"`
	Interval           int           `json:"interval"`
	QuantStep          int           `json:"quant_step"`
	PerFrameErrorBound int           `json:"per_frame_error_bound"`
	TargetBudget       int           `json:"target_budget"`
	DerivedInterval    int           `json:"derived_interval"`
	LastTheoryBound    int           `json:"last_theory_bound"`
	LastTriangleBound  int           `json:"last_triangle_bound"`
	LastActualMaxAbs   int           `json:"last_actual_max_abs"`
	LastActualMeanAbs  float64       `json:"last_actual_mean_abs"`
	LastActualRMSE     float64       `json:"last_actual_rmse"`
	WithinBound        bool          `json:"within_bound"`
	WithinBudget       bool          `json:"within_budget"`
	ArtifactSizes      []int         `json:"artifact_sizes"`
	FrameMetrics       []FrameMetric `json:"frame_metrics"`
	PixelErrors        []PixelError  `json:"pixel_errors"`
}

// MaxQuantError 返回 8 bit 整数差分在量化步长 q 下的逐阶最大绝对误差 floor(q/2)。
func MaxQuantError(q int) int {
	if q <= 1 {
		return 0
	}
	return q / 2
}

// KeyframeIntervalForBudget 由单阶量化上界反解关键帧间隔。
//
// 若允许的末帧每像素累积误差为 budget，则 (K-1)*floor(q/2) <= budget，
// 因而 K = 1 + floor(budget / floor(q/2))。这里必须向下取整；
// q<=1 时量化无损，返回最保守的 K=1。
func KeyframeIntervalForBudget(budget, q int) int {
	if budget < 0 {
		budget = 0
	}
	e := MaxQuantError(q)
	if e == 0 {
		return 1
	}
	return 1 + budget/e
}

// CumulativeErrorBound 返回距上一关键帧 depth 个 P 帧时的理论累积绝对误差上界。
func CumulativeErrorBound(depth, q int) int {
	if depth < 0 {
		depth = 0
	}
	return depth * MaxQuantError(q)
}

func validateSequence(frames []*Image, opt Options) (Options, error) {
	if err := opt.validate(); err != nil {
		return Options{}, err
	}
	opt = opt.chainOptions()
	if len(frames) == 0 {
		return Options{}, fmt.Errorf("%w: 空帧序列", ErrBadSize)
	}
	w, h := frames[0].W, frames[0].H
	if w <= 0 || h <= 0 {
		return Options{}, ErrBadSize
	}
	for i, f := range frames {
		if f == nil {
			return Options{}, fmt.Errorf("%w: 第 %d 帧为空", ErrBadSize, i)
		}
		if f.W != w || f.H != h || len(f.Pix) != w*h {
			return Options{}, fmt.Errorf("%w: 第 %d 帧画幅不一致", ErrBadSize, i)
		}
	}
	return opt, nil
}

func codingFrames(frames []*Image, gain int) []*Image {
	out := make([]*Image, len(frames))
	for i, f := range frames {
		out[i] = applyGain(f, gain)
	}
	return out
}

// EncodeSequence 按 KeyframeInterval 编码帧序列。
//
// 关键帧调用既有 Encode，因此 K=1 时每帧产物与当前单帧编码器逐字节一致；
// P 帧保存与前一原始帧之差的 GTD1 量化阶。解码端从关键帧开始逐阶累加，
// 因而量化误差会沿差分链累积，并由关键帧定期清零。
func EncodeSequence(frames []*Image, opt Options) ([][]byte, error) {
	opt, err := validateSequence(frames, opt)
	if err != nil {
		return nil, err
	}
	src := codingFrames(frames, opt.Gain)
	blobs := make([][]byte, len(src))
	for t, img := range src {
		if t%opt.KeyframeInterval == 0 {
			body, err := Encode(img, Options{Format: opt.Format})
			if err != nil {
				return nil, err
			}
			blobs[t] = body
			continue
		}

		prev := src[t-1]
		delta := make([]int, len(img.Pix))
		for i := range img.Pix {
			delta[i] = int(img.Pix[i]) - int(prev.Pix[i])
		}
		body, err := codec.EncodeDelta(img.W, img.H, opt.QuantStep, delta)
		if err != nil {
			return nil, err
		}
		blobs[t] = body
	}
	return blobs, nil
}

// DecodeSequence 解码 EncodeSequence 的产物。
// 关键帧按 opt.Format 解析；GTD1 负载自动识别。误差分析位于编码后的像素域，
// 因此这里不再对关键帧重复施加 Options.Gain 的逆变换。
func DecodeSequence(blobs [][]byte, opt Options) ([]*Image, error) {
	if err := opt.validate(); err != nil {
		return nil, err
	}
	opt = opt.chainOptions()
	out := make([]*Image, len(blobs))
	var prev *Image
	for t, blob := range blobs {
		var cur *Image
		if codec.IsDelta(blob) {
			if prev == nil {
				return nil, fmt.Errorf("%w: 第 %d 帧差分负载缺少关键帧", ErrTruncated, t)
			}
			w, h, q, levels, err := codec.DecodeDelta(blob)
			if err != nil {
				return nil, err
			}
			if w != prev.W || h != prev.H {
				return nil, ErrBadSize
			}
			pix := make([]uint8, w*h)
			for i, level := range levels {
				v := int(prev.Pix[i]) + int(level)*q
				if v < 0 {
					v = 0
				} else if v > 255 {
					v = 255
				}
				pix[i] = uint8(v)
			}
			cur = &Image{W: w, H: h, Pix: pix}
		} else {
			img, err := Decode(blob, Options{Format: opt.Format})
			if err != nil {
				return nil, fmt.Errorf("第 %d 帧关键帧: %w", t, err)
			}
			cur = img
		}
		out[t] = cur
		prev = cur
	}
	return out, nil
}

// AnalyzeSequence 完成编码、解码和逐帧/逐像素误差测量。
func AnalyzeSequence(frames []*Image, opt Options) (blobs [][]byte, decoded []*Image, report *ChainReport, err error) {
	opt, err = validateSequence(frames, opt)
	if err != nil {
		return nil, nil, nil, err
	}
	blobs, err = EncodeSequence(frames, opt)
	if err != nil {
		return nil, nil, nil, err
	}
	decoded, err = DecodeSequence(blobs, opt)
	if err != nil {
		return blobs, nil, nil, err
	}

	src := codingFrames(frames, opt.Gain)
	n := src[0].W * src[0].H
	eBound := MaxQuantError(opt.QuantStep)
	metrics := make([]FrameMetric, len(src))
	pixels := make([]PixelError, 0, len(src)*n)
	triangle := 0

	for t, want := range src {
		key := t%opt.KeyframeInterval == 0
		depth := 0
		if !key {
			depth = t - (t/opt.KeyframeInterval)*opt.KeyframeInterval
		} else {
			triangle = 0
		}

		qmax := 0
		quantErrs := make([]int, n)
		if !key {
			prev := src[t-1]
			for i := range want.Pix {
				d := int(want.Pix[i]) - int(prev.Pix[i])
				level := codec.QuantLevel(d, opt.QuantStep)
				qe := level*opt.QuantStep - d
				quantErrs[i] = qe
				if a := absInt(qe); a > qmax {
					qmax = a
				}
			}
			triangle += qmax
		}

		var sumAbs, sumSq int64
		maxAbs := 0
		for i := range want.Pix {
			signed := int(decoded[t].Pix[i]) - int(want.Pix[i])
			a := absInt(signed)
			if a > maxAbs {
				maxAbs = a
			}
			sumAbs += int64(a)
			sumSq += int64(signed * signed)
			pixels = append(pixels, PixelError{
				Frame:       t,
				Index:       i,
				X:           i % want.W,
				Y:           i / want.W,
				SignedError: signed,
				AbsError:    a,
				QuantError:  quantErrs[i],
			})
		}

		theory := depth * eBound
		m := FrameMetric{
			Frame:            t,
			Keyframe:         key,
			Depth:            depth,
			QuantMaxAbs:      qmax,
			TheoryBound:      theory,
			TriangleBound:    triangle,
			ActualMaxAbs:     maxAbs,
			ActualMeanAbs:    float64(sumAbs) / float64(n),
			ActualRMSE:       math.Sqrt(float64(sumSq) / float64(n)),
			BoundExceeded:    maxAbs > theory,
			TriangleExceeded: maxAbs > triangle,
		}
		if key {
			// 关键帧为既有无损格式；显式保证两个累积界在第 0 深度为 0。
			the := 0
			m.TheoryBound = the
			m.TriangleBound = 0
			m.BoundExceeded = maxAbs > 0
			m.TriangleExceeded = m.BoundExceeded
		}
		metrics[t] = m
	}

	last := metrics[len(metrics)-1]
	sizes := make([]int, len(blobs))
	for i, b := range blobs {
		sizes[i] = len(b)
	}
	report = &ChainReport{
		Format:             opt.Format,
		Frames:             len(src),
		Width:              src[0].W,
		Height:             src[0].H,
		Interval:           opt.KeyframeInterval,
		QuantStep:          opt.QuantStep,
		PerFrameErrorBound: eBound,
		TargetBudget:       last.TheoryBound,
		DerivedInterval:    KeyframeIntervalForBudget(last.TheoryBound, opt.QuantStep),
		LastTheoryBound:    last.TheoryBound,
		LastTriangleBound:  last.TriangleBound,
		LastActualMaxAbs:   last.ActualMaxAbs,
		LastActualMeanAbs:  last.ActualMeanAbs,
		LastActualRMSE:     last.ActualRMSE,
		WithinBound:        !last.BoundExceeded,
		WithinBudget:       !last.BoundExceeded,
		ArtifactSizes:      sizes,
		FrameMetrics:       metrics,
		PixelErrors:        pixels,
	}
	return blobs, decoded, report, nil
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
