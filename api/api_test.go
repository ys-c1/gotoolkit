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
	if _, err := Encode(img, Options{Format: "rle", ChunkSize: -1}); err == nil {
		t.Fatal("块长为负应报错")
	}
	if _, err := Encode(img, Options{Format: "rle", ChunkSize: 70000}); err == nil {
		t.Fatal("块长超上限应报错")
	}
	if _, err := Encode(&Image{W: 0, H: 1}, Options{Format: "pgm"}); err == nil {
		t.Fatal("画幅为零应报错")
	}
	if _, err := Decode(nil, Options{Format: "pgm"}); err == nil {
		t.Fatal("空数据应报错")
	}
}

func TestRLEChunkedViaAPI(t *testing.T) {
	img := sample()
	body, err := Encode(img, Options{Format: "rle", ChunkSize: 5})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(body, Options{Format: "rle"})
	if err != nil {
		t.Fatal(err)
	}
	if got.W != img.W || got.H != img.H || !bytes.Equal(got.Pix, img.Pix) {
		t.Fatal("分块 rle 往返不一致")
	}
	// 默认 ChunkSize=0 必须与显式 ChunkSize=0 输出逐字节一致
	opt := DefaultOptions()
	opt.Format = "rle"
	def, err := Encode(img, opt)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Encode(img, Options{Format: "rle", ChunkSize: 0})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(def, plain) {
		t.Fatal("默认块长应与不分块输出一致")
	}
}
