// Package api 是 gotoolkit 唯一的对外入口。
// 调用方只能看到本包的类型与函数；具体格式实现放在 internal 下，外部不可引用。
package api

import (
	"errors"
	"fmt"
	"strconv"

	"gotoolkit/internal/codec"
)

// Options 是所有编解码参数的公共入口。
// 新增可配参数必须加在这里并写明默认值，否则调用方无法设置。
type Options struct {
	Format string // pgm | bmp | rle，默认 pgm
	Gain   int    // 亮度增益百分比，0 表示不调整，默认 0
	// PredictOrder 是预测编码的最大总阶数，只对 rle 生效；pgm/bmp
	// 忽略此值，输出字节不变。
	// 0 表示关闭预测（默认）；1..MaxPredictOrder 表示在候选集
	// {不预测} ∪ {KX+KY <= N} 中按残差一阶熵最小自动选，KX 为水平
	// 阶数、KY 为竖直阶数。
	// 负值或超过 MaxPredictOrder 一律报错（ErrBadPredictOrder）。
	PredictOrder int
}

// MaxPredictOrder 是 PredictOrder 的公共上限。
const MaxPredictOrder = codec.MaxPredictOrder

// PredictCandidate 是一个二维预测候选：水平方向阶数 KX 与竖直方向
// 阶数 KY，二者之和不超过 MaxPredictOrder。KX==KY==0 表示不预测。
// String 返回如 none、x1/y0、x1/y1 的短名。
type PredictCandidate struct {
	KX, KY int
}

// String 返回候选短名。
func (c PredictCandidate) String() string {
	if c.KX == 0 && c.KY == 0 {
		return "none"
	}
	return "x" + strconv.Itoa(c.KX) + "/y" + strconv.Itoa(c.KY)
}

// PredictStat 是某个候选的统计，供调用方打印选阶依据。
// Hist 是残差（uint8，模 256）在 0..255 上的计数。
type PredictStat struct {
	Candidate PredictCandidate
	Samples   int
	Entropy   float64 // 残差一阶熵，bit/符号
	Hist      [256]int
}

// EncodeReport 描述一次预测编码的选阶结果。
type EncodeReport struct {
	Selected      PredictCandidate // 按残差一阶熵选中的候选；零值表示不预测
	Emitted       PredictCandidate // 实际写进流的候选；零值表示落回 GTR1
	FallbackToGTR bool             // 选中候选未缩小体积，已落回 GTR1
	Samples       int              // 统计样本数，等于 W*H
	Stats         []PredictStat
	Bytes         int // 实际输出字节数
	BaselineBytes int // 同一图像关闭预测时的 GTR1 字节数
}

// EncodeWithReport 与 Encode 行为一致，但在启用预测时额外返回选阶依据。
// 未启用预测（PredictOrder==0 或格式不是 rle）时 report 为 nil。
func EncodeWithReport(img *Image, opt Options) ([]byte, *EncodeReport, error) {
	body, report, err := encode(img, opt)
	if err != nil {
		return nil, nil, err
	}
	return body, report, nil
}

func toReport(info *codec.PredictInfo, n int) *EncodeReport {
	if info == nil {
		return nil
	}
	rep := &EncodeReport{
		Selected:      PredictCandidate{KX: info.Selected.KX, KY: info.Selected.KY},
		Emitted:       PredictCandidate{KX: info.Emitted.KX, KY: info.Emitted.KY},
		FallbackToGTR: info.FallbackToGTR,
		Samples:       info.Samples,
		Bytes:         n,
		BaselineBytes: info.BaselineBytes,
		Stats:         make([]PredictStat, len(info.Stats)),
	}
	for i, st := range info.Stats {
		rep.Stats[i] = PredictStat{
			Candidate: PredictCandidate{KX: st.Candidate.KX, KY: st.Candidate.KY},
			Samples:   st.Samples,
			Entropy:   st.Entropy,
			Hist:      st.Hist,
		}
	}
	return rep
}

// Image 是解码后的灰度图。
type Image struct {
	W, H int
	Pix  []uint8
}

var (
	ErrUnknownFormat   = errors.New("未知格式")
	ErrBadSize         = errors.New("画幅必须为正")
	ErrBadOption       = errors.New("参数越界")
	ErrBadPredictOrder = errors.New("预测阶数越界")
	ErrTruncated       = errors.New("数据被截断")
)

// DefaultOptions 返回默认参数。
func DefaultOptions() Options { return Options{Format: "pgm", Gain: 0, PredictOrder: 0} }

func (o Options) validate() error {
	switch o.Format {
	case "pgm", "bmp", "rle":
	default:
		return fmt.Errorf("%w: %q", ErrUnknownFormat, o.Format)
	}
	if o.Gain < -100 || o.Gain > 100 {
		return fmt.Errorf("%w: Gain=%d 超出 [-100,100]", ErrBadOption, o.Gain)
	}
	if o.PredictOrder < 0 || o.PredictOrder > MaxPredictOrder {
		return fmt.Errorf("%w: PredictOrder=%d 超出 [0,%d]", ErrBadPredictOrder, o.PredictOrder, MaxPredictOrder)
	}
	return nil
}

// Encode 把灰度图编码成指定格式。
func Encode(img *Image, opt Options) ([]byte, error) {
	body, _, err := encode(img, opt)
	return body, err
}

func encode(img *Image, opt Options) ([]byte, *EncodeReport, error) {
	if img == nil {
		return nil, nil, ErrBadSize
	}
	if img.W <= 0 || img.H <= 0 {
		return nil, nil, ErrBadSize
	}
	if len(img.Pix) != img.W*img.H {
		return nil, nil, ErrBadSize
	}
	if err := opt.validate(); err != nil {
		return nil, nil, err
	}
	src := img
	if opt.Gain != 0 {
		src = applyGain(img, opt.Gain)
	}
	switch opt.Format {
	case "pgm":
		body, err := codec.EncodePGM(src.W, src.H, src.Pix)
		return body, nil, err
	case "bmp":
		body, err := codec.EncodeBMP(src.W, src.H, src.Pix)
		return body, nil, err
	default:
		if opt.PredictOrder > 0 {
			body, info, err := codec.EncodeRLEPredict(src.W, src.H, src.Pix, opt.PredictOrder)
			if err != nil {
				return nil, nil, err
			}
			return body, toReport(info, len(body)), nil
		}
		body, err := codec.EncodeRLE(src.W, src.H, src.Pix)
		return body, nil, err
	}
}

// DecodeReport 描述一次解码的格式信息。
// 对 GTR2 流，PredictOrder 是流内记录的预测阶数；其他格式恒为 0。
type DecodeReport struct {
	Format  string
	Predict PredictCandidate // GTR2 流内候选；其他格式为零值
	Bytes   int
}

// Decode 把字节流解码成灰度图。
func Decode(data []byte, opt Options) (*Image, error) {
	img, _, err := DecodeWithReport(data, opt)
	return img, err
}

// DecodeWithReport 与 Decode 行为一致，并额外返回解码格式信息。
func DecodeWithReport(data []byte, opt Options) (*Image, *DecodeReport, error) {
	if len(data) == 0 {
		return nil, nil, ErrTruncated
	}
	if err := opt.validate(); err != nil {
		return nil, nil, err
	}
	rep := &DecodeReport{Format: opt.Format, Bytes: len(data)}
	var w, h int
	var pix []uint8
	var err error
	switch opt.Format {
	case "pgm":
		w, h, pix, err = codec.DecodePGM(data)
	case "bmp":
		w, h, pix, err = codec.DecodeBMP(data)
	default:
		if len(data) >= 4 && string(data[0:4]) == "GTR2" {
			// 解码以流内记录的候选为准，PredictOrder 只参与取值范围校验，
			// 不限制可解码的流内阶数。
			w, h, pix, err = codec.DecodeRLEPredict(data, 0)
			if err == nil {
				kx, ky := codec.StreamPredict(data)
				rep.Predict = PredictCandidate{KX: kx, KY: ky}
			}
		} else {
			w, h, pix, err = codec.DecodeRLE(data)
		}
	}
	if err != nil {
		return nil, nil, err
	}
	img := &Image{W: w, H: h, Pix: pix}
	if opt.Gain != 0 {
		img = applyGain(img, -opt.Gain)
	}
	return img, rep, nil
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
