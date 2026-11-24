package api

import (
	"fmt"

	"gotoolkit/internal/quantbias"
)

// 量化偏置分析的对外类型与常量。用类型别名把 internal 实现挂到唯一入口 api，
// 调用方只需面对 api.QuantBiasReport / api.QuantBiasCell 等名字。
const (
	QuantBiasTolerance = quantbias.BiasTolerance
	DefaultQuantSize   = quantbias.DefaultSize
	DefaultQuantSeed   = quantbias.DefaultSeed
)

type (
	QuantBiasCell    = quantbias.Cell
	QuantBiasReport  = quantbias.Report
	QuantBiasVerdict = quantbias.ClassVerdict
)

// QuantClassNames 返回四类测试图的固定名字（顺序即报告顺序）。
func QuantClassNames() []string { return append([]string(nil), quantbias.ClassNames...) }

// OriginalQuantOffsetGray 是参照用原始量化器的偏移（桶下沿重建，δ=0）。
const OriginalQuantOffsetGray = quantbias.OriginalOffsetGray

// QuantBin 返回像素在 b 位均匀量化下的档号与桶内相位。
func QuantBin(v uint8, bits int) (q, phase int) { return quantbias.Bin(v, bits) }

// QuantLevel 返回档 q 在步长 Δ 与重建偏移 δ 下的重建电平 qΔ+δ。
func QuantLevel(q int, delta, offsetGray float64) float64 {
	return quantbias.Level(q, delta, offsetGray)
}

// DefaultQuantBits 是偏置研究固定的三档位深 2/3/4 bit。
func DefaultQuantBits() []int { return append([]int(nil), quantbias.DefaultBits...) }

// SampleQuantImages 确定性生成四类测试图（gradient/photo/texture/noise）。
func SampleQuantImages(size int, seed int64) map[string]*Image {
	raw := quantbias.SampleImages(size, seed)
	out := make(map[string]*Image, len(raw))
	for name, pix := range raw {
		out[name] = &Image{W: size, H: size, Pix: pix}
	}
	return out
}

// AnalyzeQuantBias 对单张图在给定位深下测量偏置项、散度与最优重建偏移。
func AnalyzeQuantBias(img *Image, bits int, class string) (QuantBiasCell, error) {
	if img == nil || img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
		return QuantBiasCell{}, ErrBadSize
	}
	if bits < 1 || bits > 8 {
		return QuantBiasCell{}, fmt.Errorf("%w: 位深 %d 超出 1..8", ErrBadOption, bits)
	}
	return quantbias.Analyze(img.Pix, img.W, img.H, bits, class), nil
}

// BuildQuantBiasReport 跑四类图 × 给定位深的完整分析。bits 为 nil 时用三档默认。
func BuildQuantBiasReport(imgs map[string]*Image, bits []int, seed int64) (*QuantBiasReport, error) {
	if len(bits) == 0 {
		bits = quantbias.DefaultBits
	}
	raw := make(map[string][]uint8, len(imgs))
	var w, h int
	for name, img := range imgs {
		if img == nil || img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H {
			return nil, fmt.Errorf("%w: %s", ErrBadSize, name)
		}
		w, h = img.W, img.H
		raw[name] = img.Pix
	}
	if w == 0 {
		w, h = quantbias.DefaultSize, quantbias.DefaultSize
	}
	return quantbias.BuildReport(raw, w, h, bits, seed), nil
}
