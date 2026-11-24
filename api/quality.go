package api

// 本文件把 internal/quality 的画质阈值选格式能力挂到唯一对外入口 api 包下。
// 调用方（含 cmd/gtk）只依赖 api，不直接引用 internal 子包。

import "gotoolkit/internal/quality"

// 阈值与量化档边界常量。
const (
	MaxThreshold = quality.MaxThreshold // 合法阈值上界（灰度绝对差）
	MaxShift     = quality.MaxShift     // 最大右移位数
)

// 类型别名，字段与 internal/quality 保持一致。
type (
	PixelDiff       = quality.Diff
	QualityPoint    = quality.Point
	FormatResult    = quality.FormatResult
	TierReport      = quality.TierReport
	ThresholdRun    = quality.ThresholdRun
	BenchReport     = quality.BenchReport
	Consistency     = quality.Consistency
	ConsistencyItem = quality.ConsistencyItem
	BenchImage      = quality.Image
)

// DefaultSizes 是五档固定画幅。
var DefaultSizes = quality.DefaultSizes

// 闭式反解、量化、逐像素比较与基准入口。
var (
	ShiftForThreshold = quality.ShiftForThreshold
	ErrorBound        = quality.ErrorBound
	BitDepth          = quality.BitDepth
	QuantizePixels    = quality.Quantize
	ComparePixels     = quality.Compare
	DiffRaster        = quality.DiffRaster

	FormulaPGMSize = quality.FormulaPGMSize
	FormulaBMPSize = quality.FormulaBMPSize
	FormulaRLESize = quality.FormulaRLESize
	CountRLEChunks = quality.CountRLEChunks

	CanonicalPattern = quality.Pattern
	CanonicalImages  = quality.CanonicalImages
	AnalyzeTier      = quality.AnalyzeTier
	RunThresholdOn   = quality.RunThresholdOn
	RunCanonical     = quality.RunCanonical
	CompareRuns      = quality.CompareRuns
)
