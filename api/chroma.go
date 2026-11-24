package api

import (
	"errors"

	"gotoolkit/internal/chroma"
	"gotoolkit/internal/codec"
)

// ColorImage 是解码后的 8 位彩色图（RGB 紧密排列，长度 W*H*3）。
type ColorImage struct {
	W, H int
	Pix  []uint8
}

// 色度重建模式。零值（空串）表示不启用色度路径，现有灰度行为逐字节不变。
const (
	// ChromaReconBilinear 色度 4:2:0 后直接双线性重建（基线）。
	ChromaReconBilinear = chroma.ModeBilinear
	// ChromaReconAdaptive 按色度-亮度相关性、以局部亮度方差调整权重的重建。
	ChromaReconAdaptive = chroma.ModeAdaptive
)

var (
	// ErrChromaOddSize 画幅宽高不为偶数，无法做 4:2:0。
	ErrChromaOddSize = chroma.ErrOddSize
)

// EncodePPM 把彩色图编码成 P6 PPM。
func EncodePPM(img *ColorImage) ([]byte, error) {
	if img == nil || img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H*3 {
		return nil, ErrBadSize
	}
	return codec.EncodePPM(img.W, img.H, img.Pix)
}

// DecodePPM 把 P6 PPM 字节解码成彩色图。
func DecodePPM(data []byte) (*ColorImage, error) {
	if len(data) == 0 {
		return nil, ErrTruncated
	}
	w, h, pix, err := codec.DecodePPM(data)
	if err != nil {
		return nil, err
	}
	return &ColorImage{W: w, H: h, Pix: pix}, nil
}

// ChromaRoundTrip 对彩色图做 4:2:0 色度降采样再按 opt.ChromaRecon 指定模式重建。
//
// opt.ChromaRecon 必须是 ChromaReconBilinear 或 ChromaReconAdaptive；
// 亮度全分辨率保留，画幅宽高需均为偶数。返回重建后的彩色图（尺寸与输入相同）。
func ChromaRoundTrip(img *ColorImage, opt Options) (*ColorImage, error) {
	if img == nil || img.W <= 0 || img.H <= 0 || len(img.Pix) != img.W*img.H*3 {
		return nil, ErrBadSize
	}
	if err := opt.validateChroma(); err != nil {
		return nil, err
	}
	rgb := &chroma.RGB{W: img.W, H: img.H, Pix: img.Pix}
	c, err := chroma.Subsample(rgb)
	if err != nil {
		if errors.Is(err, chroma.ErrOddSize) {
			return nil, ErrChromaOddSize
		}
		return nil, err
	}
	out, err := chroma.Reconstruct(c, opt.ChromaRecon)
	if err != nil {
		return nil, err
	}
	return &ColorImage{W: out.W, H: out.H, Pix: out.Pix}, nil
}

// ChromaColorDiff 在边缘像素上比较原图与重建图，返回平均/最大色差。
// 同时给出色度空间（CbCr 欧氏距离）与最终 RGB 欧氏距离两组结果。
func ChromaColorDiff(ref, recon *ColorImage, opt Options) (mean, max, rgbMean, rgbMax float64, err error) {
	if ref == nil || recon == nil || ref.W != recon.W || ref.H != recon.H {
		return 0, 0, 0, 0, ErrBadSize
	}
	if err = opt.validateChroma(); err != nil {
		return 0, 0, 0, 0, err
	}
	c, err := chroma.Subsample(&chroma.RGB{W: ref.W, H: ref.H, Pix: ref.Pix})
	if err != nil {
		return 0, 0, 0, 0, err
	}
	m := chroma.ColorDiff(
		&chroma.RGB{W: ref.W, H: ref.H, Pix: ref.Pix},
		c,
		&chroma.RGB{W: recon.W, H: recon.H, Pix: recon.Pix},
	)
	return m.CbCrMean, m.CbCrMax, m.RGBMean, m.RGBMax, nil
}
