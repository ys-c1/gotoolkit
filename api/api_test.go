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

// opt 构造显式合法 Options：Divisor 为零值会被 validate 拦下，测试里必须写全。
func opt(format string) Options { return Options{Format: format, Divisor: 1} }

func TestRoundTripAllFormats(t *testing.T) {
	for _, f := range []string{"pgm", "bmp", "rle"} {
		img := sample()
		body, err := Encode(img, opt(f))
		if err != nil {
			t.Fatalf("%s 编码失败: %v", f, err)
		}
		got, err := Decode(body, opt(f))
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
		got, err := Encode(img, opt(f))
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
	if _, err := Encode(img, Options{Format: "tga", Divisor: 1}); err == nil {
		t.Fatal("未知格式应报错")
	}
	if _, err := Encode(img, Options{Format: "pgm", Gain: 500, Divisor: 1}); err == nil {
		t.Fatal("增益越界应报错")
	}
	if _, err := Encode(&Image{W: 0, H: 1}, Options{Format: "pgm", Divisor: 1}); err == nil {
		t.Fatal("画幅为零应报错")
	}
	if _, err := Decode(nil, Options{Format: "pgm", Divisor: 1}); err == nil {
		t.Fatal("空数据应报错")
	}
	// Divisor 为零或为负必须拦下，Encode/Decode 两个入口都要拦。
	for _, d := range []int{0, -1, -7} {
		if _, err := Encode(img, Options{Format: "pgm", Divisor: d}); err == nil {
			t.Fatalf("Divisor=%d 应在 Encode 报错", d)
		}
		body, err := Encode(img, opt("pgm"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Decode(body, Options{Format: "pgm", Divisor: d}); err == nil {
			t.Fatalf("Divisor=%d 应在 Decode 报错", d)
		}
	}
}
