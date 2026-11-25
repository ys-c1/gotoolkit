// api/graymap.go —— 灰度映射（LUT）的对外入口。
// 具体实现放在 internal/graymap，外部只能通过本包访问。
package api

import "gotoolkit/internal/graymap"

// GrayLUT 是一份灰度查找表的对外视图。
// Raw 为逐点取整的原始映射（可能倒挂），Mono 为累积最大值强制单调后的映射，
// Fixed 为平台段铺开后的最终映射；三者长度均为 1<<Bits。
type GrayLUT struct {
	Bits  int
	Raw   []int
	Mono  []int
	Fixed []int
}

// GrayInversion 是一处倒挂的对外坐标：相邻码值 I0→I1，映射值由 Y0 跌到 Y1。
type GrayInversion struct {
	I0, I1 int
	Y0, Y1 int
}

// GrayPlateau 是一个平台段的对外记录，Spread 表示是否已严格铺开。
type GrayPlateau struct {
	Start, End int
	Value      int
	Spread     bool
}

// GrayReport 是灰度映射构建与复核指标的对外视图。
type GrayReport struct {
	Bits                 int
	InversionCount       int
	Inversions           []GrayInversion
	RawTieCount          int
	ResidualTieRuns      int
	ResidualTieSteps     int
	Plateaus             []GrayPlateau
	MaxDeviationRaw      int
	MaxDeviationMono     int
	MaxDeviationIdeal    float64
	RawEnd0, RawEndN     int
	FixedEnd0, FixedEndN int
}

// BuildGrayMap 构建 8/10/12 位深的灰度映射表并返回核对报告。
func BuildGrayMap(bits int) (*GrayLUT, *GrayReport, error) {
	lut, rep, err := graymap.Build(bits)
	if err != nil {
		return nil, nil, err
	}
	out := &GrayLUT{Bits: lut.Bits, Raw: lut.Raw, Mono: lut.Mono, Fixed: lut.Fixed}
	r := &GrayReport{
		Bits:              rep.Bits,
		InversionCount:    rep.InversionCount,
		RawTieCount:       rep.RawTieCount,
		ResidualTieRuns:   rep.ResidualTieRuns,
		ResidualTieSteps:  rep.ResidualTieSteps,
		MaxDeviationRaw:   rep.MaxDeviationRaw,
		MaxDeviationMono:  rep.MaxDeviationMono,
		MaxDeviationIdeal: rep.MaxDeviationIdeal,
		RawEnd0:           rep.RawEnd0,
		RawEndN:           rep.RawEndN,
		FixedEnd0:         rep.FixedEnd0,
		FixedEndN:         rep.FixedEndN,
	}
	for _, iv := range rep.Inversions {
		r.Inversions = append(r.Inversions, GrayInversion{I0: iv.I0, I1: iv.I1, Y0: iv.Y0, Y1: iv.Y1})
	}
	for _, p := range rep.Plateaus {
		r.Plateaus = append(r.Plateaus, GrayPlateau{Start: p.Start, End: p.End, Value: p.Value, Spread: p.Spread})
	}
	return out, r, nil
}

// ApplyGrayMap 把最终映射 Fixed 应用到一张 8 位灰度图。
// LUT 位深为 8 时按码值直接查表；更高位深的 LUT 先按比例折算到 8 位索引。
func ApplyGrayMap(img *Image, lut *GrayLUT) *Image {
	if img == nil || lut == nil {
		return img
	}
	out := &Image{W: img.W, H: img.H, Pix: make([]uint8, len(img.Pix))}
	scale := float64(len(lut.Fixed)-1) / 255.0
	for i, v := range img.Pix {
		idx := int(float64(v)*scale + 0.5)
		y := lut.Fixed[idx]
		// 输出统一折算回 8 位码值
		y = int(float64(y)/scale + 0.5)
		if y < 0 {
			y = 0
		}
		if y > 255 {
			y = 255
		}
		out.Pix[i] = uint8(y)
	}
	return out
}
