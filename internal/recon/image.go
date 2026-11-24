package recon

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
)

// RGB 是一个归一化到 [0,1] 的线性处理用 sRGB 像素（未做 gamma 反转也可直接做色差，
// 转 Lab 时会在 Color.go 内统一做 sRGB 传递函数）。
type RGB struct {
	R, G, B float64
}

// Image 是交错存储的彩色图。
type Image struct {
	W, H int
	Pix  []RGB
}

// NewImage 创建并清零一张图。
func NewImage(w, h int) *Image {
	return &Image{W: w, H: h, Pix: make([]RGB, w*h)}
}

func (im *Image) at(x, y int) RGB { return im.Pix[y*im.W+x] }
func (im *Image) set(x, y int, c RGB) {
	if x >= 0 && x < im.W && y >= 0 && y < im.H {
		im.Pix[y*im.W+x] = clampRGB(c)
	}
}

// AtClamped 返回复制边界后的像素，供卷积使用。
func (im *Image) AtClamped(x, y int) RGB {
	if x < 0 {
		x = 0
	} else if x >= im.W {
		x = im.W - 1
	}
	if y < 0 {
		y = 0
	} else if y >= im.H {
		y = im.H - 1
	}
	return im.Pix[y*im.W+x]
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clampRGB(c RGB) RGB {
	return RGB{clamp01(c.R), clamp01(c.G), clamp01(c.B)}
}

// ---------- 确定性伪随机 / 值噪声 ----------

// lcg 是固定种子的 32 位生成器，保证图集跨机器、跨次运行完全一致。
type lcg uint32

func (s *lcg) next() float64 {
	*s = *s*1664525 + 1013904223
	return float64(*s) / float64(math.MaxUint32)
}

func valueNoise(w, h, knots int, seed uint32) [][]float64 {
	r := lcg(seed)
	grid := make([]float64, knots*knots)
	for i := range grid {
		grid[i] = r.next()
	}
	out := make([][]float64, h)
	for y := 0; y < h; y++ {
		out[y] = make([]float64, w)
		fy := float64(y) / float64(h-1) * float64(knots-1)
		y0 := int(math.Floor(fy))
		ty := fy - float64(y0)
		y1 := y0 + 1
		if y1 >= knots {
			y1 = knots - 1
		}
		for x := 0; x < w; x++ {
			fx := float64(x) / float64(w-1) * float64(knots-1)
			x0 := int(math.Floor(fx))
			tx := fx - float64(x0)
			x1 := x0 + 1
			if x1 >= knots {
				x1 = knots - 1
			}
			v00 := grid[y0*knots+x0]
			v10 := grid[y0*knots+x1]
			v01 := grid[y1*knots+x0]
			v11 := grid[y1*knots+x1]
			a := v00*(1-tx) + v10*tx
			b := v01*(1-tx) + v11*tx
			out[y][x] = a*(1-ty) + b*ty
		}
	}
	return out
}

// SuiteSize 是基准图集的统一画幅（偶数，便于需要时做 2x 降采样）。
const (
	SuiteW = 128
	SuiteH = 96
)

// Suite 生成固定的一组彩色测试图：渐变、径向、几何硬边、正弦中频、值噪声自然场、混合。
// 所有方法（双线性与五抽头）都在这同一组图上比较。
func Suite() []*Image {
	return []*Image{
		gradientImage(),
		radialImage(),
		shapesImage(11),
		sinusImage(),
		noiseImage(29),
		mixedImage(47),
	}
}

func gradientImage() *Image {
	im := NewImage(SuiteW, SuiteH)
	for y := 0; y < SuiteH; y++ {
		for x := 0; x < SuiteW; x++ {
			u := float64(x) / (SuiteW - 1)
			v := float64(y) / (SuiteH - 1)
			im.set(x, y, RGB{
				R: 0.15 + 0.7*u,
				G: 0.2 + 0.6*v,
				B: 0.5 + 0.4*math.Sin(2*math.Pi*u),
			})
		}
	}
	return im
}

func radialImage() *Image {
	im := NewImage(SuiteW, SuiteH)
	for y := 0; y < SuiteH; y++ {
		for x := 0; x < SuiteW; x++ {
			u := (float64(x)/(SuiteW-1) - 0.5) * 2
			v := (float64(y)/(SuiteH-1) - 0.5) * 2
			r := math.Sqrt(u*u+v*v)
			im.set(x, y, RGB{
				R: clamp01(1 - 0.7*r),
				G: clamp01(1 - math.Abs(2*r-1)),
				B: clamp01(0.25 + 0.6*r),
			})
		}
	}
	return im
}

func shapesImage(seed uint32) *Image {
	r := lcg(seed)
	im := NewImage(SuiteW, SuiteH)
	for y := 0; y < SuiteH; y++ {
		for x := 0; x < SuiteW; x++ {
			im.set(x, y, RGB{R: 0.82, G: 0.80, B: 0.76})
		}
	}
	fillDisk(im, 30, 34, 17, RGB{0.72, 0.20, 0.18})
	fillDisk(im, 92, 60, 22, RGB{0.16, 0.42, 0.62})
	fillRect(im, 58, 18, 96, 44, RGB{0.93, 0.78, 0.16})
	// 若干小色块增加边缘与饱和结构。
	for i := 0; i < 6; i++ {
		cx := int(r.next()*float64(SuiteW-10)) + 5
		cy := int(r.next()*float64(SuiteH-10)) + 5
		rad := 4 + int(r.next()*6)
		col := RGB{0.2 + 0.7*r.next(), 0.2 + 0.7*r.next(), 0.2 + 0.7*r.next()}
		fillDisk(im, cx, cy, rad, col)
	}
	return im
}

func fillDisk(im *Image, cx, cy, rad int, c RGB) {
	for y := cy - rad - 1; y <= cy+rad+1; y++ {
		for x := cx - rad - 1; x <= cx+rad+1; x++ {
			d := math.Hypot(float64(x-cx), float64(y-cy))
			a := clamp01(float64(rad) + 0.6 - d) // 轻微抗锯齿
			bg := im.AtClamped(x, y)
			im.set(x, y, RGB{
				R: bg.R*(1-a) + c.R*a,
				G: bg.G*(1-a) + c.G*a,
				B: bg.B*(1-a) + c.B*a,
			})
		}
	}
}

func fillRect(im *Image, x0, y0, x1, y1 int, c RGB) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			im.set(x, y, c)
		}
	}
}

func sinusImage() *Image {
	im := NewImage(SuiteW, SuiteH)
	for y := 0; y < SuiteH; y++ {
		for x := 0; x < SuiteW; x++ {
			u := float64(x) / SuiteW
			v := float64(y) / SuiteH
			l := 0.5 + 0.28*math.Sin(2*math.Pi*(3*u+2*v))
			l += 0.1*math.Sin(2*math.Pi*(9*u-5*v))
			cr := 0.16 * math.Sin(2*math.Pi*(2*u-3*v))
			cb := 0.16 * math.Cos(2*math.Pi*(3*u+1*v))
			im.set(x, y, RGB{
				R: clamp01(l + cr),
				G: clamp01(l + 0.25*(cr+cb)),
				B: clamp01(l + cb),
			})
		}
	}
	return im
}

func noiseImage(seed uint32) *Image {
	lum := valueNoise(SuiteW, SuiteH, 6, seed)
	cr := valueNoise(SuiteW, SuiteH, 4, seed+1)
	cb := valueNoise(SuiteW, SuiteH, 4, seed+2)
	im := NewImage(SuiteW, SuiteH)
	for y := 0; y < SuiteH; y++ {
		for x := 0; x < SuiteW; x++ {
			l := 0.25 + 0.5*lum[y][x]
			im.set(x, y, RGB{
				R: clamp01(l + 0.22*(cr[y][x]-0.5)),
				G: clamp01(l + 0.08*(cr[y][x]+cb[y][x]-1)),
				B: clamp01(l + 0.22*(cb[y][x]-0.5)),
			})
		}
	}
	fillDisk(im, SuiteW/3, SuiteH/2, 12, RGB{0.7, 0.66, 0.6})
	return im
}

func mixedImage(seed uint32) *Image {
	im := noiseImage(seed)
	for y := 20; y < 76; y++ {
		for x := 70; x < 118; x++ {
			// 斜向边界的暗块。
			if x-y > 20 {
				bg := im.at(x, y)
				a := 0.85
				im.set(x, y, RGB{
					R: bg.R*(1-a) + 0.12*a,
					G: bg.G*(1-a) + 0.16*a,
					B: bg.B*(1-a) + 0.24*a,
				})
			}
		}
	}
	return im
}

// ---------- P6 PPM 读写（审计用，不依赖第三方库） ----------

// WritePPM 把图写成二进制 P6 PPM，像素量化到 8-bit。
func WritePPM(path string, im *Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	if _, err := fmt.Fprintf(w, "P6\n%d %d\n255\n", im.W, im.H); err != nil {
		return err
	}
	buf := make([]byte, im.W*3)
	for y := 0; y < im.H; y++ {
		for x := 0; x < im.W; x++ {
			c := im.Pix[y*im.W+x]
			buf[3*x+0] = byte(math.Round(clamp01(c.R) * 255))
			buf[3*x+1] = byte(math.Round(clamp01(c.G) * 255))
			buf[3*x+2] = byte(math.Round(clamp01(c.B) * 255))
		}
		if _, err := w.Write(buf); err != nil {
			return err
		}
	}
	return w.Flush()
}

// ReadPPM 仅支持本工具写出的 P6 8-bit PPM（单空白符头），供测试回读。
func ReadPPM(path string) (*Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	br := bufio.NewReader(f)
	var magic string
	if _, err := fmt.Fscanf(br, "%2s\n", &magic); err != nil {
		return nil, err
	}
	if magic != "P6" {
		return nil, fmt.Errorf("仅支持 P6 PPM，得到 %q", magic)
	}
	var w, h, maxv int
	if _, err := fmt.Fscanf(br, "%d %d\n%d\n", &w, &h, &maxv); err != nil {
		return nil, err
	}
	if maxv != 255 {
		return nil, fmt.Errorf("仅支持 8-bit PPM，max=%d", maxv)
	}
	body, err := io.ReadAll(br)
	if err != nil {
		return nil, err
	}
	if len(body) < w*h*3 {
		return nil, fmt.Errorf("PPM 像素数据不足: %d < %d", len(body), w*h*3)
	}
	im := NewImage(w, h)
	for i := 0; i < w*h; i++ {
		im.Pix[i] = RGB{
			R: float64(body[3*i]) / 255,
			G: float64(body[3*i+1]) / 255,
			B: float64(body[3*i+2]) / 255,
		}
	}
	return im, nil
}

// NameList 是 Suite 对应的稳定文件名（不含扩展名）。
var NameList = []string{
	"01_gradient",
	"02_radial",
	"03_shapes",
	"04_sinus",
	"05_noise",
	"06_mixed",
}
