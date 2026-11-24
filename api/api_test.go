package api

import (
	"bytes"
	"math"
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

// bigSample 64x48，含渐变、平坦块与噪声状区域，便于观察 RLE 压缩差。
func bigSample() *Image {
	img := &Image{W: 64, H: 48, Pix: make([]uint8, 64*48)}
	for y := 0; y < img.H; y++ {
		for x := 0; x < img.W; x++ {
			switch {
			case y < 16:
				img.Pix[y*img.W+x] = uint8((x*4 + y) % 256)
			case y < 32:
				img.Pix[y*img.W+x] = 200
			default:
				img.Pix[y*img.W+x] = uint8((x*x + y*y) % 256)
			}
		}
	}
	return img
}

// quantStep 是位深 b 对应的均匀量化步长：Δ = 255 / (2^b − 1)。
func quantStep(b int) float64 {
	m := (1 << b) - 1
	return 255.0 / float64(m)
}

func TestRoundTripAllFormats(t *testing.T) {
	for _, f := range []string{"pgm", "bmp", "rle"} {
		img := sample()
		body, err := Encode(img, Options{Format: f, BitDepth: 8})
		if err != nil {
			t.Fatalf("%s 编码失败: %v", f, err)
		}
		got, err := Decode(body, Options{Format: f, BitDepth: 8})
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
		got, err := Encode(img, Options{Format: f, BitDepth: 8})
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
	if _, err := Encode(img, Options{Format: "tga", BitDepth: 8}); err == nil {
		t.Fatal("未知格式应报错")
	}
	if _, err := Encode(img, Options{Format: "pgm", Gain: 500, BitDepth: 8}); err == nil {
		t.Fatal("增益越界应报错")
	}
	if _, err := Encode(&Image{W: 0, H: 1}, Options{Format: "pgm", BitDepth: 8}); err == nil {
		t.Fatal("画幅为零应报错")
	}
	if _, err := Decode(nil, Options{Format: "pgm", BitDepth: 8}); err == nil {
		t.Fatal("空数据应报错")
	}
}

// 位深为 0 或超过 8 必须拦下，且错误信息指出字段名 BitDepth。
func TestBitDepthRejects(t *testing.T) {
	img := sample()
	for _, b := range []int{-1, 0, 9, 16} {
		for _, op := range []string{"encode", "decode"} {
			var err error
			if op == "encode" {
				_, err = Encode(img, Options{Format: "pgm", BitDepth: b})
			} else {
				_, err = Decode([]byte("P5\n1 1\n255\n\x00"), Options{Format: "pgm", BitDepth: b})
			}
			if err == nil {
				t.Fatalf("%s BitDepth=%d 应报错", op, b)
			}
			if !strings.Contains(err.Error(), "BitDepth") {
				t.Fatalf("%s BitDepth=%d 错误未指出字段: %v", op, b, err)
			}
		}
	}
}

// 对全部 256 个灰度级、全部合法位深穷举：量化误差不得超过反解步长的一半。
func TestQuantStepBoundExhaustive(t *testing.T) {
	img := &Image{W: 256, H: 1, Pix: make([]uint8, 256)}
	for i := range img.Pix {
		img.Pix[i] = uint8(i)
	}
	for b := 1; b <= 8; b++ {
		half := quantStep(b) / 2
		got := quantize(img, b)
		for v := 0; v < 256; v++ {
			e := math.Abs(float64(v) - float64(got.Pix[v]))
			if e > half {
				t.Fatalf("BitDepth=%d v=%d 误差 %g 超过步长一半 %g", b, v, e, half)
			}
		}
	}
	if !bytes.Equal(quantize(img, 8).Pix, img.Pix) {
		t.Fatal("BitDepth=8 应逐像素恒等")
	}
}

// 两档位深 × 三种格式：编解码往返后最大误差不得超过 Δ/2，并记录体积。
func TestQuantRoundTripBound(t *testing.T) {
	img := bigSample()
	for _, b := range []int{4, 2} {
		half := quantStep(b) / 2
		for _, f := range []string{"pgm", "bmp", "rle"} {
			body, err := Encode(img, Options{Format: f, BitDepth: b})
			if err != nil {
				t.Fatalf("b=%d %s 编码失败: %v", b, f, err)
			}
			got, err := Decode(body, Options{Format: f, BitDepth: b})
			if err != nil {
				t.Fatalf("b=%d %s 解码失败: %v", b, f, err)
			}
			maxErr := 0
			for i := range img.Pix {
				d := int(img.Pix[i]) - int(got.Pix[i])
				if d < 0 {
					d = -d
				}
				if d > maxErr {
					maxErr = d
				}
			}
			if float64(maxErr) > half {
				t.Fatalf("b=%d %s 最大误差 %d 超过步长一半 %g", b, f, maxErr, half)
			}
			t.Logf("bitDepth=%d step=%.4g half=%.4g format=%s maxErr=%d size=%d",
				b, quantStep(b), half, f, maxErr, len(body))
		}
	}
}
