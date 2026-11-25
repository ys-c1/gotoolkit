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

func TestDepthQuantize(t *testing.T) {
	// Depth=3 → d=2, Δ=4，截断到 4 的倍数
	img := &Image{W: 4, H: 1, Pix: []uint8{0, 1, 5, 7}}
	body, err := Encode(img, Options{Format: "pgm", Depth: 3})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(body, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	want := []uint8{0, 0, 4, 4}
	if !bytes.Equal(got.Pix, want) {
		t.Fatalf("量化结果 %v，期望 %v", got.Pix, want)
	}
	// 默认 Depth=0 不量化，golden 行为不变
	if body0, err := Encode(img, Options{Format: "pgm"}); err != nil {
		t.Fatal(err)
	} else if bytes.Contains(body0, []byte{4, 4}) {
		t.Fatal("默认档位不应量化")
	}
}

func TestDepthRejects(t *testing.T) {
	img := sample()
	for _, b := range []int{1, 9, -2} {
		if _, err := Encode(img, Options{Format: "pgm", Depth: b}); err == nil {
			t.Fatalf("Depth=%d 应报错", b)
		}
	}
}
