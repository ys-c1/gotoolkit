package api

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sample() *Image {
	img := &Image{W: 4, H: 3, Pix: make([]uint8, 12)}
	for i := range img.Pix {
		img.Pix[i] = uint8(i * 7)
	}
	return img
}

func TestRoundTripAllFormats(t *testing.T) {
	for _, f := range []string{"pgm", "bmp", "rle"} {
		img := sample()
		body, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatalf("%s 编码失败: %v", f, err)
		}
		got, err := Decode(body, Options{Format: f})
		if err != nil {
			t.Fatalf("%s 解码失败: %v", f, err)
		}
		if got.W != img.W || got.H != img.H {
			t.Fatalf("%s 尺寸不一致", f)
		}
		if !bytes.Equal(got.Pix, img.Pix) {
			t.Fatalf("%s 像素不一致", f)
		}
	}
}

func TestGoldenBytes(t *testing.T) {
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
			t.Fatalf("%s 与原编码不逐字节一致", f)
		}
	}
}

func TestRejects(t *testing.T) {
	img := sample()
	if _, err := Encode(img, Options{Format: "tga"}); err == nil {
		t.Fatal("未知格式应报错")
	}
	if _, err := Encode(img, Options{Format: "pgm", Gain: 500}); err == nil {
		t.Fatal("增益越界应报错")
	}
	if _, err := Encode(&Image{W: 0, H: 1}, Options{Format: "pgm"}); err == nil {
		t.Fatal("画幅为零应报错")
	}
	if _, err := Decode(nil, Options{Format: "pgm"}); err == nil {
		t.Fatal("空数据应报错")
	}
}

// 非法翻转模式必须拒绝，且错误里给出全部合法取值。
func TestFlipInvalidMode(t *testing.T) {
	img := sample()
	for _, bad := range []string{"x", "V", "flip", "h"} {
		_, err := Encode(img, Options{Format: "pgm", Flip: bad})
		if err == nil {
			t.Fatalf("Flip=%q 应报错", bad)
		}
		for _, want := range []string{`""`, `"none"`, `"v"`} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("Flip=%q 的报错缺少取值 %s: %v", bad, want, err)
			}
		}
		if _, err := Decode([]byte("P5\n1 1\n255\n\x00"), Options{Format: "pgm", Flip: bad}); err == nil {
			t.Fatalf("Decode 路径 Flip=%q 也应报错", bad)
		}
	}
}

// 翻转两次必须逐字节回到原图，三种格式逐一核对。
func TestFlipTwiceByteExact(t *testing.T) {
	// 奇数行高 + 宽度不整除 4（BMP 有行补齐）的样本。
	img := &Image{W: 3, H: 3, Pix: []uint8{1, 2, 3, 4, 5, 6, 7, 8, 9}}
	// 像素级：flipVertical 两次等于原图。
	if twice := flipVertical(flipVertical(img)); !bytes.Equal(twice.Pix, img.Pix) {
		t.Fatal("flipVertical 两次未回到原图")
	}
	for _, f := range []string{"pgm", "bmp", "rle"} {
		body, err := Encode(img, Options{Format: f, Flip: "v"})
		if err != nil {
			t.Fatalf("%s 翻转编码失败: %v", f, err)
		}
		back, err := Decode(body, Options{Format: f, Flip: "v"})
		if err != nil {
			t.Fatalf("%s 翻转解码失败: %v", f, err)
		}
		if !bytes.Equal(back.Pix, img.Pix) {
			t.Fatalf("%s 翻转两次未逐字节回到原图", f)
		}
		// 编码层面对偶：翻转图编码 == 原图编码后再翻转解码，殊途同归。
		plain, err := Encode(back, Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		plainAgain, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(plain, plainAgain) {
			t.Fatalf("%s 还原后再编码与原编码不逐字节一致", f)
		}
	}
}

// pgm 自顶向下：翻转后第一行应是原最后一行。
func TestFlipPGMTopDown(t *testing.T) {
	img := &Image{W: 2, H: 3, Pix: []uint8{1, 2, 3, 4, 5, 6}}
	body, err := Encode(img, Options{Format: "pgm", Flip: "v"})
	if err != nil {
		t.Fatal(err)
	}
	head := []byte("P5\n2 3\n255\n")
	if !bytes.Equal(body[:len(head)], head) {
		t.Fatalf("翻转后 PGM 头变了: %q", body[:len(head)])
	}
	want := []uint8{5, 6, 3, 4, 1, 2}
	if !bytes.Equal(body[len(head):], want) {
		t.Fatalf("PGM 翻转行序错误: %v", body[len(head):])
	}
}

// bmp 自底向上：翻转后文件里第一行应是原图第一行（存储序整体对调），头 54 字节不变。
func TestFlipBMPBottomUp(t *testing.T) {
	img := &Image{W: 2, H: 3, Pix: []uint8{1, 2, 3, 4, 5, 6}}
	plain, err := Encode(img, Options{Format: "bmp"})
	if err != nil {
		t.Fatal(err)
	}
	flipped, err := Encode(img, Options{Format: "bmp", Flip: "v"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain[:54], flipped[:54]) {
		t.Fatal("翻转后 BMP 头 54 字节与原来不一致")
	}
	// 未翻转时文件首行是原图末行 5,6；翻转后文件首行应是原图首行 1,2。
	row := []uint8{1, 1, 1, 2, 2, 2, 0, 0} // w=2 → 6 字节补齐到 8
	if !bytes.Equal(flipped[54:62], row) {
		t.Fatalf("BMP 翻转后首行错误: %v", flipped[54:62])
	}
}

// 奇数行高 + 行补齐：补齐字节必须跟着自己的行走，且翻转前后头部一致。
func TestFlipBMPPaddingOddHeight(t *testing.T) {
	// w=3 → 每行 9 字节，补齐到 12（3 个补齐字节）；h=3 奇数行高。
	img := &Image{W: 3, H: 3, Pix: []uint8{1, 2, 3, 4, 5, 6, 7, 8, 9}}
	plain, err := Encode(img, Options{Format: "bmp"})
	if err != nil {
		t.Fatal(err)
	}
	flipped, err := Encode(img, Options{Format: "bmp", Flip: "v"})
	if err != nil {
		t.Fatal(err)
	}
	const rowSize = 12
	if len(plain) != 54+rowSize*3 || len(flipped) != len(plain) {
		t.Fatalf("长度错误: plain=%d flipped=%d", len(plain), len(flipped))
	}
	if !bytes.Equal(plain[:54], flipped[:54]) {
		t.Fatal("翻转后 BMP 头（含 dataSize）与原来不一致")
	}
	for y := 0; y < 3; y++ {
		pad := flipped[54+y*rowSize+9 : 54+(y+1)*rowSize]
		if !bytes.Equal(pad, []byte{0, 0, 0}) {
			t.Fatalf("第 %d 行补齐字节未随行为零: %v", y, pad)
		}
		// 翻转后文件第 y 行 = 原图第 y 行（自底向上存储 + 垂直翻转对消）。
		for x := 0; x < 3; x++ {
			v := flipped[54+y*rowSize+x*3]
			if want := img.Pix[y*3+x]; v != want {
				t.Fatalf("第 %d 行第 %d 像素 = %d,  want %d", y, x, v, want)
			}
		}
	}
	// 翻转流的补齐字节布局与原流一致（都是每行尾 3 个 0）。
	for y := 0; y < 3; y++ {
		if !bytes.Equal(plain[54+y*rowSize+9:54+(y+1)*rowSize], flipped[54+y*rowSize+9:54+(y+1)*rowSize]) {
			t.Fatalf("第 %d 行补齐字节翻转前后不一致", y)
		}
	}
}

// 默认模式（Flip 零值）下三份基准字节必须保持一致。
func TestGoldenUnchangedWithDefaultFlip(t *testing.T) {
	if DefaultOptions().Flip != "" {
		t.Fatal("默认 Flip 应为空串")
	}
	img := sample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		want, err := os.ReadFile(filepath.Join("..", "testdata", "golden", f+".bin"))
		if err != nil {
			t.Skipf("缺 golden %s", f)
		}
		got, err := Encode(img, Options{Format: f}) // Flip 零值
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("默认模式下 %s 与基准不逐字节一致", f)
		}
	}
}
