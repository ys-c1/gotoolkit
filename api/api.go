// Package api 是 gotoolkit 唯一的对外入口。
// 调用方只能看到本包的类型与函数；具体格式实现放在 internal 下，外部不可引用。
package api

import (
	"errors"
	"fmt"
	"math"

	"gotoolkit/internal/codec"
)

// 亮度链路：三件事的先后顺序是固定的，不可配置。
//
//	输入 → [1] 线性增益 Gain → [2] 伽马 Gamma → [3] 透明掩码 Mask → 输出
//
// 逐像素：
//
//	gain[i]  = clamp(pix[i] + pix[i]*Gain/100, 0, 255)    // 整数截断；任何像素都不允许溢出
//	mean     = average(gain)
//	exp      = ln(Gamma/255) / ln(mean/255)               // 伽马按目标平均亮度反解
//	gamma[i] = round(255 * (gain[i]/255)^exp)
//	out[i]   = round(gamma[i] * Mask[i] / 255)            // 0 = 全透明→0，255 = 原值
//
// 顺序的可见位置（三处一致）：
//   - 下面的 Options 字段注释，逐项标注 [1/3] [2/3] [3/3]；
//   - cmd/gtk 的 --help：每个开关都带“顺序 N/3”，掩码与增益同时开启时据此说明；
//   - README 与 artifacts/order/ 下的两份顺序对照结果。
//
// 编解码两侧共用本文件的 applyBrightness：编码侧在写入格式字节前执行，
// 解码侧在解析出像素后、返回前执行，因此同一组参数在两侧效果一致。
type Options struct {
	Format string  // pgm | bmp | rle，默认 pgm
	Gain   int     // [1/3] 线性增益百分比，取值 [-100,100]，0 表示不调整，默认 0
	Gamma  float64 // [2/3] 伽马目标平均亮度，取值 (0,255]，0 表示不调整，默认 0；系数按该目标反解
	Mask   []uint8 // [3/3] 透明掩码，nil 表示不调整；长度须为 W*H，0 全透明，255 原值
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
	// ErrGammaZero：Gamma 已开启但反解出的伽马系数为零（增益后均值已是 0 或 255，
	// 或目标平均亮度为 255），此时的伽马要么无效果要么把所有像素推成纯白。
	// 错误信息会带回 Gamma 字段值与增益后均值。
	ErrGammaZero = errors.New("伽马为零")
	// ErrMaskRange：Mask 已给出但长度与画幅不符。错误信息会带回 Mask 字段长度与 W*H。
	ErrMaskRange = errors.New("掩码超范围")
)

// DefaultOptions 返回默认参数；默认值下亮度链路为空，输出与基准字节一致。
func DefaultOptions() Options { return Options{Format: "pgm", Gain: 0, Gamma: 0, Mask: nil} }

func (o Options) validate() error {
	switch o.Format {
	case "pgm", "bmp", "rle":
	default:
		return fmt.Errorf("%w: %q", ErrUnknownFormat, o.Format)
	}
	if o.Gain < -100 || o.Gain > 100 {
		return fmt.Errorf("%w: Gain=%d 超出 [-100,100]", ErrBadOption, o.Gain)
	}
	if math.IsNaN(o.Gamma) || o.Gamma < 0 || o.Gamma > 255 {
		return fmt.Errorf("%w: Gamma=%v 超出 [0,255]", ErrBadOption, o.Gamma)
	}
	return nil
}

func (o Options) brightness() bool {
	return o.Gain != 0 || o.Gamma != 0 || len(o.Mask) != 0
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
	if opt.brightness() {
		var err error
		src, err = applyBrightness(img, opt)
		if err != nil {
			return nil, err
		}
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
// 与 Encode 对称：解析出像素后按同一固定顺序套用亮度链路。
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
	if opt.brightness() {
		img, err = applyBrightness(img, opt)
		if err != nil {
			return nil, err
		}
	}
	return img, nil
}

// applyBrightness 是编解码两侧共用的唯一实现，严格按 增益 → 伽马 → 掩码 执行。
// 返回新图，不修改入参。
//
// 字段范围先于链路校验：掩码长度不符时直接报 ErrMaskRange，不进入 [1/3]。
func applyBrightness(img *Image, opt Options) (*Image, error) {
	if len(opt.Mask) != 0 && len(opt.Mask) != img.W*img.H {
		return nil, fmt.Errorf("%w: Mask 长度=%d，须为 W*H=%d（宽 %d 高 %d）", ErrMaskRange, len(opt.Mask), img.W*img.H, img.W, img.H)
	}
	out := &Image{W: img.W, H: img.H, Pix: make([]uint8, len(img.Pix))}
	copy(out.Pix, img.Pix)

	// [1/3] 线性增益。
	if opt.Gain != 0 {
		gainPixels(out.Pix, opt.Gain)
	}

	// [2/3] 伽马：系数由增益后的均值与目标均值反解。
	if opt.Gamma != 0 {
		exp, err := gammaExponent(out.Pix, opt.Gamma)
		if err != nil {
			return nil, err
		}
		if exp != 1 { // 目标恰等于当前均值时保持字节不动
			gammaPixels(out.Pix, exp)
		}
	}

	// [3/3] 透明掩码：最后叠加，0 输出 0，255 保持原值。
	if len(opt.Mask) != 0 {
		maskPixels(out.Pix, opt.Mask)
	}
	return out, nil
}

// gainPixels 施加线性增益并钳位。
// 全部用整数运算：x = v + v*gain/100（向零截断），随后夹到 [0,255]，
// 因此任何输入与任何合法 Gain 都不会溢出或回绕。
func gainPixels(pix []uint8, gain int) {
	for i, v := range pix {
		x := int(v) + int(v)*gain/100
		if x < 0 {
			x = 0
		}
		if x > 255 {
			x = 255
		}
		pix[i] = uint8(x)
	}
}

// gammaExponent 按目标平均亮度反解伽马系数：
//
//	exp = ln(target/255) / ln(mean/255)
//
// mean 为增益后的均值。无法反解（均值已在 0 或 255，或反解结果为 0）时返回 ErrGammaZero。
func gammaExponent(pix []uint8, target float64) (float64, error) {
	var sum int64
	for _, v := range pix {
		sum += int64(v)
	}
	mean := float64(sum) / float64(len(pix))
	if mean <= 0 || mean >= 255 {
		return 0, fmt.Errorf("%w: Gamma=%v，增益后均值=%.3f，无法反解系数", ErrGammaZero, target, mean)
	}
	exp := math.Log(target/255) / math.Log(mean/255)
	if math.IsNaN(exp) || math.IsInf(exp, 0) || exp <= 0 {
		return 0, fmt.Errorf("%w: Gamma=%v，增益后均值=%.3f 反解出的系数=%v", ErrGammaZero, target, mean, exp)
	}
	return exp, nil
}

func gammaPixels(pix []uint8, exp float64) {
	for i, v := range pix {
		x := math.Round(255 * math.Pow(float64(v)/255, exp))
		if x < 0 {
			x = 0
		}
		if x > 255 {
			x = 255
		}
		pix[i] = uint8(x)
	}
}

func maskPixels(pix, mask []uint8) {
	for i, v := range pix {
		pix[i] = uint8((int(v)*int(mask[i]) + 127) / 255)
	}
}
