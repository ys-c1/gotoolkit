// Package guard 是基准漂移守卫。它把三份基准的"真值"两边对齐：
//
//   - 基准对照：用 ref/ 独立参照重新生成，与 testdata/golden 逐字节比对；
//   - 实现对照：用 api 对同一样本编码，与 testdata/golden 逐字节比对。
//
// 任一对不上都报首个差异偏移并非零退出；两侧解码出的图并排落盘，并指出
// 差异像素。基准目录与 ref/ 只读，本包从不写它们，也不会"修正"基准。
package guard

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gotoolkit/api"
)

// 基准画幅与像素，必须和 ref/main.go 里的 sample() 逐位一致。
const (
	SampleW = 4
	SampleH = 3
)

var formats = []string{"bmp", "pgm", "rle"}

// EncodeSample 用本仓库的实现在基准样本上编码。
func EncodeSample(format string) ([]byte, error) {
	return api.Encode(&api.Image{W: SampleW, H: SampleH, Pix: Sample()}, api.Options{Format: format})
}

// Sample 复原 ref/main.go 的 sample()：12 字节，pix[i]=i*7。
func Sample() []byte {
	out := make([]byte, SampleW*SampleH)
	for i := range out {
		out[i] = byte(i * 7)
	}
	return out
}

// Pair 是一次"期望 vs 实际"对照，两侧解码出的图与像素差异都在这里。
type Pair struct {
	Name     string // 展示名：基准 / 实现
	Slug     string // 文件前缀：baseline / impl
	Expected string // 期望侧来源说明
	Actual   string // 实际侧来源说明
	Bytes    BytesVerdict
	Dump     *Dump
}

// Equal 表示字节与像素都一致。
func (p *Pair) Equal() bool {
	if !p.Bytes.Equal {
		return false
	}
	if p.Dump != nil && p.Dump.Diff != nil && !p.Dump.Diff.Equal {
		return false
	}
	return true
}

// FileResult 是单个格式的完整结论。
type FileResult struct {
	Format string
	Golden string // golden 基准文件路径
	Pairs  []*Pair
}

// BytesVerdict 是字节比对结论。Equal 为真时其余字段无意义；否则 FirstDiff
// 是首个差异偏移；长度不同而前缀相同时记较短一侧的长度。
type BytesVerdict struct {
	Equal     bool
	GoldenLen int
	FreshLen  int
	FirstDiff int
	Expected  byte // 期望侧在该偏移的字节
	Actual    byte // 实际侧在该偏移的字节
	Context   DiffContext
}

// DiffContext 是首个差异附近两侧的原始字节，便于一眼看出改了什么。
type DiffContext struct {
	Golden []byte
	Fresh  []byte
}

// Dump 是两侧解码后的图、并排预览与像素差异。两侧都能解码时 Diff 才有效。
type Dump struct {
	Width, Height int
	SidePath      string // 并排预览：左期望，右实际
	DiffPath      string // 标出差异像素的预览
	GoldenDecoded string // 期望侧解码出的 PGM
	FreshDecoded  string // 实际侧解码出的 PGM
	GoldenErr     error
	FreshErr      error
	Diff          *PixelDiff
}

// PixelDiff 是像素级差异统计。
type PixelDiff struct {
	Total       int // 两侧共同的 w*h，画幅取交集
	Equal       bool
	Count       int            // 差异像素数
	FirstIndex  int            // 首个差异像素的线性下标，无差异为 -1
	FirstX      int            // 首个差异像素的坐标，无差异为 -1
	FirstY      int            // 同上
	FirstGolden byte           // 首个差异处期望侧的值
	FirstRef    byte           // 首个差异处实际侧的值
	Values      map[uint16]int // 每对 (期望<<8|实际) 出现的次数
	SizeDiffer  bool           // 画幅本身不同
	GoldenW     int
	GoldenH     int
	FreshW      int
	FreshH      int
}

// Report 是一次守卫运行的完整结论，字节与像素两块都在这里。
type Report struct {
	GoldenDir string // 只读基准目录
	RefDir    string // 只读参照目录
	OutDir    string
	Results   []*FileResult
	Drift     []string // 漂移项，空表示全部一致
}

// Check 执行完整守卫，实现侧取本仓库 api。outDir 为空时只比对、不落盘。
func Check(refDir, goldenDir, outDir string) (*Report, error) {
	return CheckWith(refDir, goldenDir, outDir, EncodeSample)
}

// CheckWith 与 Check 相同，但允许注入实现侧编码器，便于测试实现漂移。
func CheckWith(refDir, goldenDir, outDir string, enc func(format string) ([]byte, error)) (*Report, error) {
	fresh, err := Regenerate(refDir)
	if err != nil {
		return nil, err
	}
	if outDir != "" {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return nil, fmt.Errorf("无法创建输出目录: %w", err)
		}
	}
	rep := &Report{GoldenDir: goldenDir, RefDir: refDir, OutDir: outDir}
	for _, f := range formats {
		goldenPath := filepath.Join(goldenDir, f+".bin")
		golden, err := os.ReadFile(goldenPath)
		if err != nil {
			return nil, fmt.Errorf("读取基准 %s 失败（基准只读，不能修补）: %w", goldenPath, err)
		}
		impl, err := enc(f)
		if err != nil {
			return nil, fmt.Errorf("实现编码 %s 失败: %w", f, err)
		}

		r := &FileResult{Format: f, Golden: goldenPath}
		pairs := []struct {
			name, slug, expected, actual string
			body                         []byte
		}{
			{"基准", "baseline", "golden", "ref", fresh[f]},
			{"实现", "impl", "golden", "实现", impl},
		}
		for _, pr := range pairs {
			p, err := comparePair(f, pr.name, pr.slug, pr.expected, pr.actual, golden, pr.body, outDir)
			if err != nil {
				return nil, err
			}
			r.Pairs = append(r.Pairs, p)
			if !p.Equal() {
				rep.Drift = append(rep.Drift, f+"("+pr.name+")")
			}
		}
		rep.Results = append(rep.Results, r)
	}
	if len(rep.Drift) > 0 {
		return rep, fmt.Errorf("基准漂移: %s", strings.Join(rep.Drift, ", "))
	}
	return rep, nil
}

func comparePair(format, name, slug, expected, actual string, want, got []byte, outDir string) (*Pair, error) {
	p := &Pair{Name: name, Slug: slug, Expected: expected, Actual: actual}
	p.Bytes = CompareBytes(want, got)
	if outDir != "" {
		d, err := dumpFormat(format, slug, want, got, outDir)
		if err != nil {
			return nil, err
		}
		p.Dump = d
	}
	return p, nil
}

// CompareBytes 逐字节比对，返回首个差异的相对偏移；长度不同而前缀相同时，
// 首个差异偏移记为较短一侧的长度。
func CompareBytes(golden, fresh []byte) BytesVerdict {
	v := BytesVerdict{GoldenLen: len(golden), FreshLen: len(fresh)}
	n := min(len(golden), len(fresh))
	for i := 0; i < n; i++ {
		if golden[i] != fresh[i] {
			v.FirstDiff = i
			v.Expected = golden[i]
			v.Actual = fresh[i]
			v.Context.Golden = window(golden, i, 8)
			v.Context.Fresh = window(fresh, i, 8)
			return v
		}
	}
	if len(golden) != len(fresh) {
		v.FirstDiff = n
		if n < len(golden) {
			v.Expected = golden[n]
		}
		if n < len(fresh) {
			v.Actual = fresh[n]
		}
		v.Context.Golden = window(golden, n, 8)
		v.Context.Fresh = window(fresh, n, 8)
		return v
	}
	v.Equal = true
	v.FirstDiff = -1
	return v
}

func window(b []byte, at, back int) []byte {
	lo := max(0, at-back)
	hi := min(len(b), at+back)
	return append([]byte(nil), b[lo:hi]...)
}

// PixelCompare 比对两侧解码出的图。尺寸不同时按交集比像素，并标记 SizeDiffer。
func PixelCompare(gw, gh int, gp []byte, fw, fh int, fp []byte) *PixelDiff {
	if gw == fw && gh == fh && len(gp) == len(fp) && bytes.Equal(gp, fp) {
		return &PixelDiff{
			Total: gw * gh, Equal: true, FirstIndex: -1, FirstX: -1, FirstY: -1,
			GoldenW: gw, GoldenH: gh, FreshW: fw, FreshH: fh,
		}
	}
	w, h := min(gw, fw), min(gh, fh)
	d := &PixelDiff{
		Total:      w * h,
		FirstIndex: -1,
		FirstX:     -1,
		FirstY:     -1,
		Values:     map[uint16]int{},
		SizeDiffer: gw != fw || gh != fh,
		GoldenW:    gw,
		GoldenH:    gh,
		FreshW:     fw,
		FreshH:     fh,
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*gw + x
			j := y*fw + x
			if gp[i] == fp[j] {
				continue
			}
			d.Count++
			d.Values[uint16(gp[i])<<8|uint16(fp[j])]++
			if d.FirstIndex < 0 {
				d.FirstIndex, d.FirstX, d.FirstY = i, x, y
				d.FirstGolden, d.FirstRef = gp[i], fp[j]
			}
		}
	}
	d.Equal = d.Count == 0 && !d.SizeDiffer
	return d
}

// Change 是某一对灰度的替换，便于排序稳定输出。
type Change struct {
	Golden, Ref byte
	Count       int
}

// Changes 把像素差异按"期望值 -> 实际值"聚合并排序，个数降序、同数按值升序。
func (d *PixelDiff) Changes() []Change {
	out := make([]Change, 0, len(d.Values))
	for k, n := range d.Values {
		out = append(out, Change{Golden: byte(k >> 8), Ref: byte(k & 0xff), Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].Golden != out[j].Golden {
			return out[i].Golden < out[j].Golden
		}
		return out[i].Ref < out[j].Ref
	})
	return out
}

func dumpFormat(format, slug string, want, got []byte, outDir string) (*Dump, error) {
	d := &Dump{Width: -1, Height: -1}
	expImg, expErr := api.Decode(want, api.Options{Format: format})
	actImg, actErr := api.Decode(got, api.Options{Format: format})
	d.GoldenErr, d.FreshErr = expErr, actErr
	if expErr != nil || actErr != nil {
		return d, nil // 坏样本解码不了属正常，不落盘
	}
	d.Width, d.Height = expImg.W, expImg.H
	d.Diff = PixelCompare(expImg.W, expImg.H, expImg.Pix, actImg.W, actImg.H, actImg.Pix)

	prefix := filepath.Join(outDir, format+"."+slug)
	var err error
	if d.GoldenDecoded, err = writePGM(prefix+".golden.pgm", expImg); err != nil {
		return nil, err
	}
	if d.FreshDecoded, err = writePGM(prefix+".actual.pgm", actImg); err != nil {
		return nil, err
	}
	if d.SidePath, err = writePNG(prefix+".side.png", sideBySide(expImg, actImg, d.Diff)); err != nil {
		return nil, err
	}
	if d.DiffPath, err = writePNG(prefix+".diff.png", render(actImg, d.Diff.FirstX, d.Diff.FirstY)); err != nil {
		return nil, err
	}
	return d, nil
}

func writePGM(path string, img *api.Image) (string, error) {
	body, err := api.Encode(img, api.Options{Format: "pgm"})
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func writePNG(path string, m image.Image) (string, error) {
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	if err := png.Encode(f, m); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return path, nil
}

// render 把灰度图放大渲染成 PNG；mx/my>=0 时用白框标出该坐标。
func render(img *api.Image, mx, my int) image.Image {
	const scale = 8
	if img == nil || img.W <= 0 || img.H <= 0 {
		return image.NewGray(image.Rect(0, 0, 1, 1))
	}
	m := image.NewGray(image.Rect(0, 0, img.W*scale, img.H*scale))
	for y := 0; y < img.H; y++ {
		for x := 0; x < img.W; x++ {
			v := color.Gray{Y: img.Pix[y*img.W+x]}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					m.SetGray(x*scale+dx, y*scale+dy, v)
				}
			}
		}
	}
	if mx >= 0 && my >= 0 {
		marker(m, mx, my, scale, color.Gray{Y: 255})
	}
	return m
}

func marker(m *image.Gray, x, y, scale int, c color.Gray) {
	markerOff(m, x, y, 0, scale, c)
}

func markerOff(m *image.Gray, x, y, oy, scale int, c color.Gray) {
	x0, y0 := x*scale, oy+y*scale
	x1, y1 := x0+scale-1, y0+scale-1
	for i := 0; i < scale; i++ {
		m.SetGray(x0+i, y0, c)
		m.SetGray(x0+i, y1, c)
		m.SetGray(x0, y0+i, c)
		m.SetGray(x1, y0+i, c)
	}
}

// sideBySide 把期望侧与实际侧并排，中间留灰缝。任一侧的差异像素都画白框，
// 首个差异像素着重标出。
func sideBySide(g, f *api.Image, diff *PixelDiff) image.Image {
	const scale = 8
	const gap = 16
	const band = 8
	w, h := max(g.W, 1), max(f.W, 1)
	rect := image.Rect(0, 0, w*scale+gap+h*scale, band+max(g.H, 1)*scale)
	m := image.NewGray(rect)
	for y := 0; y < rect.Dy(); y++ {
		for x := w * scale; x < w*scale+gap; x++ {
			m.SetGray(x, y, color.Gray{Y: 0x60})
		}
	}
	blit := func(img *api.Image, ox int) {
		for y := 0; y < img.H; y++ {
			for x := 0; x < img.W; x++ {
				v := color.Gray{Y: img.Pix[y*img.W+x]}
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						m.SetGray(ox+x*scale+dx, band+y*scale+dy, v)
					}
				}
			}
		}
	}
	blit(g, 0)
	blit(f, w*scale+gap)
	if diff != nil && diff.Count > 0 {
		for y := 0; y < min(g.H, f.H); y++ {
			for x := 0; x < min(g.W, f.W); x++ {
				if g.Pix[y*g.W+x] == f.Pix[y*f.W+x] {
					continue
				}
				markerOff(m, x, y, band, scale, color.Gray{Y: 255})
				markerOff(m, w*scale+gap+x, y, band, scale, color.Gray{Y: 255})
			}
		}
	}
	return m
}
