package api

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// DitherLevelsDefault 是抖动量化的目标色阶数缺省值：8 档（等效 3 位）。
//
// 输出仍然是 8 位容器（pgm/bmp/rle 的像素都是 0..255），所以容器体积不随
// 色阶数变化；固定它只是为了让两档扩散强度落在同一条基线上可比。
// 它不是一个藏起来的开关：色阶数本身也在 api.Options.DitherLevels 里，
// 0 表示取本缺省值，[2,256] 可自定义，命令行用 --dither-levels 设置。
const DitherLevelsDefault = 8

// 抖动相关的具名错误，调用方可以用 errors.Is 判定。
var (
	// ErrDitherKernel 表示扩散核结构非法（空核、偏移不在前进方向、
	// 偏移重复、权重为负或非有限数、未归一却没有给出理由等）。
	ErrDitherKernel = errors.New("扩散核非法")
	// ErrKernelSum 表示扩散核的权重和为零，无法归一，也无法扩散。
	ErrKernelSum = errors.New("扩散核权重和为零")
	// ErrDitherStrength 表示扩散强度非法（负数、NaN 或无穷）。
	ErrDitherStrength = errors.New("扩散强度非法")
)

// DitherTap 是扩散核的一拍：把当前像素的量化误差按 Weight 加到
// (x+DX, y+DY)。只接受前向偏移（DY>0，或 DY==0 且 DX>0），
// 蛇形推进到反向行时把 DX 镜像为 -DX。
type DitherTap struct {
	DX, DY int
	Weight float64
}

// DitherKernel 是一张误差扩散权重表。
//
// Normalize 为 true 时实际使用的权重是 Weight/权重和，保证扩散出去的误差
// 总量守恒（和为 1）。Normalize 为 false 时按原始权重使用，此时 Note 必须
// 写明不归一的理由，且权重和不得超过 1（超过会放大误差、可能振荡）。
type DitherKernel struct {
	Name      string
	Taps      []DitherTap
	Normalize bool
	Note      string
}

// FloydSteinbergKernel 返回 Floyd–Steinberg 扩散核。
//
// 来源：R. W. Floyd, L. Steinberg, "An Adaptive Algorithm for Spatial
// Greyscale", Proceedings of the Society for Information Display 17(2),
// 1976, pp. 75–77。原始整数权重为 7/3/5/1（和为 16），这里保留原始整数，
// 由 Normalize 除以 16 归一到 1。
func FloydSteinbergKernel() DitherKernel {
	return DitherKernel{
		Name: "floyd-steinberg",
		Taps: []DitherTap{
			{DX: 1, DY: 0, Weight: 7},
			{DX: -1, DY: 1, Weight: 3},
			{DX: 0, DY: 1, Weight: 5},
			{DX: 1, DY: 1, Weight: 1},
		},
		Normalize: true,
	}
}

// AtkinsonKernel 返回 Atkinson 扩散核。
//
// 来源：Bill Atkinson 在 Apple 的 1 位抖动实现（一般记述见
// "Atkinson dithering"），六拍各 1/8，总和 3/4。原式刻意丢弃 1/4 的
// 量化误差，以换取更高的局部对比，因此这里 Normalize=false，并在 Note
// 说明不归一的理由；其余内置核与所有自定义核默认归一到 1。
func AtkinsonKernel() DitherKernel {
	return DitherKernel{
		Name: "atkinson",
		Taps: []DitherTap{
			{DX: 1, DY: 0, Weight: 1.0 / 8},
			{DX: 2, DY: 0, Weight: 1.0 / 8},
			{DX: -1, DY: 1, Weight: 1.0 / 8},
			{DX: 0, DY: 1, Weight: 1.0 / 8},
			{DX: 1, DY: 1, Weight: 1.0 / 8},
			{DX: 0, DY: 2, Weight: 1.0 / 8},
		},
		Normalize: false,
		Note:      "Atkinson 原式六拍和为 3/4，刻意丢弃 1/4 误差以提高对比；归一化会改变其经典行为",
	}
}

// BuiltinDitherKernels 返回内置扩散核的副本，顺序固定。
func BuiltinDitherKernels() []DitherKernel {
	return []DitherKernel{FloydSteinbergKernel(), AtkinsonKernel()}
}

// LookupDitherKernel 按名字取内置核；名字不区分大小写，下划线等同连字符。
// 空串与 "none" 返回零值核（表示未设置）。
func LookupDitherKernel(name string) (DitherKernel, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.ReplaceAll(n, "_", "-")
	switch n {
	case "", "none":
		return DitherKernel{}, nil
	case "floyd-steinberg", "fs":
		return FloydSteinbergKernel(), nil
	case "atkinson":
		return AtkinsonKernel(), nil
	}
	return DitherKernel{}, fmt.Errorf("%w: 未知核 %q（可选 floyd-steinberg | atkinson）", ErrDitherKernel, name)
}

// IsZero 报告核是否是零值（未设置）。
func (k DitherKernel) IsZero() bool {
	return k.Name == "" && len(k.Taps) == 0 && !k.Normalize && k.Note == ""
}

// RawSum 返回原始权重之和。
func (k DitherKernel) RawSum() float64 {
	sum := 0.0
	for _, t := range k.Taps {
		sum += t.Weight
	}
	return sum
}

// Validate 检查扩散核是否完整、合法。
func (k DitherKernel) Validate() error {
	if k.IsZero() {
		return fmt.Errorf("%w: 未提供扩散核", ErrDitherKernel)
	}
	if strings.TrimSpace(k.Name) == "" {
		return fmt.Errorf("%w: 核名为空", ErrDitherKernel)
	}
	if len(k.Taps) == 0 {
		return fmt.Errorf("%w: %q 的权重表为空", ErrDitherKernel, k.Name)
	}
	seen := make(map[[2]int]bool, len(k.Taps))
	sum := 0.0
	for i, t := range k.Taps {
		if math.IsNaN(t.Weight) || math.IsInf(t.Weight, 0) || t.Weight < 0 {
			return fmt.Errorf("%w: %q 第 %d 项权重必须是非负有限数，实际 %v",
				ErrDitherKernel, k.Name, i, t.Weight)
		}
		if t.DX == 0 && t.DY == 0 {
			return fmt.Errorf("%w: %q 第 %d 项偏移为原点", ErrDitherKernel, k.Name, i)
		}
		if t.DY < 0 || (t.DY == 0 && t.DX <= 0) {
			return fmt.Errorf("%w: %q 第 %d 项偏移 (%d,%d) 不在扫描前进方向",
				ErrDitherKernel, k.Name, i, t.DX, t.DY)
		}
		key := [2]int{t.DX, t.DY}
		if seen[key] {
			return fmt.Errorf("%w: %q 偏移 (%d,%d) 重复", ErrDitherKernel, k.Name, t.DX, t.DY)
		}
		seen[key] = true
		sum += t.Weight
	}
	if math.IsNaN(sum) || math.IsInf(sum, 0) {
		return fmt.Errorf("%w: %q 权重和不是有限数", ErrDitherKernel, k.Name)
	}
	if sum <= 0 {
		return fmt.Errorf("%w: %q 的 %d 项权重全为零", ErrKernelSum, k.Name, len(k.Taps))
	}
	if !k.Normalize {
		if strings.TrimSpace(k.Note) == "" {
			return fmt.Errorf("%w: %q 未归一但未在 Note 说明理由", ErrDitherKernel, k.Name)
		}
		if sum > 1+1e-9 {
			return fmt.Errorf("%w: %q 未归一且权重和 %v > 1，会放大误差",
				ErrDitherKernel, k.Name, sum)
		}
	}
	return nil
}

// EffectiveTaps 返回实际参与扩散的权重：Normalize 为 true 时已除以权重和，
// 否则原样返回。调用前会先 Validate。
func (k DitherKernel) EffectiveTaps() ([]DitherTap, error) {
	if err := k.Validate(); err != nil {
		return nil, err
	}
	out := make([]DitherTap, len(k.Taps))
	copy(out, k.Taps)
	if k.Normalize {
		sum := k.RawSum()
		for i := range out {
			out[i].Weight /= sum
		}
	}
	return out, nil
}

// EffectiveSum 返回实际参与扩散的权重和：归一核恒为 1（含浮点舍入），
// 未归一核等于原始权重和。
func (k DitherKernel) EffectiveSum() (float64, error) {
	taps, err := k.EffectiveTaps()
	if err != nil {
		return 0, err
	}
	sum := 0.0
	for _, t := range taps {
		sum += t.Weight
	}
	return sum, nil
}

// WeightTable 把核的原始权重、实际使用权重与权重和打印成文本，便于逐项核对。
func (k DitherKernel) WeightTable() string {
	var b strings.Builder
	sum := k.RawSum()
	eff, err := k.EffectiveTaps()
	fmt.Fprintf(&b, "kernel=%s normalize=%v taps=%d rawSum=%g\n", k.Name, k.Normalize, len(k.Taps), sum)
	if k.Note != "" {
		fmt.Fprintf(&b, "note=%s\n", k.Note)
	}
	fmt.Fprintf(&b, "%3s %4s %4s %14s %14s\n", "idx", "dx", "dy", "raw", "effective")
	for i := range k.Taps {
		effW := k.Taps[i].Weight
		if err == nil {
			effW = eff[i].Weight
		}
		fmt.Fprintf(&b, "%3d %4d %4d %14.6f %14.6f\n", i, k.Taps[i].DX, k.Taps[i].DY, k.Taps[i].Weight, effW)
	}
	if err != nil {
		fmt.Fprintf(&b, "invalid: %v\n", err)
		return b.String()
	}
	effSum := 0.0
	for _, t := range eff {
		effSum += t.Weight
	}
	fmt.Fprintf(&b, "effectiveSum=%.6f\n", effSum)
	return b.String()
}

// DitherDirection 返回第 y 行（从 0 起）的水平扫描方向：偶数行 +1（左→右），
// 奇数行 -1（右→左）。
//
// 方向切换点可以复算：方向在每一行的行末切换，切换点是行边界 y=1..H-1；
// 第 y 行从 DitherRowStart(y, W) 开始、到 DitherRowEnd(y, W) 结束，
// 下一行总是从上一行的结束列开始向反方向推进。
func DitherDirection(y int) int {
	if y%2 == 0 {
		return 1
	}
	return -1
}

// DitherRowStart 返回第 y 行第一个被访问的列：偶数行为 0，奇数行为 w-1。
func DitherRowStart(y, w int) int {
	if y%2 == 0 {
		return 0
	}
	return w - 1
}

// DitherRowEnd 返回第 y 行最后一个被访问的列：偶数行为 w-1，奇数行为 0。
func DitherRowEnd(y, w int) int {
	if y%2 == 0 {
		return w - 1
	}
	return 0
}

// ditherPalette 返回 levels 个等间距色阶（四舍五入到 8 位）；levels 须 >= 2。
func ditherPalette(levels int) []uint8 {
	pal := make([]uint8, levels)
	step := 255.0 / float64(levels-1)
	for i := range pal {
		pal[i] = uint8(math.Round(float64(i) * step))
	}
	return pal
}

// QuantizeOnly 只做逐像素最近色阶量化、不做误差扩散，返回新图。
//
// 这是间距方差的基线口径：它代表“没有抖动时的低色阶输出”。
// levels 必须在 [2,256] 内；传入与抖动相同的色阶数即可直接比间距方差。
func QuantizeOnly(img *Image, levels int) (*Image, error) {
	if img == nil || img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return nil, ErrBadSize
	}
	if levels < 2 || levels > 256 {
		return nil, fmt.Errorf("%w: levels=%d 必须在 [2,256]", ErrBadOption, levels)
	}
	pal := ditherPalette(levels)
	out := &Image{W: img.W, H: img.H, Pix: make([]uint8, len(img.Pix))}
	for i, v := range img.Pix {
		out.Pix[i] = pal[nearestLevelIndex(pal, float64(v))]
	}
	return out, nil
}

// nearestLevelIndex 返回与 v 最接近的色阶下标；等距时取较低的一档。
func nearestLevelIndex(pal []uint8, v float64) int {
	best, bestDist := 0, math.Abs(v-float64(pal[0]))
	for i := 1; i < len(pal); i++ {
		d := math.Abs(v - float64(pal[i]))
		if d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

// Dither 用蛇形推进的误差扩散把 img 量化到 DitherLevelsDefault 个色阶，
// 返回新图，不改动入参。需要自定义色阶数时用 DitherWithLevels。
//
//   - strength 是扩散强度，0 表示只做最近色阶量化、不扩散误差（基线）；
//     负数、NaN、Inf 返回 ErrDitherStrength。
//   - 核只在 strength > 0 时必需；提供的核必须先通过 Validate，
//     权重和为零返回 ErrKernelSum。
//   - 扫描逐行蛇形：偶数行左→右，奇数行右→左，反向行把 DX 镜像为 -DX；
//     方向函数与起止列见 DitherDirection / DitherRowStart / DitherRowEnd。
//   - 每个像素先把累加值与 [0,255] 夹取，再取最近色阶；误差按有效权重
//     （归一核已除以权重和）乘 strength 后加到邻居。
func Dither(img *Image, strength float64, k DitherKernel) (*Image, error) {
	return DitherWithLevels(img, strength, k, DitherLevelsDefault)
}

// DitherWithLevels 与 Dither 相同，但目标色阶数由调用方给出（2..256）。
func DitherWithLevels(img *Image, strength float64, k DitherKernel, levels int) (*Image, error) {
	if levels < 2 || levels > 256 {
		return nil, fmt.Errorf("%w: DitherLevels=%d 必须在 [2,256]", ErrBadOption, levels)
	}
	if img == nil || img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return nil, ErrBadSize
	}
	if math.IsNaN(strength) || math.IsInf(strength, 0) || strength < 0 {
		return nil, fmt.Errorf("%w: %v（必须是非负有限数）", ErrDitherStrength, strength)
	}
	var taps []DitherTap
	if !k.IsZero() {
		var err error
		taps, err = k.EffectiveTaps()
		if err != nil {
			return nil, err
		}
	} else if strength > 0 {
		return nil, fmt.Errorf("%w: 强度 %v > 0 但未提供扩散核", ErrDitherKernel, strength)
	}

	w, h := img.W, img.H
	pal := ditherPalette(levels)
	buf := make([]float64, w*h)
	for i, v := range img.Pix {
		buf[i] = float64(v)
	}
	out := &Image{W: w, H: h, Pix: make([]uint8, w*h)}

	for y := 0; y < h; y++ {
		dir := DitherDirection(y)
		x := DitherRowStart(y, w)
		for n := 0; n < w; n++ {
			i := y*w + x
			v := buf[i]
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			q := nearestLevelIndex(pal, v)
			out.Pix[i] = pal[q]
			if strength > 0 && len(taps) > 0 {
				e := (v - float64(pal[q])) * strength
				for _, t := range taps {
					nx := x + dir*t.DX
					ny := y + t.DY
					if nx < 0 || nx >= w || ny < 0 || ny >= h {
						continue
					}
					buf[ny*w+nx] += e * t.Weight
				}
			}
			x += dir
		}
	}
	return out, nil
}

// SpacingStats 是一幅图上“相邻色阶间距”的统计量。
type SpacingStats struct {
	Rows     int     // 至少有一个内部间距的行数
	Samples  int     // 内部间距样本总数
	Mean     float64 // 内部间距均值（像素）
	Variance float64 // 内部间距的总体方差（除以样本数）
	Min      int     // 最短内部间距
	Max      int     // 最长内部间距
}

// MeasureLevelSpacing 统计 img 中相邻色阶间距的方差。
//
// 口径（逐行）：把每行切成同值游程，去掉首尾两条——它们被画幅左右边缘
// 截断，长度不代表真实间距——其余每条游程的长度就是相邻两次色阶变化之间
// 的间距。所有行的内部间距汇在一起取总体方差。
//
// 这个口径直接对应“台阶是否均匀”：未抖动时同一色阶的带宽由量化步长与
// 渐变斜率决定，宽窄不一；抖动把每次色阶变化打散成接近均匀的短游程，
// 间距方差随之下降。全行同值或只有两条游程的行没有内部间距，跳过；
// 若全图都没有内部间距（例如垂直渐变或纯色），返回零值统计。
func MeasureLevelSpacing(img *Image) (SpacingStats, error) {
	if img == nil || img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return SpacingStats{}, ErrBadSize
	}
	var st SpacingStats
	sum, sumSq := 0.0, 0.0
	for y := 0; y < img.H; y++ {
		row := img.Pix[y*img.W : (y+1)*img.W]
		runs := make([]int, 0, img.W)
		run := 1
		for x := 1; x < len(row); x++ {
			if row[x] == row[x-1] {
				run++
				continue
			}
			runs = append(runs, run)
			run = 1
		}
		runs = append(runs, run)
		if len(runs) < 3 {
			continue
		}
		st.Rows++
		for _, r := range runs[1 : len(runs)-1] {
			if st.Samples == 0 || r < st.Min {
				st.Min = r
			}
			if r > st.Max {
				st.Max = r
			}
			st.Samples++
			sum += float64(r)
			sumSq += float64(r * r)
		}
	}
	if st.Samples == 0 {
		return st, nil
	}
	st.Mean = sum / float64(st.Samples)
	st.Variance = sumSq/float64(st.Samples) - st.Mean*st.Mean
	if st.Variance < 0 { // 浮点抵消保护
		st.Variance = 0
	}
	return st, nil
}

// SpacingVarianceDrop 返回 (基线方差 - 抖动后方差)/基线方差。
// 基线方差不为正时（纯色、无内部间距）无法定义降幅，ok=false。
func SpacingVarianceDrop(before, after SpacingStats) (float64, bool) {
	if before.Variance <= 0 {
		return 0, false
	}
	return 1 - after.Variance/before.Variance, true
}
