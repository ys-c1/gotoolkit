package api

import (
	"bytes"
	"os"
	"path/filepath"
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

func TestBLKRoundTrip(t *testing.T) {
	// sample() 是 4×3，必须用块长同时整除 4 和 3 的画幅，这里自建 4×4。
	pix := make([]uint8, 16)
	for i := range pix {
		pix[i] = uint8(i*13 + 7)
	}
	img := &Image{W: 4, H: 4, Pix: pix}
	body, err := Encode(img, Options{Format: "blk", BlockLen: 2})
	if err != nil {
		t.Fatalf("blk 编码失败: %v", err)
	}
	got, err := Decode(body, Options{Format: "blk"})
	if err != nil {
		t.Fatalf("blk 解码失败: %v", err)
	}
	if got.W != 4 || got.H != 4 || len(got.Pix) != 16 {
		t.Fatal("blk 回读画幅错误")
	}
}

func TestBLKDefaultBlockLenEquivalence(t *testing.T) {
	// 8×8 画幅：默认块长（BlockLen=0）与显式 8 必须逐字节一致。
	pix := make([]uint8, 64)
	for i := range pix {
		pix[i] = uint8(i*5 + 1)
	}
	img := &Image{W: 8, H: 8, Pix: pix}
	a, err := Encode(img, Options{Format: "blk"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encode(img, Options{Format: "blk", BlockLen: 8})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("默认块长产物与显式 BlockLen=8 不一致")
	}
	if a[8] != 8 {
		t.Fatalf("流中块长应为 8, got %d", a[8])
	}
}

func TestBLKBlockLenRejects(t *testing.T) {
	img := sample() // 4×3
	if _, err := Encode(img, Options{Format: "blk", BlockLen: 256}); err == nil {
		t.Fatal("块长超 255 应报错")
	}
	if _, err := Encode(img, Options{Format: "blk", BlockLen: 2}); err == nil {
		t.Fatal("块长不整除画幅宽高应报错")
	}
	if _, err := Encode(img, Options{Format: "blk", BlockLen: 4}); err == nil {
		t.Fatal("块长整除宽但不整除高也应报错")
	}
}

func TestExistingFormatsUnchanged(t *testing.T) {
	// 回归：新增 blk 与 BlockLen 后，三种旧格式产物仍与 golden 逐字节一致。
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
			t.Fatalf("%s 产物与改前不一致", f)
		}
	}
}
