// Package roundtrip 对三种格式做往返校验：同一张图经某种格式编码一圈再解码，
// 逐像素与源图比对；同时从码流本身（而不是复用解码器）解析头字段——宽、高、
// 灰度位深；再把解码结果重编码，核对字节是否与原始码流一致。
//
// 三种格式各出一份报告，三份报告合并成一次总判定：任一份的像素、头字段或
// 重编码字节不一致，整体即判为失败。
//
// 校验本身不改变任何编码路径：不做改动（Options 默认值）时，Encode 输出的
// 字节与 testdata/golden 逐字节一致。
package roundtrip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"os"
	"path/filepath"
	"strings"

	"gotoolkit/api"
)

// Formats 是参与往返校验的三种格式，顺序固定，报告也按此顺序分节。
var Formats = []string{"pgm", "bmp", "rle"}

// SampleBitDepth 是本仓库灰度图的位深：每个像素样本 8 位。
const SampleBitDepth = 8

// Header 是从码流本身解析出的头字段。
//
// BitDepth 是「每个灰度样本的有效位数」，三种格式都是 8，因而是三份报告之间
// 可以横向比较的口径。BitsPerPixel 是容器里每个像素实际占用的位数（BMP 为
// 24，即 R/G/B 三通道同值），仅用于说明，不参与一致性判定。
type Header struct {
	Width        int
	Height       int
	BitDepth     int
	BitsPerPixel int
}

// Report 是单一格式的往返报告。所有差异量都是计数或偏移，报告里直接给出数字。
type Report struct {
	Format string

	SourceWidth    int
	SourceHeight   int
	SourceBitDepth int

	// Encoded 是本次校验实际使用的码流（Encode 的产物），供落盘与外部复核。
	Encoded      []byte
	EncodedBytes int

	Header       Header
	HeaderParsed bool
	HeaderOK     bool // 头字段的宽、高、位深与源图逐一相等

	// Decoded 是 Encoded 按同格式解回来的图；解码失败时为 nil。
	Decoded       *api.Image
	DecodedWidth  int
	DecodedHeight int
	DecodedPixels int
	DimsOK        bool // 解码出的宽高与源图相等

	PixelDiffs int // 与源图像素不同的位置数
	MaxAbsDiff int // 所有位置上的最大绝对差（0..255）
	FirstDiff  int // 第一个不同像素的下标，0 起；-1 表示无差异
	PixelsOK   bool

	ReencodedBytes int // 解码结果重新编码的字节数；-1 表示未执行
	ByteDiffs      int // 重编码码流与原始码流的字节差异数；-1 表示未执行
	FirstByteDiff  int // 第一个不同字节的偏移，0 起；-1 表示无差异
	ByteOK         bool

	OK     bool
	Issues []string
}

// Suite 是三份报告合并后的结果，也就是一次总判定。
type Suite struct {
	SourceWidth    int
	SourceHeight   int
	SourcePixels   int
	SourceBitDepth int

	Reports []Report

	TotalEncodedBytes   int
	TotalReencodedBytes int
	TotalPixelDiffs     int
	TotalByteDiffs      int
	TotalCrossDiffs     int // 三格式解码结果两两之间的像素差异数
	Passed              int // 通过的报告份数
	Expected            int // 应当存在的报告份数（= len(Formats)）
	OK                  bool
	Issues              []string
}

// Verify 对一张图按 format 做一次完整往返：编码 → 解析头 → 解码 → 比对 →
// 重编码比对。编码失败返回 error；校验层面的任何不一致都体现在 Report 里，
// 由 Report.OK 给出，不返回 error。
func Verify(src *api.Image, format string) (Report, error) {
	body, err := api.Encode(src, api.Options{Format: format})
	if err != nil {
		return Report{}, err
	}
	return Evaluate(src, format, body), nil
}

// Evaluate 对一份已有码流做往返校验，不重新编码。它给故障注入测试留出接缝：
// 传入被改坏的码流即可验证像素差、头差与字节差都能被如实计数。
func Evaluate(src *api.Image, format string, encoded []byte) Report {
	r := Report{
		Format:         format,
		SourceWidth:    src.W,
		SourceHeight:   src.H,
		SourceBitDepth: SampleBitDepth,
		Encoded:        append([]byte(nil), encoded...),
		EncodedBytes:   len(encoded),
		FirstDiff:      -1,
		FirstByteDiff:  -1,
		ReencodedBytes: -1,
		ByteDiffs:      -1,
	}

	hdr, err := ParseHeader(format, encoded)
	if err != nil {
		r.Issues = append(r.Issues, "头解析失败: "+err.Error())
	} else {
		r.Header = hdr
		r.HeaderParsed = true
		r.HeaderOK = hdr.Width == src.W && hdr.Height == src.H && hdr.BitDepth == SampleBitDepth
		if !r.HeaderOK {
			r.Issues = append(r.Issues, fmt.Sprintf(
				"头字段与源图不一致: 头宽 %d %s 源宽 %d，头高 %d %s 源高 %d，头位深 %d %s 源位深 %d",
				hdr.Width, eq(hdr.Width == src.W), src.W,
				hdr.Height, eq(hdr.Height == src.H), src.H,
				hdr.BitDepth, eq(hdr.BitDepth == SampleBitDepth), SampleBitDepth))
		}
	}

	dec, derr := api.Decode(encoded, api.Options{Format: format})
	if derr != nil {
		r.Issues = append(r.Issues, "解码失败: "+derr.Error())
		r.DecodedPixels = 0
		r.DimsOK = false
		r.PixelsOK = false
	} else {
		r.Decoded = dec
		r.DecodedWidth = dec.W
		r.DecodedHeight = dec.H
		r.DecodedPixels = len(dec.Pix)
		r.DimsOK = dec.W == src.W && dec.H == src.H
		if !r.DimsOK {
			r.Issues = append(r.Issues, fmt.Sprintf(
				"解码尺寸与源图不一致: %d×%d != %d×%d", dec.W, dec.H, src.W, src.H))
		}
		r.PixelDiffs, r.MaxAbsDiff, r.FirstDiff = diffPixels(src.Pix, dec.Pix)
		r.PixelsOK = r.PixelDiffs == 0
		if !r.PixelsOK {
			r.Issues = append(r.Issues, fmt.Sprintf(
				"像素差异 %d 个（共 %d 个像素），最大绝对差 %d，首差偏移 %d",
				r.PixelDiffs, maxInt(len(src.Pix), len(dec.Pix)), r.MaxAbsDiff, r.FirstDiff))
		}
	}

	// 重编码比对：解码结果原样编码回去，必须与进入解码器的码流逐字节相同。
	// 这一项能抓住「像素没变但补齐字节/头被改坏」这类不一致。
	if r.Decoded != nil {
		re, rerr := api.Encode(r.Decoded, api.Options{Format: format})
		if rerr != nil {
			r.Issues = append(r.Issues, "重编码失败: "+rerr.Error())
		} else {
			r.ReencodedBytes = len(re)
			r.ByteDiffs, r.FirstByteDiff = diffBytes(r.Encoded, re)
			r.ByteOK = r.ByteDiffs == 0 && r.ReencodedBytes == r.EncodedBytes
			if !r.ByteOK {
				r.Issues = append(r.Issues, fmt.Sprintf(
					"重编码字节差异 %d 个（原 %d 字节，重编码 %d 字节），首差偏移 %d",
					r.ByteDiffs, r.EncodedBytes, r.ReencodedBytes, r.FirstByteDiff))
			}
		}
	}

	r.OK = r.HeaderOK && r.DimsOK && r.PixelsOK && r.ByteOK
	return r
}

// VerifyAll 依次校验三种格式，并把三份报告合成一次总判定。
func VerifyAll(src *api.Image) (*Suite, error) {
	reports := make([]Report, 0, len(Formats))
	for _, f := range Formats {
		r, err := Verify(src, f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		reports = append(reports, r)
	}
	return Combine(src, reports), nil
}

// Combine 把若干份报告合并成一次总判定。三种格式必须各出现且仅出现一次，
// 且每一份都通过，总判定才通过；缺一份、重复一份或任一不一致都判失败。
func Combine(src *api.Image, reports []Report) *Suite {
	s := &Suite{
		SourceWidth:    src.W,
		SourceHeight:   src.H,
		SourcePixels:   len(src.Pix),
		SourceBitDepth: SampleBitDepth,
		Expected:       len(Formats),
	}

	// 按固定格式顺序排列报告，保证渲染可复现。
	byFmt := make(map[string][]Report, len(reports))
	for _, r := range reports {
		known := false
		for _, f := range Formats {
			if r.Format == f {
				known = true
				break
			}
		}
		if !known {
			// 未知格式的报告不参与三格式判定，但点名出来，避免被静默吞掉。
			s.Issues = append(s.Issues, fmt.Sprintf("未知格式报告 %q", r.Format))
			continue
		}
		byFmt[r.Format] = append(byFmt[r.Format], r)
	}
	for _, f := range Formats {
		got := byFmt[f]
		if len(got) == 0 {
			s.Issues = append(s.Issues, "缺少 "+f+" 报告")
			continue
		}
		if len(got) > 1 {
			s.Issues = append(s.Issues, fmt.Sprintf("%s 报告重复 %d 份", f, len(got)))
		}
		// 重复时取第一份参与汇总。
		s.Reports = append(s.Reports, got[0])
	}

	for _, r := range s.Reports {
		if r.OK {
			s.Passed++
		}
		s.TotalEncodedBytes += maxInt(r.EncodedBytes, 0)
		s.TotalReencodedBytes += maxInt(r.ReencodedBytes, 0)
		s.TotalPixelDiffs += maxInt(r.PixelDiffs, 0)
		s.TotalByteDiffs += maxInt(r.ByteDiffs, 0)
	}

	// 三种格式各写各的头：除了各自与源图对齐，还要横向两两一致。
	// 以第一份解析成功的头为基准，其余逐项比对，差异以数字列出。
	var base *Header
	baseFormat := ""
	for i := range s.Reports {
		if !s.Reports[i].HeaderParsed {
			continue
		}
		if base == nil {
			base = &s.Reports[i].Header
			baseFormat = s.Reports[i].Format
			continue
		}
		h := s.Reports[i].Header
		if h.Width != base.Width || h.Height != base.Height || h.BitDepth != base.BitDepth {
			s.Issues = append(s.Issues, fmt.Sprintf(
				"%s 头与 %s 头不一致: 宽 %d/%d（差 %d），高 %d/%d（差 %d），位深 %d/%d（差 %d）",
				s.Reports[i].Format, baseFormat,
				h.Width, base.Width, h.Width-base.Width,
				h.Height, base.Height, h.Height-base.Height,
				h.BitDepth, base.BitDepth, h.BitDepth-base.BitDepth))
		}
	}

	// 同一张图经三种格式各转一圈，三份解码结果必须逐像素相同。这样落盘的
	// 两张图（源图与一份往返结果）才代表全部三种格式。
	var first *Report
	for i := range s.Reports {
		if s.Reports[i].Decoded == nil {
			continue
		}
		if first == nil {
			first = &s.Reports[i]
			continue
		}
		diffs, maxAbs, firstOff := diffPixels(first.Decoded.Pix, s.Reports[i].Decoded.Pix)
		s.TotalCrossDiffs += diffs
		if diffs != 0 {
			s.Issues = append(s.Issues, fmt.Sprintf(
				"%s 解码结果与 %s 解码结果不一致: 像素差异 %d，最大差 %d，首差偏移 %d",
				s.Reports[i].Format, first.Format, diffs, maxAbs, firstOff))
		}
	}

	s.OK = len(s.Issues) == 0 && s.Passed == s.Expected && len(s.Reports) == s.Expected
	return s
}

// ParseHeader 从码流本身解析头字段，不依赖 internal/codec 的解码路径。
func ParseHeader(format string, data []byte) (Header, error) {
	switch format {
	case "pgm":
		return parsePGMHeader(data)
	case "bmp":
		return parseBMPHeader(data)
	case "rle":
		return parseRLEHeader(data)
	default:
		return Header{}, fmt.Errorf("%w: %q", api.ErrUnknownFormat, format)
	}
}

func parsePGMHeader(data []byte) (Header, error) {
	if len(data) < 2 || data[0] != 'P' || data[1] != '5' {
		return Header{}, errors.New("不是 P5 流")
	}
	i := 2
	next := func() (int, bool) {
		for i < len(data) && (data[i] == ' ' || data[i] == '\n' || data[i] == '\r' || data[i] == '\t') {
			i++
		}
		start, val := i, 0
		for i < len(data) && data[i] >= '0' && data[i] <= '9' {
			val = val*10 + int(data[i]-'0')
			i++
		}
		return val, i > start
	}
	w, ok1 := next()
	h, ok2 := next()
	maxv, ok3 := next()
	if !ok1 || !ok2 || !ok3 || w <= 0 || h <= 0 || maxv <= 0 {
		return Header{}, errors.New("PGM 头非法")
	}
	depth := bits.Len(uint(maxv))
	bpp := 8
	if maxv > 255 {
		bpp = 16
	}
	return Header{Width: w, Height: h, BitDepth: depth, BitsPerPixel: bpp}, nil
}

func parseBMPHeader(data []byte) (Header, error) {
	if len(data) < 54 || data[0] != 'B' || data[1] != 'M' {
		return Header{}, errors.New("不是 BMP 流")
	}
	w := int(int32(binary.LittleEndian.Uint32(data[18:])))
	h := int(int32(binary.LittleEndian.Uint32(data[22:])))
	bpp := int(binary.LittleEndian.Uint16(data[28:]))
	if w <= 0 || h == 0 || bpp <= 0 {
		return Header{}, errors.New("BMP 头非法")
	}
	// 本仓库的 BMP 是 24 位三通道同值灰度：容器 24 bpp，灰度样本 8 位。
	depth := bpp
	if bpp%3 == 0 {
		depth = bpp / 3
	}
	return Header{Width: w, Height: h, BitDepth: depth, BitsPerPixel: bpp}, nil
}

func parseRLEHeader(data []byte) (Header, error) {
	if len(data) < 8 || string(data[0:4]) != "GTR1" {
		return Header{}, errors.New("不是 GTR1 流")
	}
	w := int(binary.LittleEndian.Uint16(data[4:]))
	h := int(binary.LittleEndian.Uint16(data[6:]))
	if w <= 0 || h <= 0 {
		return Header{}, errors.New("RLE 头非法")
	}
	// GTR1 的游程值恒为 1 字节，灰度样本固定 8 位，文件头不另存位深字段。
	return Header{Width: w, Height: h, BitDepth: 8, BitsPerPixel: 8}, nil
}

// DefaultSample 是报告使用的确定性样本：37×23 灰度图，851 像素。
//
// 宽度 37 使 BMP 每行 111 字节、补齐到 112，覆盖行补齐路径；
// 前 300 个像素取同值，使 RLE 触到单条游程 255 的上限；
// 其余像素按固定公式铺开，避免全图同值掩盖差异。
func DefaultSample() *api.Image {
	const w, h = 37, 23
	img := &api.Image{W: w, H: h, Pix: make([]uint8, w*h)}
	for i := range img.Pix {
		img.Pix[i] = uint8((i*37 + 11) % 256)
	}
	for i := 0; i < 300 && i < len(img.Pix); i++ {
		img.Pix[i] = 17
	}
	return img
}

// diffPixels 逐像素比较两个切片，缺位按最大差 255 计。
func diffPixels(want, got []uint8) (diffs, maxAbs, first int) {
	first = -1
	n := maxInt(len(want), len(got))
	for i := 0; i < n; i++ {
		if i >= len(want) || i >= len(got) {
			diffs++
			if maxAbs < 255 {
				maxAbs = 255
			}
			if first < 0 {
				first = i
			}
			continue
		}
		d := int(want[i]) - int(got[i])
		if d < 0 {
			d = -d
		}
		if d != 0 {
			diffs++
			if d > maxAbs {
				maxAbs = d
			}
			if first < 0 {
				first = i
			}
		}
	}
	return diffs, maxAbs, first
}

// diffBytes 逐字节比较，缺位计为差异。
func diffBytes(want, got []byte) (diffs, first int) {
	first = -1
	n := maxInt(len(want), len(got))
	for i := 0; i < n; i++ {
		if i >= len(want) || i >= len(got) || want[i] != got[i] {
			diffs++
			if first < 0 {
				first = i
			}
		}
	}
	return diffs, first
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func eq(ok bool) string {
	if ok {
		return "="
	}
	return "!="
}

// RenderFormat 渲染单份报告（一个格式一节）。
func RenderFormat(w io.Writer, r Report) {
	fmt.Fprintf(w, "== %s ==\n", r.Format)
	if r.HeaderParsed {
		fmt.Fprintf(w, "头字段: 宽 %d  高 %d  位深 %d（容器每像素 %d 位）\n",
			r.Header.Width, r.Header.Height, r.Header.BitDepth, r.Header.BitsPerPixel)
	} else {
		fmt.Fprintln(w, "头字段: 解析失败")
	}
	fmt.Fprintf(w, "源图像: 宽 %d  高 %d  位深 %d\n", r.SourceWidth, r.SourceHeight, r.SourceBitDepth)
	if r.Decoded != nil {
		fmt.Fprintf(w, "解码后: 宽 %d  高 %d  像素 %d\n", r.DecodedWidth, r.DecodedHeight, r.DecodedPixels)
	} else {
		fmt.Fprintln(w, "解码后: 失败")
	}
	fmt.Fprintf(w, "字节数: %d  重编码字节数: %s\n",
		r.EncodedBytes, countOrDash(r.ReencodedBytes))
	fmt.Fprintf(w, "差异数: 像素差异 %d  字节差异 %s  最大差 %d  首差偏移 %s\n",
		r.PixelDiffs, countOrDash(r.ByteDiffs), r.MaxAbsDiff, offsetOrDash(r.FirstDiff))
	fmt.Fprintf(w, "判定: %s\n", verdict(r.OK))
	for _, is := range r.Issues {
		fmt.Fprintf(w, "  问题: %s\n", is)
	}
}

// RenderSuite 渲染三份报告与总判定。
func RenderSuite(w io.Writer, s *Suite) {
	fmt.Fprintf(w, "往返报告  源图像 %d×%d（%d 像素，%d 位灰度）\n",
		s.SourceWidth, s.SourceHeight, s.SourcePixels, s.SourceBitDepth)
	fmt.Fprintln(w, "按格式分节，字节数与差异数并列；三份报告合并为一次总判定，任一份不一致即失败。")
	fmt.Fprintln(w)
	for i, r := range s.Reports {
		RenderFormat(w, r)
		if i < len(s.Reports)-1 {
			fmt.Fprintln(w)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "== 总判定 ==")
	parts := make([]string, 0, len(s.Reports))
	for _, r := range s.Reports {
		parts = append(parts, fmt.Sprintf("%s %s", r.Format, verdict(r.OK)))
	}
	fmt.Fprintf(w, "三份报告: %s\n", strings.Join(parts, " / "))
	fmt.Fprintf(w, "头字段: %s\n", headerLine(s.Reports))
	fmt.Fprintf(w, "汇总: 字节数 %d  重编码字节数 %d  像素差异 %d  跨格式像素差异 %d  字节差异 %d\n",
		s.TotalEncodedBytes, s.TotalReencodedBytes, s.TotalPixelDiffs, s.TotalCrossDiffs, s.TotalByteDiffs)
	for _, is := range s.Issues {
		fmt.Fprintf(w, "  问题: %s\n", is)
	}
	if s.OK {
		fmt.Fprintf(w, "总判定: 通过（%d/%d）\n", s.Passed, s.Expected)
	} else {
		fmt.Fprintf(w, "总判定: 失败（%d/%d）\n", s.Passed, s.Expected)
	}
}

func verdict(ok bool) string {
	if ok {
		return "通过"
	}
	return "失败"
}

// headerLine 把三份头的宽、高、位深并排成一行，便于肉眼核对横向一致。
func headerLine(reports []Report) string {
	parts := make([]string, 0, len(reports))
	for _, r := range reports {
		if !r.HeaderParsed {
			parts = append(parts, r.Format+" 解析失败")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %d×%d×%d", r.Format, r.Header.Width, r.Header.Height, r.Header.BitDepth))
	}
	return strings.Join(parts, " | ")
}

func countOrDash(n int) string {
	if n < 0 {
		return "-"
	}
	return fmt.Sprintf("%d", n)
}

func offsetOrDash(n int) string {
	if n < 0 {
		return "-"
	}
	return fmt.Sprintf("%d", n)
}

// WriteArtifacts 把三份单格式报告（pgm.txt / bmp.txt / rle.txt）、合并报告
// （report.txt）以及往返前后的两张图（source.pgm / roundtrip.pgm）写到 dir。
//
// 两张图是同一张图往返前后的完整字节流：校验通过时逐字节相同，差异数在
// report.txt 中以数字给出（像素差异、最大差、首差偏移）。
func WriteArtifacts(dir string, src *api.Image, s *Suite) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, r := range s.Reports {
		var b strings.Builder
		RenderFormat(&b, r)
		if err := os.WriteFile(filepath.Join(dir, r.Format+".txt"), []byte(b.String()), 0o644); err != nil {
			return err
		}
	}

	var combined strings.Builder
	RenderSuite(&combined, s)
	if err := os.WriteFile(filepath.Join(dir, "report.txt"), []byte(combined.String()), 0o644); err != nil {
		return err
	}

	srcBody, err := api.Encode(src, api.Options{Format: "pgm"})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "source.pgm"), srcBody, 0o644); err != nil {
		return err
	}

	// 三格式解码结果在通过时两两相同；取第一份有解码结果的报告落盘。
	for _, r := range s.Reports {
		if r.Decoded == nil {
			continue
		}
		body, err := api.Encode(r.Decoded, api.Options{Format: "pgm"})
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "roundtrip.pgm"), body, 0o644); err != nil {
			return err
		}
		break
	}
	return nil
}

// EqualImages 报告两张图的宽高与像素是否完全相同，差异量由 diffPixels 给出。
// 它用于在报告之外单独核对落盘的两张图。
func EqualImages(a, b *api.Image) (equal bool, diffs, maxAbs, first int) {
	if a == nil || b == nil {
		return false, maxInt(lenPix(a), lenPix(b)), 255, 0
	}
	diffs, maxAbs, first = diffPixels(a.Pix, b.Pix)
	equal = a.W == b.W && a.H == b.H && diffs == 0
	return equal, diffs, maxAbs, first
}

func lenPix(img *api.Image) int {
	if img == nil {
		return 0
	}
	return len(img.Pix)
}
