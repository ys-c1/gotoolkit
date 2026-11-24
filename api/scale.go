package api

import (
	"fmt"
	"math"

	"gotoolkit/internal/scale"
)

// FrameReport 是补偿量在一档画幅上的复算结果。
type FrameReport struct {
	Name           string
	W, H           int
	OutW, OutH     int
	MaxEncodedBias int
	MaxResidual    int
}

// GammaReport 是一个缩放比对应的解析关系和实测残差。
type GammaReport struct {
	Denom            int
	Ratio            float64
	Compensation     float64
	BiasAtDark32     float64
	Encoded32        uint8
	GammaDual32      uint8
	Compensated32    uint8
	ResidualAtDark32 int
	Frames           []FrameReport
	WithinTolerance  bool
}

// Scale 按 Options.Ratio 指定的整数盒缩小比缩放灰度图。
// Options.ScaleSpace=encoded 时保留直接在编码值上平均的原路径。
func Scale(img *Image, opt Options) (*Image, error) {
	if img == nil {
		return nil, ErrBadSize
	}
	if img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return nil, ErrBadSize
	}
	denom, err := ratioDenom(opt.Ratio)
	if err != nil {
		return nil, err
	}
	gamma := opt.Gamma
	if gamma == 0 {
		gamma = 2.2
	}
	mode := scale.Mode(opt.ScaleSpace)
	if opt.ScaleSpace == "" {
		mode = scale.ModeEncoded
	}

	src := scale.Image{W: img.W, H: img.H, Pix: img.Pix}
	out, err := scale.Box(src, denom, gamma, mode)
	if err != nil {
		return nil, err
	}
	return &Image{W: out.W, H: out.H, Pix: out.Pix}, nil
}

// GammaLab 在 640x480 与 1920x1080 两档画幅上复算三档缩放比。
func GammaLab(opt Options) ([]GammaReport, error) {
	gamma := opt.Gamma
	if gamma == 0 {
		gamma = 2.2
	}
	tolerance := opt.Tolerance
	if tolerance == 0 {
		tolerance = 1
	}
	rows, err := scale.RunLab(gamma, tolerance, scale.DefaultFrames())
	if err != nil {
		return nil, err
	}

	out := make([]GammaReport, 0, len(rows))
	for _, r := range rows {
		gr := GammaReport{
			Denom:            r.Denom,
			Ratio:            r.Ratio,
			Compensation:     r.K,
			BiasAtDark32:     r.BiasAt32,
			Encoded32:        r.Encoded32,
			GammaDual32:      r.Dual32,
			Compensated32:    r.Comp32,
			ResidualAtDark32: r.Residual32,
			WithinTolerance:  r.Pass,
		}
		for _, f := range r.Frames {
			gr.Frames = append(gr.Frames, FrameReport{
				Name:           f.Name,
				W:              f.W,
				H:              f.H,
				OutW:           f.OutW,
				OutH:           f.OutH,
				MaxEncodedBias: f.MaxEncodedBias,
				MaxResidual:    f.MaxAbsResidual,
			})
		}
		out = append(out, gr)
	}
	return out, nil
}

// EdgeBias 返回“偏亮”符号约定下的解析偏差 C_E-C_D。
func EdgeBias(cDark byte, ratio, gamma float64) float64 {
	return scale.EdgeBias(float64(cDark), ratio, gamma)
}

// EdgeCompensation 返回反解出的乘性补偿量 K=ratio^(gamma-1)。
func EdgeCompensation(ratio, gamma float64) float64 {
	return scale.EdgeCompensation(ratio, gamma)
}

func ratioDenom(ratio float64) (int, error) {
	if ratio <= 0 || ratio > 1 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return 0, fmt.Errorf("%w: Ratio=%v 不在 (0,1]", ErrBadOption, ratio)
	}
	n := math.Round(1 / ratio)
	if math.Abs(1/ratio-n) > 1e-9 {
		return 0, fmt.Errorf("%w: Ratio=%v 必须是 1/N 的整数盒比例", ErrBadOption, ratio)
	}
	if n < 1 {
		return 0, fmt.Errorf("%w: Ratio=%v", ErrBadOption, ratio)
	}
	return int(n), nil
}
