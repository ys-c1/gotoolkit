package subband

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

// SavePGM 写二进制 P5 PGM。
func SavePGM(path string, w, h int, pix []uint8) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString("P5\n" + itoa(w) + " " + itoa(h) + "\n255\n"); err != nil {
		return err
	}
	_, err = f.Write(pix)
	return err
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// SaveGrayPNG 把灰度像素写成 PNG。
func SaveGrayPNG(path string, w, h int, pix []uint8) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	img := image.NewGray(image.Rect(0, 0, w, h))
	copy(img.Pix, pix)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// MapAuto 用两个浮点图的共同 min/max 把它们映射成灰度，保证左右亮度尺度一致。
// 返回映射后的两张图。
func MapAuto(a, b []float64, w, h int) ([]uint8, []uint8) {
	mn, mx := inf(1), inf(-1)
	for _, v := range a {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	for _, v := range b {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	out1 := mapRange(a, mn, mx)
	out2 := mapRange(b, mn, mx)
	return out1, out2
}

func inf(sign int) float64 {
	if sign < 0 {
		return -1e300
	}
	return 1e300
}

func mapRange(v []float64, mn, mx float64) []uint8 {
	out := make([]uint8, len(v))
	if mx-mn < 1e-9 {
		for i := range out {
			out[i] = 128
		}
		return out
	}
	for i, x := range v {
		g := 255 * (x - mn) / (mx - mn)
		if g < 0 {
			g = 0
		}
		if g > 255 {
			g = 255
		}
		out[i] = uint8(g + 0.5)
	}
	return out
}

// MapSymmetric 以 0 为中灰、按 gain*|v| 对称映射误差图（放大失真用）。
func MapSymmetric(e []float64, gain float64) []uint8 {
	out := make([]uint8, len(e))
	for i, v := range e {
		g := 128 + gain*v
		if g < 0 {
			g = 0
		}
		if g > 255 {
			g = 255
		}
		out[i] = uint8(g + 0.5)
	}
	return out
}

// ComposeRow 把等尺寸灰度面板横向拼成一张 PNG，面板间留 gap 白缝，
// 并用最近邻放大 scale 倍以便查看块状/毛刺细节。
func ComposeRow(path string, panels [][]uint8, w, h, gap, scale int) error {
	k := len(panels)
	nativeW := k*w + (k-1)*gap
	nativeH := h
	canvas := image.NewGray(image.Rect(0, 0, nativeW, nativeH))
	for i := range canvas.Pix {
		canvas.Pix[i] = 255
	}
	for p, data := range panels {
		x0 := p * (w + gap)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				canvas.Pix[y*nativeW+x0+x] = data[y*w+x]
			}
		}
	}
	if scale <= 1 {
		return SaveGrayPNG(path, nativeW, nativeH, canvas.Pix)
	}
	big := image.NewGray(image.Rect(0, 0, nativeW*scale, nativeH*scale))
	for y := 0; y < nativeH*scale; y++ {
		sy := y / scale
		for x := 0; x < nativeW*scale; x++ {
			sx := x / scale
			big.Pix[y*nativeW*scale+x] = canvas.Pix[sy*nativeW+sx]
		}
	}
	return SaveGrayPNG(path, nativeW*scale, nativeH*scale, big.Pix)
}

// CropZoom 从灰度图裁出矩形并最近邻放大 zoom 倍存盘。
func CropZoom(path string, pix []uint8, w, x0, y0, cw, ch, zoom int) error {
	crop := make([]uint8, cw*ch)
	for y := 0; y < ch; y++ {
		for x := 0; x < cw; x++ {
			crop[y*cw+x] = pix[(y0+y)*w+(x0+x)]
		}
	}
	bigW, bigH := cw*zoom, ch*zoom
	big := make([]uint8, bigW*bigH)
	for y := 0; y < bigH; y++ {
		for x := 0; x < bigW; x++ {
			big[y*bigW+x] = crop[(y/zoom)*cw+(x/zoom)]
		}
	}
	return SaveGrayPNG(path, bigW, bigH, big)
}

// 保证 color 包被引用（后续可能给接缝标红）。
var _ = color.Gray{}
