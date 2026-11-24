// Package api 是 gotoolkit 唯一的对外入口。
// 调用方只能看到本包的类型与函数；具体格式实现放在 internal 下，外部不可引用。
package api

import (
	"errors"
	"fmt"

	"gotoolkit/internal/codec"
	"gotoolkit/internal/lowbit"
)

// Force 是三处联动的总开关的取值。
//
//	"off"  —— 三项全部关闭，走原始路径，输出与基线逐字节相同；
//	"on"   —— 三项全部开启；
//	"quant" / "dither" / "palette" —— 只开指定的一项，其余两项仍使用共享的
//	          Bits 参数推导出的口径（输出口径随开关变化，但字段不会被忽略）。
type Force string

const (
	ForceOff     Force = "off"
	ForceOn      Force = "on"
	ForceQuant   Force = "quant"
	ForceDither  Force = "dither"
	ForcePalette Force = "palette"
)

// Options 是所有编解码参数的公共入口。
// 新增可配参数必须加在这里并写明默认值，否则调用方无法设置。
type Options struct {
	Format string // pgm | bmp | rle，默认 pgm
	Gain   int    // 亮度增益百分比，0 表示不调整，默认 0

	// 低位深三处联动的共享参数。三处都从 Bits 推导，故同一字段任一项开启，
	// 其余两项的输出口径同步改变。
	Bits        int    // 目标位深 d，1..8；0 表示关闭（等价于 ForceOff）
	Force       Force  // off | on | quant | dither | palette，默认 on
	TargetErr   int    // 目标最大逐像素误差 E，0 表示按位深推导默认值
	Kernel      string // 抖动核：floyd-steinberg | bayer | custom:...；空表示按 Force 推导
	PaletteSize int    // 调色板条目数，0 表示 2^Bits
}

// Image 是解码后的灰度图。
type Image struct {
	W, H int
	Pix  []uint8
}

// 稳定错误码。全部串在错误链里，可用 errors.Is 判断。
var (
	ErrUnknownFormat = errors.New("未知格式")
	ErrBadSize       = errors.New("画幅必须为正")
	ErrBadOption     = errors.New("参数越界")
	ErrTruncated     = errors.New("数据被截断")

	ErrCodeBitDepth   = errors.New("低位深: E_LOWBIT_BITS")
	ErrCodeKernelZero = errors.New("低位深: E_LOWBIT_KERNEL_SUM")
	ErrCodeKernel     = errors.New("低位深: E_LOWBIT_KERNEL")
	ErrCodePalette    = errors.New("低位深: E_LOWBIT_PALETTE")
)

// DefaultOptions 返回默认参数。
func DefaultOptions() Options { return Options{Format: "pgm", Gain: 0, Bits: 0, Force: ""} }

// force 归一化总开关：未设置且给定位深时视为 on。
func (o Options) force() Force {
	if o.Bits == 0 {
		return ForceOff
	}
	if o.Force == "" {
		return ForceOn
	}
	return o.Force
}

// lowbitOn 判断本次是否走低位深路径。
func (o Options) lowbitOn() bool { return o.force() != ForceOff }

func (o Options) validate() error {
	switch o.Format {
	case "pgm", "bmp", "rle":
	default:
		return fmt.Errorf("%w: %q", ErrUnknownFormat, o.Format)
	}
	if o.Gain < -100 || o.Gain > 100 {
		return fmt.Errorf("%w: Gain=%d 超出 [-100,100]", ErrBadOption, o.Gain)
	}
	if o.Force != "" {
		switch o.Force {
		case ForceOff, ForceOn, ForceQuant, ForceDither, ForcePalette:
		default:
			return fmt.Errorf("%w: Force=%q 取值非法", ErrBadOption, o.Force)
		}
	}
	// 位深越界是参数错误，与 Force 无关：给了非法值就报错，不静默忽略。
	if o.Bits < 0 || o.Bits > lowbit.MaxBits {
		return fmt.Errorf("%w: Bits=%d 不在 [1,%d]", ErrCodeBitDepth, o.Bits, lowbit.MaxBits)
	}
	// 显式开启却给 0 位深属于“字段被忽略”，必须报错而不是静默走原路径。
	if o.Bits == 0 && (o.Force == ForceOn || o.Force == ForceQuant ||
		o.Force == ForceDither || o.Force == ForcePalette) {
		return fmt.Errorf("%w: Force=%q 但未给 Bits", ErrCodeBitDepth, o.Force)
	}
	if o.TargetErr < 0 || o.TargetErr > 255 {
		return fmt.Errorf("%w: TargetErr=%d 不在 [0,255]", ErrBadOption, o.TargetErr)
	}
	// PaletteSize 的校验在 lowbit.NewParams（依赖位深范围），此处只挡明显越界。
	if o.PaletteSize < 0 || o.PaletteSize > 256 {
		return fmt.Errorf("%w: PaletteSize=%d 不在 [1,256]", ErrCodePalette, o.PaletteSize)
	}
	return nil
}

// lowbitParams 派生低位深参数，并校验核。
func (o Options) lowbitParams() (lowbit.Params, *lowbit.Kernel, bool, error) {
	p, err := lowbit.NewParams(o.Bits, o.TargetErr, o.PaletteSize)
	if err != nil {
		switch {
		case errors.Is(err, lowbit.ErrBadBits):
			return p, nil, false, fmt.Errorf("%w: %v", ErrCodeBitDepth, err)
		case errors.Is(err, lowbit.ErrBadPalette):
			return p, nil, false, fmt.Errorf("%w: %v", ErrCodePalette, err)
		default:
			return p, nil, false, fmt.Errorf("%w: %v", ErrBadOption, err)
		}
	}
	ditherOn := o.force() == ForceOn || o.force() == ForceDither
	kname := o.Kernel
	if kname == "" && ditherOn {
		kname = "floyd-steinberg"
	}
	k, err := lowbit.ResolveKernel(kname)
	if err != nil {
		switch {
		case errors.Is(err, lowbit.ErrZeroSum):
			return p, nil, false, fmt.Errorf("%w: %v", ErrCodeKernelZero, err)
		default:
			return p, nil, false, fmt.Errorf("%w: %v", ErrCodeKernel, err)
		}
	}
	if k != nil {
		if err := k.Validate(); err != nil {
			if errors.Is(err, lowbit.ErrZeroSum) {
				return p, nil, false, fmt.Errorf("%w: %v", ErrCodeKernelZero, err)
			}
			return p, nil, false, fmt.Errorf("%w: %v", ErrCodeKernel, err)
		}
	}
	return p, k, ditherOn, nil
}

// LowbitOn 供命令与测试查询本次是否走低位深路径。
func (o Options) LowbitOn() bool { return o.lowbitOn() }

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
	var p lowbit.Params
	var k *lowbit.Kernel
	ditherOn := false
	if opt.lowbitOn() {
		var err error
		p, k, ditherOn, err = opt.lowbitParams()
		if err != nil {
			return nil, err
		}
	}
	src := img
	if opt.Gain != 0 {
		src = applyGain(img, opt.Gain)
	}
	if opt.lowbitOn() {
		st := lowbit.Pipeline(&lowbit.Image{W: src.W, H: src.H, Pix: src.Pix}, p, k, ditherOn)
		src = &Image{W: st.W, H: st.H, Pix: st.Quant}
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
//
// 注意：低位深不可逆（量化损失信息）。解码端的低位深开启时，会把解出的像素
// 再走同一条流水线，使 encode→decode 的往返在量化口径下自洽。
func Decode(data []byte, opt Options) (*Image, error) {
	if len(data) == 0 {
		return nil, ErrTruncated
	}
	if err := opt.validate(); err != nil {
		return nil, err
	}
	var p lowbit.Params
	var k *lowbit.Kernel
	ditherOn := false
	if opt.lowbitOn() {
		var err error
		p, k, ditherOn, err = opt.lowbitParams()
		if err != nil {
			return nil, err
		}
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
	if opt.lowbitOn() {
		st := lowbit.Pipeline(&lowbit.Image{W: w, H: h, Pix: pix}, p, k, ditherOn)
		img = &Image{W: st.W, H: st.H, Pix: st.Quant}
	}
	if opt.Gain != 0 {
		img = applyGain(img, -opt.Gain)
	}
	return img, nil
}

// LowbitState 暴露全部低位深中间结果（码值、缩略、前后调色板、两侧索引表），
// 供命令行 --dump 与验收脚本逐条列举。全关时返回 nil。
func LowbitState(img *Image, opt Options) (*lowbit.State, error) {
	if img == nil || img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return nil, ErrBadSize
	}
	if err := opt.validate(); err != nil {
		return nil, err
	}
	if !opt.lowbitOn() {
		return nil, nil
	}
	p, k, ditherOn, err := opt.lowbitParams()
	if err != nil {
		return nil, err
	}
	return lowbit.Pipeline(&lowbit.Image{W: img.W, H: img.H, Pix: img.Pix}, p, k, ditherOn), nil
}

// Params 暴露派生参数，供报告打印步长不等式。
func Params(opt Options) (lowbit.Params, error) {
	if opt.Bits == 0 {
		return lowbit.Params{}, nil
	}
	if err := opt.validate(); err != nil {
		return lowbit.Params{}, err
	}
	p, err := lowbit.NewParams(opt.Bits, opt.TargetErr, opt.PaletteSize)
	if err != nil {
		return lowbit.Params{}, err
	}
	return p, nil
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
