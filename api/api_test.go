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

func TestRLECapOption(t *testing.T) {
	// 构造一个长度 5 的同值行，cap=2 时必须切成 3 条游程（2+2+1）。
	img := &Image{W: 5, H: 1, Pix: []uint8{6, 6, 6, 6, 6}}

	def, err := Encode(img, Options{Format: "rle"})
	if err != nil {
		t.Fatal(err)
	}
	if got := (len(def) - 8) / 2; got != 1 {
		t.Fatalf("默认 cap=255 应 1 条游程，得到 %d", got)
	}

	capped, err := Encode(img, Options{Format: "rle", RLECap: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := (len(capped) - 8) / 2; got != 3 {
		t.Fatalf("cap=2 应 3 条游程，得到 %d", got)
	}
	if got, err := Decode(capped, Options{Format: "rle"}); err != nil || !bytes.Equal(got.Pix, img.Pix) {
		t.Fatal("cap=2 回读失败")
	}

	for _, bad := range []int{-1, 256} {
		if _, err := Encode(img, Options{Format: "rle", RLECap: bad}); err == nil {
			t.Fatalf("RLECap=%d 应报越界", bad)
		}
	}
}
