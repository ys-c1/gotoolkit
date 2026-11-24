// Package api 是 gotoolkit 唯一的对外入口。
// 调用方只能看到本包的类型与函数；具体格式实现放在 internal 下，外部不可引用。
package api

import (
	"errors"
	"fmt"
	"math"

	"gotoolkit/internal/codec"
)

// Options 是所有编解码参数的公共入口。
// 新增可配参数必须加在这里并写明默认值，否则调用方无法设置。
type Options struct {
	Format string  // pgm | bmp | rle，默认 pgm
	Gain   int     // 亮度增益百分比，0 表示不调整，默认 0
	Gamma  float64 // 伽马校正指数，变换 out=255*(x/255)^(1/Gamma)；1 为恒等，默认 1，必须为正的有限值
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
	ErrBadTarget     = errors.New("目标平均亮度不可达")
)

// DefaultOptions 返回默认参数。
func DefaultOptions() Options { return Options{Format: "pgm", Gain: 0, Gamma: 1} }

func (o Options) validate() error {
	switch o.Format {
	case "pgm", "bmp", "rle":
	default:
		return fmt.Errorf("%w: %q", ErrUnknownFormat, o.Format)
	}
	if o.Gain < -100 || o.Gain > 100 {
		return fmt.Errorf("%w: Gain=%d 超出 [-100,100]", ErrBadOption, o.Gain)
	}
	// Gamma 为零、负数、NaN 或 Inf 一律拒绝；1 为恒等，也是缺省值。
	if o.Gamma <= 0 || math.IsNaN(o.Gamma) || math.IsInf(o.Gamma, 0) {
		return fmt.Errorf("%w: Gamma=%v 必须为正的有限值", ErrBadOption, o.Gamma)
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
		src = applyGain(src, opt.Gain)
	}
	if opt.Gamma != 1 {
		src = applyGamma(src, opt.Gamma)
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
	// 解码按编码的逆序还原：先逆伽马，再逆增益。
	if opt.Gamma != 1 {
		img = applyGamma(img, 1/opt.Gamma)
	}
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

// applyGamma 对全图做伽马变换：out = 255*(x/255)^(1/gamma)，四舍五入并截断到 [0,255]。
// gamma>1 提亮暗部（曲线下凸），0<gamma<1 压暗；gamma=1 为恒等。
func applyGamma(img *Image, gamma float64) *Image {
	inv := 1 / gamma
	out := &Image{W: img.W, H: img.H, Pix: make([]uint8, len(img.Pix))}
	for i, v := range img.Pix {
		y := 255 * math.Pow(float64(v)/255, inv)
		x := int(math.Round(y))
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

// SolveGamma 由目标平均亮度 targetMean 反解伽马值，不做枚举，采用区间二分。
// 返回反解出的 gamma 与回代上界 bound：对返回图像再做一次伽马变换后，
// 平均亮度与 targetMean 的偏差不超过 bound。
//
// 可达域：端点像素 v=0、v=255 在任何伽马下都不动，故可达目标严格落在
// (minMean, maxMean) 开区间内；targetMean 落在端点或区间外返回 ErrBadTarget。
//
// 单调性（反解唯一性，两行）：
// 对 x∈(0,1)，x^(1/γ) 随 γ 严格递增（1/γ 递增且 0<x<1 时幂随指数递增）；
// 均值 F(γ) 是这些严格递增函数的平均，故 F 严格递增，二分收敛到唯一解。
func SolveGamma(pix []uint8, targetMean float64) (gamma, bound float64, err error) {
	if len(pix) == 0 {
		return 0, 0, ErrBadSize
	}
	if math.IsNaN(targetMean) || math.IsInf(targetMean, 0) || targetMean <= 0 {
		return 0, 0, fmt.Errorf("%w: targetMean=%v 非法", ErrBadTarget, targetMean)
	}
	n := float64(len(pix))
	// γ→0+ 时仅 v=255 的像素保持 255，其余趋于 0；
	// γ→+∞ 时所有 v>0 的像素趋于 255，v=0 保持 0。
	minMean, maxMean := 0.0, 0.0
	for _, v := range pix {
		if v == 255 {
			minMean += 255
		}
		if v > 0 {
			maxMean += 255
		}
	}
	minMean /= n
	maxMean /= n
	if targetMean <= minMean || targetMean >= maxMean {
		return 0, 0, fmt.Errorf("%w: targetMean=%.4f 不在可达区间 (%.4f, %.4f)",
			ErrBadTarget, targetMean, minMean, maxMean)
	}

	// 连续模型（不做整数舍入）下的均值函数，二分只依赖它的严格单调性。
	meanAt := func(g float64) float64 {
		inv := 1 / g
		s := 0.0
		for _, v := range pix {
			s += 255 * math.Pow(float64(v)/255, inv)
		}
		return s / n
	}

	// 动态扩张括号，确保 f(lo) < target < f(hi)。
	lo, hi := 1.0, 1.0
	if meanAt(1) < targetMean {
		for meanAt(hi) < targetMean {
			lo, hi = hi, hi*2
		}
	} else {
		lo = 0.5
		for meanAt(lo) > targetMean {
			hi, lo = lo, lo/2
		}
	}

	// 二分至连续模型残差 ≤ 1e-9；100 次为兜底上限。
	for i := 0; i < 100 && hi-lo > 1e-12; i++ {
		mid := (lo + hi) / 2
		if meanAt(mid) < targetMean {
			lo = mid
		} else {
			hi = mid
		}
		if math.Abs(meanAt(mid)-targetMean) <= 1e-9 {
			lo, hi = mid, mid
			break
		}
	}
	gamma = (lo + hi) / 2

	// 回代上界 = 连续模型实际残差 + 量化上界 0.5（每个像素舍入误差 ≤0.5）。
	residual := math.Abs(meanAt(gamma) - targetMean)
	bound = residual + 0.5
	return gamma, bound, nil
}
