package api

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

var allCombos = []Options{
	{RowOrder: "down", ByteOrder: "le", RowAlign: 1},
	{RowOrder: "down", ByteOrder: "le", RowAlign: 4},
	{RowOrder: "down", ByteOrder: "be", RowAlign: 1},
	{RowOrder: "down", ByteOrder: "be", RowAlign: 4},
	{RowOrder: "up", ByteOrder: "le", RowAlign: 1},
	{RowOrder: "up", ByteOrder: "le", RowAlign: 4},
	{RowOrder: "up", ByteOrder: "be", RowAlign: 1},
	{RowOrder: "up", ByteOrder: "be", RowAlign: 4},
}

// tallSample 行数、列数都大于 1，且行宽不是 4 的倍数，
// 这样三个标志在 pgm/bmp/rle 上都可观察（除 pgm 的字节序本身无作用对象）。
func tallSample() *Image {
	img := &Image{W: 5, H: 5, Pix: make([]uint8, 25)}
	for i := range img.Pix {
		img.Pix[i] = uint8(10 + i*4)
	}
	return img
}

func TestEightCombosRoundTrip(t *testing.T) {
	img := tallSample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		for i, c := range allCombos {
			c.Format = f
			body, err := Encode(img, c)
			if err != nil {
				t.Fatalf("%s C%d 编码: %v", f, i, err)
			}
			got, err := Decode(body, c)
			if err != nil {
				t.Fatalf("%s C%d 解码: %v", f, i, err)
			}
			if got.W != img.W || got.H != img.H || !bytes.Equal(got.Pix, img.Pix) {
				t.Fatalf("%s C%d 像素不一致", f, i)
			}
		}
	}
}

func TestZeroOptionsMatchesLegacyGoldens(t *testing.T) {
	img := sample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		want, err := os.ReadFile(filepath.Join("..", "testdata", "golden", f+".bin"))
		if err != nil {
			t.Skipf("缺 golden %s", f)
		}
		got, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s 零值 Options 与原基准不逐字节一致", f)
		}
	}
}

func TestDefaultOptionsMatchesLegacyGoldens(t *testing.T) {
	img := sample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		want, err := os.ReadFile(filepath.Join("..", "testdata", "golden", f+".bin"))
		if err != nil {
			t.Skipf("缺 golden %s", f)
		}
		got, err := Encode(img, DefaultOptions().withFormat(f))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s DefaultOptions 与原基准不逐字节一致", f)
		}
	}
}

func (o Options) withFormat(f string) Options { o.Format = f; return o }

// TestFlagsReallyChangeBytes 证明每个标志在声称生效的格式里确实改变字节；
// 不生效的（pgm 的 ByteOrder）必须在 Describe 里被标出来，而不是悄悄忽略。
func TestFlagsReallyChangeBytes(t *testing.T) {
	img := tallSample()
	base := Options{Format: "pgm", RowOrder: "down", ByteOrder: "le", RowAlign: 1}

	// 行序：pgm/bmp/rle 都应改变字节。
	for _, f := range []string{"pgm", "bmp", "rle"} {
		a := base
		a.Format = f
		b := a
		b.RowOrder = "up"
		bottomRow := uint8(10 + 4*(img.W*(img.H-1))) // 逻辑末行首像素
		if !bytes.Contains(mustEncode(t, img, a), []byte{bottomRow}) {
			t.Fatalf("%s 基线里找不到末行首像素", f)
		}
		// 行序翻转后，存储流第一行应是逻辑末行：第一个有效像素字节随之改变。
		ab := mustEncode(t, img, a)
		bb := mustEncode(t, img, b)
		if bytes.Equal(ab, bb) {
			t.Fatalf("%s: RowOrder 旗标未改变字节（行序未生效）", f)
		}
		if firstPixelByte(t, f, bb) != bottomRow {
			t.Fatalf("%s: RowOrder=up 时首像素不是逻辑末行（行序未生效）", f)
		}
	}

	// 字节序：bmp/rle 改头字节；pgm 头是 ASCII，必须被标为不生效。
	for _, f := range []string{"bmp", "rle"} {
		a := base
		a.Format = f
		b := a
		b.ByteOrder = "be"
		if bytes.Equal(mustEncode(t, img, a), mustEncode(t, img, b)) {
			t.Fatalf("%s: ByteOrder 旗标未改变字节（字节序未生效）", f)
		}
	}
	d, err := Describe("pgm", img.W, img.H, img.Pix, Options{Format: "pgm", ByteOrder: "be"})
	if err != nil {
		t.Fatal(err)
	}
	if d.ByteOrderEffective {
		t.Fatal("pgm 的 ByteOrder 不应被标为生效")
	}

	// 行对齐：三格式都应改变总长度。
	for _, f := range []string{"pgm", "bmp", "rle"} {
		a := base
		a.Format = f
		b := a
		b.RowAlign = 4
		if len(mustEncode(t, img, a)) == len(mustEncode(t, img, b)) {
			t.Fatalf("%s: RowAlign 未改变长度（行对齐未生效）", f)
		}
	}
}

// TestAlignInertWhenRowAlreadyAligned 记录“行宽已是 4 的倍数”时
// 行对齐确实没有作用对象，Describe 必须如实标注。
func TestAlignInertWhenRowAlreadyAligned(t *testing.T) {
	img := &Image{W: 4, H: 3, Pix: make([]uint8, 12)}
	for i := range img.Pix {
		img.Pix[i] = uint8(i * 7)
	}
	for _, f := range []string{"pgm", "rle"} {
		d, err := Describe(f, img.W, img.H, img.Pix, Options{Format: f, RowAlign: 4})
		if err != nil {
			t.Fatal(err)
		}
		if d.AlignEffective {
			t.Fatalf("%s: 行宽 4 字节时 RowAlign 应标为不生效", f)
		}
	}
	d, _ := Describe("bmp", img.W, img.H, img.Pix, Options{Format: "bmp", RowAlign: 4})
	if d.AlignEffective {
		t.Fatal("bmp 行宽 12 字节时 RowAlign 应标为不生效")
	}
}

func TestSamplesBothRowOrdersSameImage(t *testing.T) {
	type sc struct {
		name, ext, format string
		altOrder          string
	}
	for _, s := range []sc{
		{"sample1", "pgm", "pgm", "up"},
		{"sample2", "bmp", "bmp", "down"},
		{"sample3", "gtr", "rle", "up"},
	} {
		dir := filepath.Join("..", "testdata", "samples")
		src := mustLoadPGM(t, filepath.Join(dir, s.name+".src.pgm"))
		legacy := mustLoad(t, filepath.Join(dir, s.name+"."+s.ext), s.format, Options{Format: s.format})
		if !bytes.Equal(legacy.Pix, src.Pix) {
			t.Fatalf("%s: 旧文件与逻辑基准不同图", s.name)
		}
		alt := mustLoad(t, filepath.Join(dir, s.name+".alt."+s.ext), s.format,
			Options{Format: s.format, RowOrder: s.altOrder})
		if !bytes.Equal(alt.Pix, src.Pix) {
			t.Fatalf("%s: 另一种行序的文件应解出同一张图", s.name)
		}
	}
}

func mustEncode(t *testing.T, img *Image, o Options) []byte {
	t.Helper()
	b, err := Encode(img, o)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustLoad(t *testing.T, path, format string, o Options) *Image {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := Decode(raw, o)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return img
}

func mustLoadPGM(t *testing.T, path string) *Image {
	return mustLoad(t, path, "pgm", Options{Format: "pgm"})
}

// firstPixelByte 取存储流里第一个有效像素字节的偏移：
// pgm 跳过 ASCII 头，bmp 跳过 54 字节头，rle 解压后从 0 起。
func firstPixelByte(t *testing.T, format string, body []byte) uint8 {
	t.Helper()
	switch format {
	case "pgm":
		i := bytes.IndexByte(body, '\n') // P5
		for n := 0; n < 2; n++ {
			i = bytes.IndexByte(body[i+1:], '\n') + i + 1
		}
		return body[i+1]
	case "bmp":
		return body[54]
	default:
		// GTR1 头 8 字节；首个游程 (count,value) 的 value 就是首像素。
		return body[9]
	}
}

func TestDescribeRowOrderMap(t *testing.T) {
	img := tallSample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		down, _ := Describe(f, img.W, img.H, img.Pix, Options{Format: f, RowOrder: "down"})
		up, _ := Describe(f, img.W, img.H, img.Pix, Options{Format: f, RowOrder: "up"})
		for i := range down.Rows {
			if down.Rows[i].LogicalRow != i {
				t.Fatalf("%s down 槽 %d 应对应逻辑行 %d", f, i, i)
			}
			if up.Rows[i].LogicalRow != img.H-1-i {
				t.Fatalf("%s up 槽 %d 应对应逻辑行 %d", f, i, img.H-1-i)
			}
		}
	}
}

func TestDescribeAlignOffsets(t *testing.T) {
	img := tallSample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		a1, _ := Describe(f, img.W, img.H, img.Pix, Options{Format: f, RowAlign: 1})
		a4, _ := Describe(f, img.W, img.H, img.Pix, Options{Format: f, RowAlign: 4})
		if a1.Stride == a4.Stride {
			t.Fatalf("%s 两种对齐的跨度应不同（本例行宽非 4 倍数）", f)
		}
		// 行对齐保证“像素区内”每行起点是 4 的倍数；文件偏移含头长度，故减去头再取模。
		for _, r := range a4.Rows {
			if (r.Offset-a4.HeaderLen)%4 != 0 {
				t.Fatalf("%s align=4 的行 %d 文件偏移 %d 减头后不是 4 的倍数", f, r.StoredRow, r.Offset)
			}
			if r.PadLen <= 0 {
				t.Fatalf("%s align=4 行 %d 应有补位", f, r.StoredRow)
			}
		}
	}
}

func TestRejectsBadLayout(t *testing.T) {
	img := tallSample()
	for _, o := range []Options{
		{Format: "pgm", RowOrder: "sideways"},
		{Format: "pgm", ByteOrder: "middle"},
		{Format: "pgm", RowAlign: 2},
	} {
		if _, err := Encode(img, o); err == nil {
			t.Fatalf("%+v 应报错", o)
		}
	}
}

// TestAllCombosSameImage 检查 24 个结果（3 格式 × 8 组合）逐像素一致。
func TestAllCombosSameImage(t *testing.T) {
	img := tallSample()
	for i, c := range allCombos {
		var ref []uint8
		for _, f := range []string{"pgm", "bmp", "rle"} {
			c.Format = f
			got := mustDecode(t, mustEncode(t, img, c), c)
			if !bytes.Equal(got.Pix, img.Pix) {
				t.Fatalf("%s C%d 像素与源不同", f, i)
			}
			if ref == nil {
				ref = got.Pix
			} else if !bytes.Equal(ref, got.Pix) {
				t.Fatalf("%s C%d 与同组合其他格式不同", f, i)
			}
		}
	}
}

func mustDecode(t *testing.T, body []byte, o Options) *Image {
	t.Helper()
	img, err := Decode(body, o)
	if err != nil {
		t.Fatalf("%+v: %v", o, err)
	}
	return img
}

func ExampleDescribe() {
	d, _ := Describe("bmp", 3, 2, []uint8{1, 2, 3, 4, 5, 6}, Options{Format: "bmp", RowOrder: "up", RowAlign: 4})
	for _, r := range d.Rows {
		fmt.Printf("slot %d <- row %d @%d\n", r.StoredRow, r.LogicalRow, r.Offset)
	}
	// Output:
	// slot 0 <- row 1 @54
	// slot 1 <- row 0 @66
}
