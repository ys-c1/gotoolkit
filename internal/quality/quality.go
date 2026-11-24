// Package quality 实现“画质阈值 → 量化档位反解 → 三格式真实体积对账 → 逐像素核对 → 最省合格格式选择”。
//
// 量化器采用 2 的幂步长的均匀量化 + 中点反量化。令右移位数 s∈[0,7]，
// 有效位深 b=8−s：
//
//	s=0 时为恒等（无损），最大误差 E=0；
//	s≥1 时步长 Δ=2^s，每个量化桶宽 Δ，中点重建，故最大逐像素误差
//	    E(s) = Δ/2 = 2^(s−1)。
//
// 给定画质阈值 τ（允许的最大逐像素绝对差，0..255），反解最粗合格档：
//
//	s*(τ) = 0,                         τ=0
//	        min(7, ⌊log2 τ⌋ + 1),       τ≥1
//
// 因为 E(s+1)=2^s ≤ τ ⇔ s ≤ ⌊log2 τ⌋。
//
// 三种容器（PGM/BMP/RLE）本身均无损，量化在编码前统一进行，因此同一档位下
// 三者的量化误差完全相同，差异只在真实编码字节数。PGM/BMP 的体积只由画幅
// 决定（有闭式式），RLE 的体积 = 8 + 2×游程块数；游程块数随档位变粗单调不增。
// 为防止“图像实际误差小于最坏值上界、更粗档位反而合格”的情况，选择前会对
// s=0..7 全档真实编码 + 解码 + 逐像素扫描，保证选中的格式与体积是真正的最小。
package quality

import (
	"errors"
	"fmt"
	"math"

	"gotoolkit/internal/codec"
)

// MaxThreshold 是阈值的合法上界（灰度绝对差最大为 255）。
const MaxThreshold = 255

// MaxShift 是最大右移位数（s=7 对应 1 位图像）。
const MaxShift = 7

// Formats 是参与比较的三种格式，顺序固定，保证报告稳定。
var Formats = []string{"pgm", "bmp", "rle"}

// ErrBadSize 表示画幅或像素长度不合法。
var ErrBadSize = errors.New("画幅必须为正且像素数匹配")

// ErrUnknownFormat 表示请求了 pgm/bmp/rle 之外的格式。
var ErrUnknownFormat = errors.New("未知格式")

// ErrorBound 返回移位 s 对应的最坏情况最大逐像素误差上界。
func ErrorBound(shift int) int {
	if shift <= 0 {
		return 0
	}
	if shift >= MaxShift {
		return 1 << (MaxShift - 1)
	}
	return 1 << (shift - 1)
}

// ShiftForThreshold 按阈值 τ 闭式反解最粗合格移位 s*。
// τ≤0 只能无损（s=0）；τ≥MaxThreshold 也只会落到最粗的 s=7。
func ShiftForThreshold(tau int) int {
	if tau <= 0 {
		return 0
	}
	s := 0
	// 进到 s+1 档的条件是 E(s+1)=1<<s ≤ τ。
	for s < MaxShift && (1<<s) <= tau {
		s++
	}
	return s
}

// BitDepth 返回移位 s 对应的有效位深 b=8−s。
func BitDepth(shift int) int {
	if shift < 0 {
		shift = 0
	}
	if shift > 8 {
		shift = 8
	}
	return 8 - shift
}

// Quantize 对像素做右移均匀量化并中点反量化回 8 位灰度。
// shift=0 时原样复制；量化误差上界见 ErrorBound。
func Quantize(pix []uint8, shift int) []uint8 {
	out := make([]uint8, len(pix))
	if shift <= 0 {
		copy(out, pix)
		return out
	}
	if shift > MaxShift {
		shift = MaxShift
	}
	step := 1 << shift
	half := step >> 1
	for i, v := range pix {
		q := int(v) >> shift
		r := q*step + half
		if r > 255 {
			r = 255
		}
		out[i] = uint8(r)
	}
	return out
}

// Diff 是两帧等大灰度图逐像素比较的结果。
type Diff struct {
	N          int     `json:"n"`          // 像素总数
	MaxAbs     int     `json:"maxAbs"`     // 最大绝对差
	MAE        float64 `json:"mae"`        // 平均绝对误差
	DiffPixels int     `json:"diffPixels"` // 不相等像素数
}

// DiffRatio 返回不相等像素占比（0..1）。
func (d Diff) DiffRatio() float64 {
	if d.N == 0 {
		return 0
	}
	return float64(d.DiffPixels) / float64(d.N)
}

// Compare 逐像素比较原图 a 与重建图 b。
func Compare(a, b []uint8) Diff {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	d := Diff{N: len(a)}
	var sum int64
	for i := 0; i < n; i++ {
		x := int(a[i]) - int(b[i])
		if x < 0 {
			x = -x
		}
		if x > d.MaxAbs {
			d.MaxAbs = x
		}
		sum += int64(x)
		if x != 0 {
			d.DiffPixels++
		}
	}
	if d.N > 0 {
		d.MAE = float64(sum) / float64(d.N)
	}
	return d
}

// DiffRaster 生成放大 amp 倍的绝对差灰度图（截断到 255），用于“出图看差”。
func DiffRaster(a, b []uint8, amp int) []uint8 {
	out := make([]uint8, len(a))
	if amp <= 0 {
		amp = 1
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		v := d * amp
		if v > 255 {
			v = 255
		}
		out[i] = uint8(v)
	}
	return out
}

// PGMHeaderLen 按 PGM P5 头的真实 Sprintf 格式计算头长度。
func PGMHeaderLen(w, h int) int {
	return len([]byte(fmt.Sprintf("P5\n%d %d\n255\n", w, h)))
}

// FormulaPGMSize 是 PGM 的闭式体积：头 + w×h 灰度字节。
func FormulaPGMSize(w, h int) int { return PGMHeaderLen(w, h) + w*h }

// FormulaBMPSize 是 24 位灰度 BMP 的闭式体积：54 头 + 4 字节对齐行×h。
func FormulaBMPSize(w, h int) int { return 54 + ((w*3+3)&^3)*h }

// CountRLEChunks 按编码器同样的规则统计游程块数（同值连续段，每块最长 255）。
func CountRLEChunks(pix []uint8) int {
	chunks := 0
	i := 0
	for i < len(pix) {
		j := i
		for j < len(pix) && pix[j] == pix[i] && j-i < 255 {
			j++
		}
		chunks++
		i = j
	}
	return chunks
}

// FormulaRLESize 是 GTR1 RLE 的闭式体积：8 头 + 每块 (计数,值) 两字节。
func FormulaRLESize(pix []uint8) int { return 8 + 2*CountRLEChunks(pix) }

// Point 是“某格式 × 某量化档”一次真实编解码的全部对账数据。
type Point struct {
	Format       string `json:"format"`
	Shift        int    `json:"shift"`
	BitDepth     int    `json:"bitDepth"`
	Bound        int    `json:"bound"`        // 该档误差闭式上界
	Bytes        int    `json:"bytes"`        // 真实编码字节数
	FormulaBytes int    `json:"formulaBytes"` // 按画幅/游程推出的体积
	FormulaExact bool   `json:"formulaExact"` // 闭式体积是否与真实字节逐字节一致
	Measured     Diff   `json:"measured"`     // 编码→解码后对原图的逐像素误差
	Eligible     bool   `json:"eligible"`     // 实测最大误差 ≤ 阈值

	Body  []byte  `json:"-"` // 真实编码产物
	Recon []uint8 `json:"-"` // 解码回的灰度
}

// FormatResult 是一个格式在某阈值下的推导点与全档扫描最优点。
type FormatResult struct {
	Format string `json:"format"`
	// Planned 是按阈值闭式反解出的档位对应的点（三种格式同档）。
	Planned Point `json:"planned"`
	// Best 是 s=0..7 中合格且体积最小的点（真正的“满足阈值的最小体积”）。
	Best Point `json:"best"`
	// Chosen 是最终用于出图与结论的点：正常情况下等于 Planned
	// （MinBytesConfirmed=true）；若图像误差未达最坏上界、更粗档实测更省，
	// 则退化为 Best，保证“体积最小”始终成立。
	Chosen Point `json:"chosen"`
	// MinBytesConfirmed 表示全档扫描中不存在比 Planned 体积更小的合格点；
	// 规范样本上 Planned 与 Best 体积相同（PGM/BMP 恒等，RLE 单调性保证）。
	MinBytesConfirmed bool `json:"minBytesConfirmed"`
}

// TierReport 是一个画幅档位在某阈值下的完整结论。
type TierReport struct {
	Threshold    int    `json:"threshold"`
	W, H, N      int    `json:"-"`
	Label        string `json:"label"`
	PlanShift    int    `json:"planShift"`
	PlanBitDepth int    `json:"planBitDepth"`
	PlanBound    int    `json:"planBound"`

	Formats []FormatResult `json:"formats"`

	SelectedFormat string `json:"selectedFormat"`
	SelectedBytes  int    `json:"selectedBytes"`
	SelectedShift  int    `json:"selectedShift"`

	// AllPlannedEligible：三种格式的推导点经逐像素核对都在阈值内。
	AllPlannedEligible bool `json:"allPlannedEligible"`
	// BoundHolds：所有点的实测最大误差都不超过闭式上界。
	BoundHolds bool `json:"boundHolds"`
	// ReconIdentical：三种格式解码重建出的像素逐字节相同
	// （容器均无损，差异只可能来自量化）。
	ReconIdentical bool `json:"reconIdentical"`
}

// Dims 返回 “WxH” 形式。
func (t *TierReport) Dims() string { return fmt.Sprintf("%dx%d", t.W, t.H) }

func encodeWith(f string, w, h int, pix []uint8) ([]byte, error) {
	switch f {
	case "pgm":
		return codec.EncodePGM(w, h, pix)
	case "bmp":
		return codec.EncodeBMP(w, h, pix)
	case "rle":
		return codec.EncodeRLE(w, h, pix)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownFormat, f)
	}
}

func decodeWith(f string, body []byte) (int, int, []uint8, error) {
	switch f {
	case "pgm":
		return codec.DecodePGM(body)
	case "bmp":
		return codec.DecodeBMP(body)
	case "rle":
		return codec.DecodeRLE(body)
	default:
		return 0, 0, nil, fmt.Errorf("%w: %q", ErrUnknownFormat, f)
	}
}

// formulaSize 按格式的闭式式计算体积。
func formulaSize(f string, w, h int, quantized []uint8) int {
	switch f {
	case "pgm":
		return FormulaPGMSize(w, h)
	case "bmp":
		return FormulaBMPSize(w, h)
	default:
		return FormulaRLESize(quantized)
	}
}

// analyzePoint 完成单个“格式×档位”的真实编码、解码与逐像素核对。
func analyzePoint(f string, w, h int, src []uint8, tau, s int) (Point, error) {
	q := Quantize(src, s)
	body, err := encodeWith(f, w, h, q)
	if err != nil {
		return Point{}, err
	}
	dw, dh, recon, err := decodeWith(f, body)
	if err != nil {
		return Point{}, err
	}
	if dw != w || dh != h || len(recon) != w*h {
		return Point{}, fmt.Errorf("%s 回读画幅不一致", f)
	}
	m := Compare(src, recon)
	fs := formulaSize(f, w, h, q)
	return Point{
		Format:       f,
		Shift:        s,
		BitDepth:     BitDepth(s),
		Bound:        ErrorBound(s),
		Bytes:        len(body),
		FormulaBytes: fs,
		FormulaExact: fs == len(body),
		Measured:     m,
		Eligible:     m.MaxAbs <= tau,
		Body:         body,
		Recon:        recon,
	}, nil
}

// AnalyzeTier 对一个画幅执行完整分析：闭式反解 + 三格式全档扫描 + 最省选择。
func AnalyzeTier(w, h int, src []uint8, tau int, label string) (*TierReport, error) {
	if w <= 0 || h <= 0 || len(src) != w*h {
		return nil, ErrBadSize
	}
	if tau < 0 || tau > MaxThreshold {
		return nil, fmt.Errorf("%w: threshold=%d 超出 [0,%d]", ErrBadSize, tau, MaxThreshold)
	}
	ps := ShiftForThreshold(tau)
	tr := &TierReport{
		Threshold:    tau,
		W:            w,
		H:            h,
		N:            w * h,
		Label:        label,
		PlanShift:    ps,
		PlanBitDepth: BitDepth(ps),
		PlanBound:    ErrorBound(ps),
	}

	bestFmt := ""
	bestBytes := math.MaxInt
	bestShift := 0
	bestMAE := math.MaxFloat64
	bestMax := math.MaxInt
	boundHolds := true

	for _, f := range Formats {
		fr := FormatResult{Format: f}
		var minEligible Point
		have := false
		for s := 0; s <= MaxShift; s++ {
			p, err := analyzePoint(f, w, h, src, tau, s)
			if err != nil {
				return nil, err
			}
			if p.Measured.MaxAbs > p.Bound {
				boundHolds = false
			}
			if s == ps {
				fr.Planned = p
			}
			if p.Eligible {
				// 体积优先；同体积取误差更小、再取 MAE 更小、最后档位更细。
				if !have || p.Bytes < minEligible.Bytes ||
					(p.Bytes == minEligible.Bytes && p.Measured.MaxAbs < minEligible.Measured.MaxAbs) ||
					(p.Bytes == minEligible.Bytes && p.Measured.MaxAbs == minEligible.Measured.MaxAbs && p.Measured.MAE < minEligible.Measured.MAE) ||
					(p.Bytes == minEligible.Bytes && p.Measured.MaxAbs == minEligible.Measured.MaxAbs && almostEqual(p.Measured.MAE, minEligible.Measured.MAE) && p.Shift < minEligible.Shift) {
					minEligible = p
					have = true
				}
			}
		}
		if !have {
			// s=0 恒无损，τ≥0 时必然合格，理论上到不了这里。
			return nil, fmt.Errorf("%s 在阈值 %d 下无合格档位", f, tau)
		}
		fr.Best = minEligible
		// 推导点体积必须等于该格式的最小合格体积，才称对账确认。
		fr.MinBytesConfirmed = fr.Planned.Eligible && fr.Planned.Bytes == minEligible.Bytes
		if fr.MinBytesConfirmed {
			// 规范路径：结论与出图都用阈值反解档，三种格式误差一致、可直接逐像素对照。
			fr.Chosen = fr.Planned
		} else {
			// 兜底：闭式上界偏保守时，用全档扫描到的真正最省合格点。
			fr.Chosen = fr.Best
		}
		tr.Formats = append(tr.Formats, fr)

		c := fr.Chosen
		if bestFmt == "" ||
			c.Bytes < bestBytes ||
			(c.Bytes == bestBytes && c.Measured.MaxAbs < bestMax) ||
			(c.Bytes == bestBytes && c.Measured.MaxAbs == bestMax && c.Measured.MAE < bestMAE) ||
			(c.Bytes == bestBytes && c.Measured.MaxAbs == bestMax && almostEqual(c.Measured.MAE, bestMAE) && f < bestFmt) {
			bestFmt = f
			bestBytes = c.Bytes
			bestShift = c.Shift
			bestMax = c.Measured.MaxAbs
			bestMAE = c.Measured.MAE
		}
	}

	tr.SelectedFormat = bestFmt
	tr.SelectedBytes = bestBytes
	tr.SelectedShift = bestShift
	tr.BoundHolds = boundHolds

	tr.AllPlannedEligible = true
	tr.ReconIdentical = true
	var recon0 []uint8
	for i, fr := range tr.Formats {
		if !fr.Planned.Eligible {
			tr.AllPlannedEligible = false
		}
		if i == 0 {
			recon0 = fr.Planned.Recon
			continue
		}
		if !bytesEqual(recon0, fr.Planned.Recon) {
			tr.ReconIdentical = false
		}
	}
	return tr, nil
}

func bytesEqual(a, b []uint8) bool {
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

func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-12 }
