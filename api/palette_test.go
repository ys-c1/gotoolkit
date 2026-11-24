package api

import (
	"bytes"
	"errors"
	"testing"
)

func TestPaletteDisabledIgnoresBucketFieldAndMatchesGoldenPath(t *testing.T) {
	img := sample()
	// 即使误填 Buckets，只要 Palette=false，就必须完全走旧路径。
	off, err := Encode(img, Options{Format: "pgm", Palette: false, Buckets: 16})
	if err != nil {
		t.Fatal(err)
	}
	old, err := Encode(img, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(off, old) {
		t.Fatal("Palette=false 且填写 Buckets 时输出发生变化")
	}
}

func TestPaletteValidatesBuckets(t *testing.T) {
	img := sample()
	for _, k := range []int{0, -1, 257} {
		_, err := Encode(img, Options{Format: "pgm", Palette: true, Buckets: k})
		if !errors.Is(err, ErrBadBuckets) {
			t.Fatalf("K=%d err=%v, want ErrBadBuckets", k, err)
		}
	}
	if _, _, err := QuantizePalette(img, Options{Format: "pgm", Palette: false}); !errors.Is(err, ErrBadOption) {
		t.Fatalf("未启用 Palette 时 QuantizePalette 应报 ErrBadOption, got %v", err)
	}
}

func TestPaletteAllFormatsRoundTrip(t *testing.T) {
	img := &Image{W: 256, H: 4, Pix: make([]uint8, 256*4)}
	for y := 0; y < 4; y++ {
		for x := 0; x < 256; x++ {
			img.Pix[y*256+x] = uint8(x)
		}
	}
	for _, f := range []string{"pgm", "bmp", "rle"} {
		opt := Options{Format: f, Palette: true, Buckets: 16}
		body, err := Encode(img, opt)
		if err != nil {
			t.Fatalf("%s encode: %v", f, err)
		}
		got, err := Decode(body, opt)
		if err != nil {
			t.Fatalf("%s decode: %v", f, err)
		}
		if got.W != img.W || got.H != img.H {
			t.Fatalf("%s dimensions %dx%d", f, got.W, got.H)
		}
		quantized, stats, err := QuantizePalette(img, Options{Palette: true, Buckets: 16})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Pix, quantized.Pix) {
			t.Fatalf("%s decoded pixels do not match palette result", f)
		}
		if stats.Actual != 16 || stats.MSE != 21.5 {
			t.Fatalf("unexpected K=16 stats: actual=%d MSE=%v", stats.Actual, stats.MSE)
		}
	}
}
