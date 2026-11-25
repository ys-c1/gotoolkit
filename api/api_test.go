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
	for _, f := range []string{"pgm", "bmp", "rle", "gtbk"} {
		img := sample()
		if f == "gtbk" {
			// 现有 sample 为 4x3，gtbk 要求整除；改用 4x4。
			img = &Image{W: 4, H: 4, Pix: make([]uint8, 16)}
			for i := range img.Pix {
				img.Pix[i] = uint8(i*11 + 3)
			}
		}
		body, err := Encode(img, Options{Format: f, Block: 2})
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

func TestGTBKDefaultAndBadBlock(t *testing.T) {
	img := &Image{W: 8, H: 8, Pix: make([]uint8, 64)}
	for i := range img.Pix {
		img.Pix[i] = uint8(i*5 + 1)
	}
	body, err := Encode(img, Options{Format: "gtbk"})
	if err != nil {
		t.Fatalf("默认块长编码失败: %v", err)
	}
	if int(body[8]) != DefaultBlock {
		t.Fatalf("默认块长应为 %d", DefaultBlock)
	}
	if _, err := Encode(img, Options{Format: "gtbk", Block: 3}); err == nil {
		t.Fatal("非 2 的幂块长应报错")
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
