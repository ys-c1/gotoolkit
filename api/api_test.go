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

func sharpenSample(w, h, lo, hi int) *Image {
	img := &Image{W: w, H: h, Pix: make([]uint8, w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(lo)
			if x >= w/2 {
				v = uint8(hi)
			}
			img.Pix[y*w+x] = v
		}
	}
	return img
}

func TestSharpenUnsharpenedUnchanged(t *testing.T) {
	img := sharpenSample(16, 4, 96, 160)
	plain, err := Encode(img, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	want, err := Encode(img, Options{Format: "pgm", Sharpen: false})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain, want) {
		t.Fatal("未锐化时产物应与原编码一致")
	}
}

func TestSharpenZeroOvershoot(t *testing.T) {
	for _, c := range [][2]int{{96, 160}, {64, 192}, {32, 224}} {
		img := sharpenSample(32, 4, c[0], c[1])
		res, err := SolveSharpen(img, 0)
		if err != nil {
			t.Fatal(err)
		}
		if res.OverPix != 0 || res.Over != 0 {
			t.Fatalf("lo=%d hi=%d k=%d 解出增益 %.6f 下过冲 %d 像素 / %g 灰阶", c[0], c[1], res.K, res.Gain, res.OverPix, res.Over)
		}
		if len(res.Kernel) != res.K {
			t.Fatalf("核长 %d != 解出 k %d", len(res.Kernel), res.K)
		}
		over, overPix, _ := MeasureSharpen(img, res, res.Gain)
		if overPix != 0 || over != 0 {
			t.Fatalf("独立复测过冲 %d 像素 / %g 灰阶", overPix, over)
		}
		// 锐化产物经编码往返须保持一致（不再开锐化，验证的是编码本身）。
		enc, err := Encode(&Image{W: img.W, H: img.H, Pix: res.Pix}, Options{Format: "pgm"})
		if err != nil {
			t.Fatal(err)
		}
		dec, err := Decode(enc, Options{Format: "pgm"})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(dec.Pix, res.Pix) {
			t.Fatal("sharpened 结果经编码往返后不一致")
		}
	}
}

func TestSharpenOverLimitMeasured(t *testing.T) {
	img := sharpenSample(32, 4, 64, 192)
	res, err := SolveSharpen(img, 0)
	if err != nil {
		t.Fatal(err)
	}
	over, overPix, boost := MeasureSharpen(img, res, res.Gain*1.25)
	if overPix == 0 || over <= 0 {
		t.Fatalf("1.25x 应超限，实测 %d 像素 / %g", overPix, over)
	}
	if boost <= res.Boost {
		t.Fatalf("超限时边缘增幅 %.3f 应大于反解值 %.3f", boost, res.Boost)
	}
}

func TestSharpenRejects(t *testing.T) {
	img := sharpenSample(8, 2, 64, 192)
	if _, err := Encode(img, Options{Format: "pgm", Sharpen: true, SharpenK: 4}); err == nil {
		t.Fatal("偶核应报错")
	}
	if _, err := Encode(img, Options{Format: "pgm", Sharpen: true, SharpenGain: -1}); err == nil {
		t.Fatal("负增益应报错")
	}
	if _, err := SolveSharpen(nil, 0); err == nil {
		t.Fatal("空图应报错")
	}
}
