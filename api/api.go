// Package api 是 gotoolkit 唯一的对外入口。
// 调用方只能看到本包的类型与函数；具体格式实现放在 internal 下，外部不可引用。
package api

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"gotoolkit/internal/codec"
)

// Options 是所有编解码参数的公共入口。
// 新增可配参数必须加在这里并写明默认值，否则调用方无法设置。
type Options struct {
	Format string // pgm | bmp | rle，默认 pgm
	Gain   int    // 亮度增益百分比，0 表示不调整，默认 0（链外的既有步骤）

	// 以下三项构成固定顺序的处理链，顺序不可配置，唯一合法顺序见 CanonicalOrder：
	//
	//	输入 → [1] 透明掩码 MaskValues → [2] 伽马 Gamma → [3] 抖动 Dither → 输出
	//
	// 顺序对本实现是硬语义：掩码判定只看进入掩码这一步的像素，
	// 伽马作用在掩码之后的像素上，抖动最后才加在伽马结果上。
	// 调换任意两步都会得到不同的字节流，对照见 docs/chain.md。

	// MaskValues 是透明掩码命中的灰度值集合（取值 0..255，可多个）。
	// 编码时等于其中任一值的像素在尾段里标为透明，像素字节本身不改；
	// 解码后 Alpha 逐像素还原。空表示关闭，默认关闭。
	// 重复取值返回 ErrMaskDuplicate。
	MaskValues []int

	// Gamma 是伽马指数，变换 out = round(255*(x/255)^(1/Gamma))。
	// 0 表示关闭（恒等），默认关闭；开启时必须为正的有限值。
	// NaN 或 ±Inf 返回 ErrGammaNonFinite，负数返回 ErrBadOption。
	Gamma float64

	// Dither 是 4×4 有序抖动的幅度（0..255），0 表示关闭，默认关闭。
	// out = clamp(round(x + (bayer(x,y)/16 - 0.5) * Dither))，确定性、无随机数。
	Dither int
}

// CanonicalOrder 是处理链唯一合法顺序，写进注释、帮助与文档，三处必须一致。
var CanonicalOrder = []string{"mask", "gamma", "dither"}

// Image 是解码后的灰度图。
type Image struct {
	W, H int
	Pix  []uint8
	// Alpha 是每像素透明度，长度与 Pix 相同：0 透明（命中掩码值），255 不透明。
	// 未启用掩码时为 nil。
	Alpha []uint8
}

var (
	ErrUnknownFormat = errors.New("未知格式")
	ErrBadSize       = errors.New("画幅必须为正")
	ErrBadOption     = errors.New("参数越界")
	ErrTruncated     = errors.New("数据被截断")
	// ErrMaskDuplicate 表示掩码值集合里有重复项。
	ErrMaskDuplicate = errors.New("掩码值重复")
	// ErrGammaNonFinite 表示伽马为 NaN 或 ±Inf。
	ErrGammaNonFinite = errors.New("伽马非有限")
	// ErrMaskMismatch 表示流中记录的掩码值与请求值不一致。
	ErrMaskMismatch = errors.New("掩码值与流中记录的不一致")
	// ErrBadOrder 表示处理链顺序非法（缺项、多项、未知名）。
	ErrBadOrder = errors.New("处理链顺序非法")
)

// ErrorCode 是本包对外约定的稳定错误码。
type ErrorCode int

const (
	CodeNone           ErrorCode = 0
	CodeUnknownFormat  ErrorCode = 1
	CodeBadSize        ErrorCode = 2
	CodeBadOption      ErrorCode = 3
	CodeTruncated      ErrorCode = 4
	CodeMaskDuplicate  ErrorCode = 5
	CodeGammaNonFinite ErrorCode = 6
	CodeMaskMismatch   ErrorCode = 7
	CodeBadOrder       ErrorCode = 8
)

// CodeOf 把错误映射为稳定错误码；未知错误返回 CodeNone。
func CodeOf(err error) ErrorCode {
	switch {
	case err == nil:
		return CodeNone
	case errors.Is(err, ErrUnknownFormat):
		return CodeUnknownFormat
	case errors.Is(err, ErrBadSize):
		return CodeBadSize
	case errors.Is(err, ErrBadOption):
		return CodeBadOption
	case errors.Is(err, ErrTruncated):
		return CodeTruncated
	case errors.Is(err, ErrMaskDuplicate):
		return CodeMaskDuplicate
	case errors.Is(err, ErrGammaNonFinite):
		return CodeGammaNonFinite
	case errors.Is(err, ErrMaskMismatch):
		return CodeMaskMismatch
	case errors.Is(err, ErrBadOrder):
		return CodeBadOrder
	}
	return CodeNone
}

// DefaultOptions 返回默认参数。
// 三项新功能默认全关：此时编码字节与引入处理链之前逐字节一致。
func DefaultOptions() Options {
	return Options{Format: "pgm", Gain: 0, MaskValues: nil, Gamma: 0, Dither: 0}
}

func (o Options) validate() error {
	switch o.Format {
	case "pgm", "bmp", "rle":
	default:
		return fmt.Errorf("%w: %q", ErrUnknownFormat, o.Format)
	}
	if o.Gain < -100 || o.Gain > 100 {
		return fmt.Errorf("%w: Gain=%d 超出 [-100,100]", ErrBadOption, o.Gain)
	}
	if _, err := normalizeMask(o.MaskValues); err != nil {
		return err
	}
	if math.IsNaN(o.Gamma) || math.IsInf(o.Gamma, 0) {
		return fmt.Errorf("%w: Gamma=%v", ErrGammaNonFinite, o.Gamma)
	}
	if o.Gamma < 0 {
		return fmt.Errorf("%w: Gamma=%v 必须为 0（关闭）或正数", ErrBadOption, o.Gamma)
	}
	if o.Dither < 0 || o.Dither > 255 {
		return fmt.Errorf("%w: Dither=%d 超出 [0,255]", ErrBadOption, o.Dither)
	}
	return nil
}

// normalizeMask 校验并归一化为升序、互不相同的 uint8 集合。重复即报 ErrMaskDuplicate。
func normalizeMask(vals []int) ([]uint8, error) {
	if len(vals) == 0 {
		return nil, nil
	}
	seen := make(map[int]bool, len(vals))
	for _, v := range vals {
		if v < 0 || v > 255 {
			return nil, fmt.Errorf("%w: MaskValues 含 %d，超出 [0,255]", ErrBadOption, v)
		}
		if seen[v] {
			return nil, fmt.Errorf("%w: MaskValues 含重复值 %d", ErrMaskDuplicate, v)
		}
		seen[v] = true
	}
	out := make([]uint8, 0, len(vals))
	for v := range seen {
		out = append(out, uint8(v))
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// chainStep 是处理链里的单步。
type chainStep int

const (
	stepMask chainStep = iota
	stepGamma
	stepDither
)

// parseOrder 把名字序列解析为步骤序列，要求三步各出现且仅出现一次。
func parseOrder(names []string) ([]chainStep, error) {
	if len(names) != 3 {
		return nil, fmt.Errorf("%w: 需要恰好三步，实际 %d 步", ErrBadOrder, len(names))
	}
	seen := map[string]bool{}
	out := make([]chainStep, 0, 3)
	for _, n := range names {
		if seen[n] {
			return nil, fmt.Errorf("%w: 步骤 %q 重复", ErrBadOrder, n)
		}
		seen[n] = true
		switch n {
		case "mask":
			out = append(out, stepMask)
		case "gamma":
			out = append(out, stepGamma)
		case "dither":
			out = append(out, stepDither)
		default:
			return nil, fmt.Errorf("%w: 未知步骤 %q", ErrBadOrder, n)
		}
	}
	return out, nil
}

// canonicalSteps 是 CanonicalOrder 的解析结果，包初始化时确定一次。
var canonicalSteps = mustOrder(CanonicalOrder)

func mustOrder(names []string) []chainStep {
	s, err := parseOrder(names)
	if err != nil {
		panic(err)
	}
	return s
}

// runChain 按给定顺序跑处理链，返回最终像素与（若经过掩码步）alpha。
// 关闭的步骤是恒等变换，所以三步全关时像素与入参逐字节相同。
func runChain(pix []uint8, w, h int, maskVals []uint8, gamma float64, dither int, order []chainStep) ([]uint8, []uint8) {
	cur := append([]uint8(nil), pix...)
	var alpha []uint8
	for _, s := range order {
		switch s {
		case stepMask:
			if len(maskVals) > 0 {
				alpha = maskAlpha(cur, maskVals)
			}
		case stepGamma:
			if gamma != 0 {
				cur = gammaPix(cur, gamma)
			}
		case stepDither:
			if dither != 0 {
				cur = ditherPix(cur, w, h, dither)
			}
		}
	}
	return cur, alpha
}

// Encode 按 CanonicalOrder（掩码→伽马→抖动）编码。
func Encode(img *Image, opt Options) ([]byte, error) {
	return EncodeWithOrder(img, opt, CanonicalOrder)
}

// EncodeWithOrder 按调用方指定的顺序编码，用于证明顺序敏感。
// 生产路径请用 Encode；本函数只接受三步的一个排列，非法顺序返回 ErrBadOrder。
func EncodeWithOrder(img *Image, opt Options, order []string) ([]byte, error) {
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
	steps, err := parseOrder(order)
	if err != nil {
		return nil, err
	}
	maskVals, _ := normalizeMask(opt.MaskValues)

	// Gain 是链外的既有步骤，先于三段链执行；链本身按 order 跑。
	src := img.Pix
	if opt.Gain != 0 {
		src = gainPix(src, opt.Gain)
	}
	pix, alpha := runChain(src, img.W, img.H, maskVals, opt.Gamma, opt.Dither, steps)

	if len(maskVals) == 0 {
		// 掩码关闭：与引入处理链之前的编码路径完全一致。
		switch opt.Format {
		case "pgm":
			return codec.EncodePGM(img.W, img.H, pix)
		case "bmp":
			return codec.EncodeBMP(img.W, img.H, pix)
		default:
			return codec.EncodeRLE(img.W, img.H, pix)
		}
	}
	switch opt.Format {
	case "pgm":
		return codec.EncodePGMMasked(img.W, img.H, pix, alpha, maskVals)
	case "bmp":
		return codec.EncodeBMPMasked(img.W, img.H, pix, alpha, maskVals)
	default:
		return codec.EncodeRLEMasked(img.W, img.H, pix, alpha, maskVals)
	}
}

// Decode 把字节流解码成灰度图。
// 逆序还原：掩码透明位图从尾段还原到 Alpha，再逆伽马；抖动是单向显示变换，不反解。
func Decode(data []byte, opt Options) (*Image, error) {
	if len(data) == 0 {
		return nil, ErrTruncated
	}
	if err := opt.validate(); err != nil {
		return nil, err
	}
	maskVals, _ := normalizeMask(opt.MaskValues)

	var (
		w, h       int
		pix, alpha []uint8
		err        error
	)
	if len(maskVals) == 0 {
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
	} else {
		var stored []uint8
		switch opt.Format {
		case "pgm":
			w, h, stored, pix, alpha, err = codec.DecodePGMMasked(data)
		case "bmp":
			w, h, stored, pix, alpha, err = codec.DecodeBMPMasked(data)
		default:
			w, h, stored, pix, alpha, err = codec.DecodeRLEMasked(data)
		}
		if err != nil {
			return nil, err
		}
		if !sameVals(stored, maskVals) {
			return nil, fmt.Errorf("%w: 请求 %v，流中记录 %v", ErrMaskMismatch, maskVals, stored)
		}
	}
	if opt.Gamma != 0 {
		pix = gammaPix(pix, 1/opt.Gamma)
	}
	// Gain 是链外的既有步骤，编码时先于链执行，所以解码时最后逆回去。
	if opt.Gain != 0 {
		pix = gainPix(pix, -opt.Gain)
	}
	return &Image{W: w, H: h, Pix: pix, Alpha: alpha}, nil
}

func sameVals(a, b []uint8) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// maskAlpha 依据掩码值集合生成每像素 alpha：命中任一值为透明（0），否则不透明（255）。
func maskAlpha(pix []uint8, vals []uint8) []uint8 {
	set := make(map[uint8]bool, len(vals))
	for _, v := range vals {
		set[v] = true
	}
	alpha := make([]uint8, len(pix))
	for i, v := range pix {
		if set[v] {
			alpha[i] = 0
		} else {
			alpha[i] = 255
		}
	}
	return alpha
}

func gainPix(pix []uint8, gain int) []uint8 {
	out := make([]uint8, len(pix))
	for i, v := range pix {
		x := int(v) + int(v)*gain/100
		if x < 0 {
			x = 0
		}
		if x > 255 {
			x = 255
		}
		out[i] = uint8(x)
	}
	return out
}

// gammaPix 逐像素做 out = round(255*(x/255)^(1/gamma))，结果钳到 [0,255]。
func gammaPix(pix []uint8, gamma float64) []uint8 {
	out := make([]uint8, len(pix))
	for i, v := range pix {
		var x float64
		if v > 0 {
			x = 255 * math.Pow(float64(v)/255, 1/gamma)
		}
		x = math.Round(x)
		if x < 0 {
			x = 0
		}
		if x > 255 {
			x = 255
		}
		out[i] = uint8(x)
	}
	return out
}

// bayer4 是 4×4 有序抖动矩阵，取值 0..15。
var bayer4 = [4][4]int{
	{0, 8, 2, 10},
	{12, 4, 14, 6},
	{3, 11, 1, 9},
	{15, 7, 13, 5},
}

// ditherPix 做 4×4 有序抖动：out = clamp(round(x + (m/16 - 0.5)*strength))。
// strength 为 0 时严格恒等；同一输入两次运行结果完全一致（无随机数、无状态）。
func ditherPix(pix []uint8, w, h, strength int) []uint8 {
	out := make([]uint8, len(pix))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			t := float64(bayer4[y&3][x&3])/16 - 0.5
			v := float64(pix[i]) + t*float64(strength)
			v = math.Round(v)
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			out[i] = uint8(v)
		}
	}
	return out
}
