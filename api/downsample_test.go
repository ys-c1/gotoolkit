package api

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// pointSample 是测试参照：块内左上角点采样（nearest），仅用于和面积平均做对比，
// 不属于产品代码。
func pointSample(img *Image, d int) *Image {
	ow, oh := ceilDiv(img.W, d), ceilDiv(img.H, d)
	out := &Image{W: ow, H: oh, Pix: make([]uint8, ow*oh)}
	for oy := 0; oy < oh; oy++ {
		for ox := 0; ox < ow; ox++ {
			out.Pix[oy*ow+ox] = img.Pix[oy*d*img.W+ox*d]
		}
	}
	return out
}

// TestDivisorOneByteIdentical：除数为一时，三种格式输出与 golden 基准逐字节相同，
// Decode 路径同样保持原画幅原像素。
func TestDivisorOneByteIdentical(t *testing.T) {
	img := sample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		want, err := os.ReadFile(filepath.Join("..", "testdata", "golden", f+".bin"))
		if err != nil {
			t.Skipf("缺 golden %s", f)
		}
		got, err := Encode(img, Options{Format: f, Divisor: 1})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: Divisor=1 输出与 golden 不逐字节相同", f)
		}
		dec, err := Decode(got, Options{Format: f, Divisor: 1})
		if err != nil {
			t.Fatal(err)
		}
		if dec.W != img.W || dec.H != img.H || !bytes.Equal(dec.Pix, img.Pix) {
			t.Fatalf("%s: Divisor=1 解码未保持原图", f)
		}
	}
}

// TestAreaAvgHandComputed 用手算值核对面积平均与边缘残缺块语义。
func TestAreaAvgHandComputed(t *testing.T) {
	// 4x3，d=2：整块 2x2 两个，右边缘是 1x2 残缺块（列 x=3）。
	// 像素布局：
	//	 0  10  20 | 30
	//	40  50  60 | 70
	//	80  90 100 |110
	img := &Image{W: 4, H: 3, Pix: []uint8{
		0, 10, 20, 30,
		40, 50, 60, 70,
		80, 90, 100, 110,
	}}
	got := downsampleAreaAvg(img, 2)
	if got.W != 2 || got.H != 2 {
		t.Fatalf("输出画幅应为 ceil(4/2)xceil(3/2)=2x2，实际 %dx%d", got.W, got.H)
	}
	// (0,0): (0+10+40+50)/4 = 25          —— 完整 2x2 块
	// (1,0): (20+30+60+70)/4 = 45         —— 完整 2x2 块
	// (0,1): (80+90)/2 = 85               —— 下边缘 2x1 残缺块，除数取实际像素数 2
	// (1,1): (100+110)/2 = 105            —— 下边缘 2x1 残缺块
	want := []uint8{25, 45, 85, 105}
	if !bytes.Equal(got.Pix, want) {
		t.Fatalf("面积平均值错误: got %v want %v", got.Pix, want)
	}
}

// TestAreaAvgEdgeBlocks 用 3x3、d=2 同时覆盖列残块、行残块与角上 1x1 残块，
// 验证边缘块权重：块内每个实际像素等权 1/n，不补零不复制。
func TestAreaAvgEdgeBlocks(t *testing.T) {
	// 布局：
	//	 0  10 | 20
	//	40  50 | 60
	//	----- + --
	//	80  90 |100
	img := &Image{W: 3, H: 3, Pix: []uint8{
		0, 10, 20,
		40, 50, 60,
		80, 90, 100,
	}}
	got := downsampleAreaAvg(img, 2) // ceil(3/2)=2，输出 2x2
	want := []uint8{
		25,  // (0,0): 完整 2x2，(0+10+40+50)/4
		40,  // (1,0): 列残 1x2，(20+60)/2 —— 若错误补零成 1x2 四格会得 20
		85,  // (0,1): 行残 2x1，(80+90)/2
		100, // (1,1): 角上 1x1，仅像素 100，输出自身
	}
	if !bytes.Equal(got.Pix, want) {
		t.Fatalf("边缘残缺块错误: got %v want %v", got.Pix, want)
	}
}

// TestRoundingHalfUp：平均出现 .5 时四舍五入（向上）。
func TestRoundingHalfUp(t *testing.T) {
	// 1x2 -> 1x1：(1+2)/2 = 1.5 -> 2；(2+3)/2 = 2.5 -> 3。
	a := downsampleAreaAvg(&Image{W: 1, H: 2, Pix: []uint8{1, 2}}, 2)
	if a.Pix[0] != 2 {
		t.Fatalf("1.5 应向上取整为 2，实际 %d", a.Pix[0])
	}
	b := downsampleAreaAvg(&Image{W: 1, H: 2, Pix: []uint8{2, 3}}, 2)
	if b.Pix[0] != 3 {
		t.Fatalf("2.5 应向上取整为 3，实际 %d", b.Pix[0])
	}
	// 整除时不引入偏差：(0+60)/2 = 30。
	c := downsampleAreaAvg(&Image{W: 1, H: 2, Pix: []uint8{0, 60}}, 2)
	if c.Pix[0] != 30 {
		t.Fatalf("整除应得 30，实际 %d", c.Pix[0])
	}
}

// TestPointSampleDifferenceD2：第一档除数 d=2，给出面积平均相对点采样的差异下界。
// 取 2x2 图 [0 0; 200 200]：点采样取左上角得 0，面积平均得 100，
// 因此 |area - point| = 100，下界 100（即至少差 100/255）。
func TestPointSampleDifferenceD2(t *testing.T) {
	img := &Image{W: 2, H: 2, Pix: []uint8{0, 0, 200, 200}}
	area := downsampleAreaAvg(img, 2)
	point := pointSample(img, 2)
	const lowerBound = 100
	diff := int(area.Pix[0]) - int(point.Pix[0])
	if diff < 0 {
		diff = -diff
	}
	if diff < lowerBound {
		t.Fatalf("d=2 差异 %d 未达到下界 %d", diff, lowerBound)
	}
	t.Logf("d=2 证据: 面积平均=%d 点采样=%d 差异=%d (下界 %d/255)",
		area.Pix[0], point.Pix[0], diff, lowerBound)
}

// TestPointSampleDifferenceD3：第二档除数 d=3，取 3x3 图，点采样取左上角 0，
// 面积平均为 (8 个 28)/9 = 224/9 = 25（四舍五入），差异 25，下界 25。
func TestPointSampleDifferenceD3(t *testing.T) {
	img := &Image{W: 3, H: 3, Pix: make([]uint8, 9)}
	for i := 1; i < 9; i++ {
		img.Pix[i] = 28
	}
	area := downsampleAreaAvg(img, 3)
	point := pointSample(img, 3)
	const lowerBound = 25
	diff := int(area.Pix[0]) - int(point.Pix[0])
	if diff < 0 {
		diff = -diff
	}
	if diff < lowerBound {
		t.Fatalf("d=3 差异 %d 未达到下界 %d", diff, lowerBound)
	}
	if area.Pix[0] != 25 {
		t.Fatalf("d=3 面积平均应为 round(224/9)=25，实际 %d", area.Pix[0])
	}
	t.Logf("d=3 证据: 面积平均=%d 点采样=%d 差异=%d (下界 %d/255)",
		area.Pix[0], point.Pix[0], diff, lowerBound)
}

// TestThumbnailRoundTripAllFormats：三种格式分别验证降采样编码后能按缩略图画幅解回，
// 且解回像素与面积平均结果一致。
func TestThumbnailRoundTripAllFormats(t *testing.T) {
	img := sample() // 4x3，d=2 -> 2x2
	for _, f := range []string{"pgm", "bmp", "rle"} {
		body, err := Encode(img, Options{Format: f, Divisor: 2})
		if err != nil {
			t.Fatalf("%s 编码失败: %v", f, err)
		}
		got, err := Decode(body, Options{Format: f, Divisor: 1})
		if err != nil {
			t.Fatalf("%s 解码失败: %v", f, err)
		}
		want := downsampleAreaAvg(img, 2)
		if got.W != want.W || got.H != want.H || !bytes.Equal(got.Pix, want.Pix) {
			t.Fatalf("%s 缩略图回读不一致: got %dx%d %v, want %dx%d %v",
				f, got.W, got.H, got.Pix, want.W, want.H, want.Pix)
		}
		// 输出画幅必须真的变小，而不是整图。
		if got.W >= img.W || got.H >= img.H {
			t.Fatalf("%s 缩略图未缩小", f)
		}
	}
}

// TestDecodeSideThumbnail：decode 侧带 Divisor 同样产出缩略图。
func TestDecodeSideThumbnail(t *testing.T) {
	img := sample()
	body, err := Encode(img, opt("pgm"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(body, Options{Format: "pgm", Divisor: 2})
	if err != nil {
		t.Fatal(err)
	}
	want := downsampleAreaAvg(img, 2)
	if got.W != want.W || got.H != want.H || !bytes.Equal(got.Pix, want.Pix) {
		t.Fatalf("decode 侧缩略图错误: got %v want %v", got.Pix, want.Pix)
	}
}

// TestBadDivisorErrorType：非法除数要返回公开的 ErrBadOption，而不是任意错误。
func TestBadDivisorErrorType(t *testing.T) {
	img := sample()
	_, err := Encode(img, Options{Format: "pgm", Divisor: 0})
	if !errors.Is(err, ErrBadOption) {
		t.Fatalf("Divisor=0 应返回 ErrBadOption，实际 %v", err)
	}
}
