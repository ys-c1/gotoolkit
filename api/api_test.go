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

func TestReorderRoundTrip(t *testing.T) {
	// 构造游程很多的图：逐像素交替 16 个灰度级
	img := &Image{W: 32, H: 16, Pix: make([]uint8, 32*16)}
	for i := range img.Pix {
		img.Pix[i] = uint8(i % 16)
	}
	plain, err := Encode(img, Options{Format: "rle"})
	if err != nil {
		t.Fatal(err)
	}
	re, err := Encode(img, Options{Format: "rle", Reorder: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(re[:4]) != "GTR2" {
		t.Fatalf("Reorder 应输出 GTR2 流，得 %q", re[:4])
	}
	got, err := Decode(re, Options{Format: "rle"})
	if err != nil {
		t.Fatalf("GTR2 解码失败: %v", err)
	}
	if !bytes.Equal(got.Pix, img.Pix) {
		t.Fatal("重排后解码与原图不逐像素一致")
	}
	// 游程数：重排后应等于不同索引个数 16，且远小于重排前
	if r := CountRuns(img.Pix); r != 32*16 {
		t.Fatalf("重排前游程数应为 %d，得 %d", 32*16, r)
	}
	seq, _ := ReorderSequence(img.Pix)
	if r := CountRuns(seq); r != 16 {
		t.Fatalf("重排后游程数应为 16，得 %d", r)
	}
	if len(re) >= len(plain) {
		t.Fatalf("本图重排后体积应更小: %d >= %d", len(re), len(plain))
	}
	// 旧 GTR1 流仍按原样解码
	g1, err := Decode(plain, Options{Format: "rle"})
	if err != nil || !bytes.Equal(g1.Pix, img.Pix) {
		t.Fatal("GTR1 兼容性被破坏")
	}
}
