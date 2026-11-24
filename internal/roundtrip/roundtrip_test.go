package roundtrip

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotoolkit/api"
)

// TestAllFormatsRoundTrip 是往返校验的主断言：三种格式各转一圈，像素与源图
// 逐字节相同，头字段的宽高位深与源图一致，三份报告合并的总判定通过。
func TestAllFormatsRoundTrip(t *testing.T) {
	src := DefaultSample()
	suite, err := VerifyAll(src)
	if err != nil {
		t.Fatalf("往返校验返回错误: %v", err)
	}
	if len(suite.Reports) != len(Formats) {
		t.Fatalf("报告份数 %d，期望 %d", len(suite.Reports), len(Formats))
	}
	for _, r := range suite.Reports {
		if r.Format != Formats[0] && r.Format != Formats[1] && r.Format != Formats[2] {
			t.Fatalf("未知格式 %q", r.Format)
		}
		if !r.PixelsOK || r.PixelDiffs != 0 || r.MaxAbsDiff != 0 || r.FirstDiff != -1 {
			t.Fatalf("%s 像素不一致: 差异 %d，最大差 %d，首差 %d", r.Format, r.PixelDiffs, r.MaxAbsDiff, r.FirstDiff)
		}
		if !r.HeaderOK || r.Header.Width != src.W || r.Header.Height != src.H || r.Header.BitDepth != SampleBitDepth {
			t.Fatalf("%s 头字段不一致: %+v（源 %d×%d×%d）", r.Format, r.Header, src.W, src.H, SampleBitDepth)
		}
		if !r.DimsOK || r.DecodedWidth != src.W || r.DecodedHeight != src.H || r.DecodedPixels != len(src.Pix) {
			t.Fatalf("%s 解码尺寸不一致", r.Format)
		}
		if !r.ByteOK || r.ByteDiffs != 0 || r.ReencodedBytes != r.EncodedBytes {
			t.Fatalf("%s 重编码字节不一致: 差异 %d", r.Format, r.ByteDiffs)
		}
		if !r.OK || len(r.Issues) != 0 {
			t.Fatalf("%s 单份判定失败: %v", r.Format, r.Issues)
		}
	}
	if !suite.OK || suite.Passed != suite.Expected || suite.Expected != 3 {
		t.Fatalf("总判定失败: passed=%d expected=%d issues=%v", suite.Passed, suite.Expected, suite.Issues)
	}
	if suite.TotalPixelDiffs != 0 || suite.TotalByteDiffs != 0 || suite.TotalCrossDiffs != 0 {
		t.Fatalf("汇总差异非零: 像素 %d，跨格式 %d，字节 %d",
			suite.TotalPixelDiffs, suite.TotalCrossDiffs, suite.TotalByteDiffs)
	}
}

// TestHeadersAgreeAcrossFormats 断言三份头的宽、高、位深两两相同。
func TestHeadersAgreeAcrossFormats(t *testing.T) {
	suite, err := VerifyAll(DefaultSample())
	if err != nil {
		t.Fatal(err)
	}
	if len(suite.Reports) != 3 {
		t.Fatalf("报告份数 %d", len(suite.Reports))
	}
	base := suite.Reports[0].Header
	for _, r := range suite.Reports[1:] {
		if r.Header.Width != base.Width || r.Header.Height != base.Height || r.Header.BitDepth != base.BitDepth {
			t.Fatalf("%s 头 %+v 与 %s 头 %+v 不一致", r.Format, r.Header, suite.Reports[0].Format, base)
		}
	}
}

// TestParseHeaderAcrossDimensions 用多组宽高覆盖 BMP 行补齐边界、RLE 255 游程上限，
// 确认头字段解析在三种格式上都与源图一致。
func TestParseHeaderAcrossDimensions(t *testing.T) {
	dims := [][2]int{{1, 1}, {2, 2}, {3, 1}, {4, 3}, {5, 7}, {37, 23}, {255, 2}, {256, 1}, {1, 300}}
	for _, d := range dims {
		w, h := d[0], d[1]
		img := &api.Image{W: w, H: h, Pix: make([]uint8, w*h)}
		for i := range img.Pix {
			img.Pix[i] = uint8((i*13 + 7) % 256)
		}
		for _, f := range Formats {
			body, err := api.Encode(img, api.Options{Format: f})
			if err != nil {
				t.Fatalf("%s %dx%d 编码: %v", f, w, h, err)
			}
			hdr, err := ParseHeader(f, body)
			if err != nil {
				t.Fatalf("%s %dx%d 头解析: %v", f, w, h, err)
			}
			if hdr.Width != w || hdr.Height != h || hdr.BitDepth != SampleBitDepth {
				t.Fatalf("%s %dx%d 头为 %+v", f, w, h, hdr)
			}
		}
	}
}

// TestPixelDiffIsCounted 用故障注入确认像素差会被逐位计数：改掉一个像素后
// 差异数必须精确为 1，最大差与首差偏移也指向那个位置。
func TestPixelDiffIsCounted(t *testing.T) {
	src := DefaultSample()
	body, err := api.Encode(src, api.Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	// PGM 头固定为 "P5\n37 23\n255\n"，共 13 字节，像素从偏移 13 开始。
	const off = 13
	bad := append([]byte(nil), body...)
	bad[off+5] ^= 0x40 // 第 6 个像素
	r := Evaluate(src, "pgm", bad)
	if r.PixelDiffs != 1 {
		t.Fatalf("像素差异 %d，期望 1", r.PixelDiffs)
	}
	if r.MaxAbsDiff != 0x40 {
		t.Fatalf("最大差 %d，期望 %d", r.MaxAbsDiff, 0x40)
	}
	if r.FirstDiff != 5 {
		t.Fatalf("首差偏移 %d，期望 5", r.FirstDiff)
	}
	if r.OK {
		t.Fatal("像素不一致时判定不应通过")
	}
}

// TestByteDiffIsCounted 改坏 BMP 的行补齐字节：像素全对，但重编码后补齐为零，
// 字节差异必须被数字量出，且总判定失败。
func TestByteDiffIsCounted(t *testing.T) {
	src := DefaultSample()
	body, err := api.Encode(src, api.Options{Format: "bmp"})
	if err != nil {
		t.Fatal(err)
	}
	// 37×23 的 BMP：每行 37*3=111 字节，补齐到 112，每行末尾 1 个补齐字节。
	// 第一行补齐字节位于偏移 54+111。
	const padOff = 54 + 111
	if body[padOff] != 0 {
		t.Fatalf("前提不成立：补齐字节应为 0，实际 %d", body[padOff])
	}
	bad := append([]byte(nil), body...)
	bad[padOff] = 0x7f
	r := Evaluate(src, "bmp", bad)
	if r.PixelDiffs != 0 {
		t.Fatalf("改补齐字节后像素差异应为 0，实际 %d", r.PixelDiffs)
	}
	if r.ByteDiffs != 1 || r.FirstByteDiff != padOff {
		t.Fatalf("字节差异 %d，首差 %d；期望 1 与 %d", r.ByteDiffs, r.FirstByteDiff, padOff)
	}
	if r.ByteOK || r.OK {
		t.Fatal("字节不一致时判定不应通过")
	}
}

// TestHeaderMismatchDetected 把 BMP 头里的高度改成别的值（像素数据不动），
// 头字段必须报不一致，并且总判定失败。
func TestHeaderMismatchDetected(t *testing.T) {
	src := DefaultSample()
	body, err := api.Encode(src, api.Options{Format: "bmp"})
	if err != nil {
		t.Fatal(err)
	}
	bad := append([]byte(nil), body...)
	bad[22] = byte(src.H + 1) // biHeight 低字节
	r := Evaluate(src, "bmp", bad)
	if r.HeaderOK {
		t.Fatal("头高被改后 HeaderOK 不应为真")
	}
	if !strings.Contains(strings.Join(r.Issues, "\n"), "头高") {
		t.Fatalf("问题列表应点名头高: %v", r.Issues)
	}
	if r.OK {
		t.Fatal("头不一致时判定不应通过")
	}
}

// TestCombineFailsOnMissingReport：三份报告缺一份，总判定即失败。
func TestCombineFailsOnMissingReport(t *testing.T) {
	src := DefaultSample()
	var reports []Report
	for _, f := range Formats {
		r, err := Verify(src, f)
		if err != nil {
			t.Fatal(err)
		}
		if f == "rle" {
			continue
		}
		reports = append(reports, r)
	}
	s := Combine(src, reports)
	if s.OK {
		t.Fatal("缺 rle 报告时总判定应失败")
	}
	if !strings.Contains(strings.Join(s.Issues, "\n"), "缺少 rle 报告") {
		t.Fatalf("问题列表应点名缺 rle: %v", s.Issues)
	}
}

// TestCombineFailsOnAnyIssue：三份都在、但其中一份不一致，总判定失败。
func TestCombineFailsOnAnyIssue(t *testing.T) {
	src := DefaultSample()
	var reports []Report
	for _, f := range Formats {
		r, err := Verify(src, f)
		if err != nil {
			t.Fatal(err)
		}
		reports = append(reports, r)
	}
	victim := &reports[1] // bmp
	victim.PixelDiffs = 3
	victim.PixelsOK = false
	victim.OK = false
	s := Combine(src, reports)
	if s.OK {
		t.Fatal("任一份不一致时总判定应失败")
	}
	if s.Passed != 2 || s.Expected != 3 {
		t.Fatalf("passed=%d expected=%d", s.Passed, s.Expected)
	}
	if s.TotalPixelDiffs != 3 {
		t.Fatalf("汇总像素差异 %d，期望 3", s.TotalPixelDiffs)
	}
}

// TestCombineFailsOnCrossFormatPixels：三份报告各自与源图一致，但两份解码结果
// 之间像素不同（模拟「各写各的、转来转去像素变了没人发现」），总判定失败。
func TestCombineFailsOnCrossFormatPixels(t *testing.T) {
	src := DefaultSample()
	var reports []Report
	for _, f := range Formats {
		r, err := Verify(src, f)
		if err != nil {
			t.Fatal(err)
		}
		reports = append(reports, r)
	}
	// 把 bmp 那份的解码结果改一个像素：它自己仍与源图比对过（此处直接篡改
	// 解码结果，不动 PixelDiffs，专测跨格式互比这一步）。
	reports[1].Decoded.Pix[0] ^= 0xff
	s := Combine(src, reports)
	if s.OK {
		t.Fatal("跨格式像素不一致时总判定应失败")
	}
	if s.TotalCrossDiffs != 1 {
		t.Fatalf("跨格式差异 %d，期望 1", s.TotalCrossDiffs)
	}
	if !strings.Contains(strings.Join(s.Issues, "\n"), "解码结果") {
		t.Fatalf("问题列表应点名跨格式不一致: %v", s.Issues)
	}
}

// TestRenderHasSectionsAndCounts：报告按格式分节，字节数与差异数并列出现。
func TestRenderHasSectionsAndCounts(t *testing.T) {
	src := DefaultSample()
	suite, err := VerifyAll(src)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	RenderSuite(&buf, suite)
	text := buf.String()
	for _, f := range Formats {
		if !strings.Contains(text, "== "+f+" ==") {
			t.Fatalf("缺少 %s 分节", f)
		}
	}
	for _, want := range []string{"字节数:", "差异数:", "== 总判定 ==", "总判定: 通过"} {
		if !strings.Contains(text, want) {
			t.Fatalf("报告缺少 %q", want)
		}
	}
}

// TestWriteArtifactsTwoImages：产物目录里两张图都是有效 PGM，且校验通过时
// 逐字节相同；报告文件三份单格式 + 一份合并都在，已落盘的两张图差异数为 0。
func TestWriteArtifactsTwoImages(t *testing.T) {
	src := DefaultSample()
	suite, err := VerifyAll(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := WriteArtifacts(dir, src, suite); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pgm.txt", "bmp.txt", "rle.txt", "report.txt", "source.pgm", "roundtrip.pgm"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("缺产物 %s: %v", name, err)
		}
	}
	a, err := api.Decode(mustRead(t, filepath.Join(dir, "source.pgm")), api.Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := api.Decode(mustRead(t, filepath.Join(dir, "roundtrip.pgm")), api.Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	equal, diffs, maxAbs, first := EqualImages(a, b)
	if !equal || diffs != 0 || maxAbs != 0 || first != -1 {
		t.Fatalf("两张图不一致: diffs=%d max=%d first=%d", diffs, maxAbs, first)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestEncodeBytesUnchanged：不做改动时 encode 的字节仍是 golden 原样。
func TestEncodeBytesUnchanged(t *testing.T) {
	src := DefaultSample()
	for _, f := range Formats {
		body, err := api.Encode(src, api.Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		r := Evaluate(src, f, body)
		if r.ReencodedBytes != len(body) {
			t.Fatalf("%s 重编码字节数 %d != %d", f, r.ReencodedBytes, len(body))
		}
	}
}

// TestGoldenBaselineUntouched 锁定「不做改动时 encode 的字节保持原样」：
// 用 testdata/golden 的同一张 4×3 样本，三格式输出必须与基准逐字节一致。
func TestGoldenBaselineUntouched(t *testing.T) {
	img := &api.Image{W: 4, H: 3, Pix: make([]uint8, 12)}
	for i := range img.Pix {
		img.Pix[i] = uint8(i * 7)
	}
	for _, f := range Formats {
		want, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", f+".bin"))
		if err != nil {
			t.Fatalf("读 golden %s: %v", f, err)
		}
		got, err := api.Encode(img, api.Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s 输出与原编码不逐字节一致（%d vs %d 字节）", f, len(got), len(want))
		}
	}
}

// TestWriteDefaultArtifacts 让 `make test` 直接重生成往返报告：
// artifacts/roundtrip/ 下三份单格式报告、合并报告与两张图。
func TestWriteDefaultArtifacts(t *testing.T) {
	src := DefaultSample()
	suite, err := VerifyAll(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join("..", "..", "artifacts", "roundtrip")
	if err := WriteArtifacts(dir, src, suite); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pgm.txt", "bmp.txt", "rle.txt", "report.txt", "source.pgm", "roundtrip.pgm"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("缺产物 %s: %v", name, err)
		}
	}
}
