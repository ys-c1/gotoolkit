package api

import (
	"bytes"
	"testing"
)

// 不给 Threshold（零值）时编码必须与历史行为逐字节一致。
func TestThresholdZeroKeepsBehavior(t *testing.T) {
	img := sample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		a, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		b, err := Encode(img, Options{Format: f, Threshold: 0})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("%s Threshold=0 改变了输出", f)
		}
	}
}

func TestThresholdQuantizesAndStaysWithinBound(t *testing.T) {
	// 256 级斜坡，量化误差一定取到上界。
	img := &Image{W: 16, H: 16, Pix: make([]uint8, 256)}
	for i := range img.Pix {
		img.Pix[i] = uint8(i)
	}
	for _, tau := range []int{1, 2, 4, 8, 16} {
		body, err := Encode(img, Options{Format: "pgm", Threshold: tau})
		if err != nil {
			t.Fatal(err)
		}
		got, err := Decode(body, Options{Format: "pgm"})
		if err != nil {
			t.Fatal(err)
		}
		d := ComparePixels(img.Pix, got.Pix)
		if d.MaxAbs > tau {
			t.Fatalf("tau=%d 编码后实测最大误差 %d 超阈值", tau, d.MaxAbs)
		}
	}
	if _, err := Encode(img, Options{Format: "pgm", Threshold: 300}); err == nil {
		t.Fatal("阈值越界必须报错")
	}
}

func TestAPIRunCanonical(t *testing.T) {
	rep, err := RunCanonical([]int{4, 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Runs) != 2 || len(rep.Sizes) != 5 {
		t.Fatal("应当有两个阈值、五档尺寸")
	}
	if !rep.Consistency.PairsAgree {
		t.Fatal("规范样本 τ=4/7 结论对应完全一致")
	}
}
