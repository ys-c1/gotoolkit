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
	if _, err := Encode(img, Options{Format: "subband", SubLevels: 9}); err == nil {
		t.Fatal("子带层数越界应报错")
	}
}

func TestSubbandRoundTripAndModes(t *testing.T) {
	// 画幅需能被 2^levels 整除，用 8x8。
	img := &Image{W: 8, H: 8, Pix: make([]uint8, 64)}
	for i := range img.Pix {
		img.Pix[i] = uint8(i*3 + 7)
	}
	budget := 8 * 8 * 2
	for _, water := range []bool{true, false} {
		opt := Options{Format: "subband", SubLevels: 2, SubBudget: budget, SubWaterfill: water}
		body, err := Encode(img, opt)
		if err != nil {
			t.Fatalf("waterfill=%v 编码失败: %v", water, err)
		}
		if _, err := Decode(body, Options{Format: "subband"}); err != nil {
			t.Fatalf("waterfill=%v 解码失败: %v", water, err)
		}
	}
	// 默认选项（零值层数/预算）应自动补默认值。
	body, err := Encode(img, Options{Format: "subband"})
	if err != nil {
		t.Fatalf("默认子带选项编码失败: %v", err)
	}
	if _, err := Decode(body, Options{Format: "subband"}); err != nil {
		t.Fatalf("默认子带选项解码失败: %v", err)
	}
}
