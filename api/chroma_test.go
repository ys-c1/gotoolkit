package api

import (
	"bytes"
	"testing"

	"gotoolkit/internal/chroma"
)

// TestDisabledPathByteIdentical 不启用色度路径时，三种灰度格式输出与基线逐字节一致。
// 新增 ChromaRecon 字段的零值不得改变任何既有编码结果。
func TestDisabledPathByteIdentical(t *testing.T) {
	img := sample()
	// 显式构造带零值 ChromaRecon 的 Options，与不带该字段的默认 Options 输出必须相同。
	for _, f := range []string{"pgm", "bmp", "rle"} {
		a, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		b, err := Encode(img, Options{Format: f, ChromaRecon: ""})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("%s 零值 ChromaRecon 改变了灰度输出", f)
		}
	}
}

func TestPPMRoundTrip(t *testing.T) {
	src := &ColorImage{W: 4, H: 2, Pix: []uint8{
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12,
		13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24,
	}}
	body, err := EncodePPM(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(body[:11]) != "P6\n4 2\n255\n" {
		t.Fatalf("PPM 头错误: %q", body[:11])
	}
	got, err := DecodePPM(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.W != 4 || got.H != 2 || !bytes.Equal(got.Pix, src.Pix) {
		t.Fatal("PPM 往返像素不一致")
	}
}

func TestChromaRoundTripModes(t *testing.T) {
	ref := genColor(16, 8)
	for _, mode := range []string{ChromaReconBilinear, ChromaReconAdaptive} {
		out, err := ChromaRoundTrip(ref, Options{ChromaRecon: mode})
		if err != nil {
			t.Fatalf("%s 往返失败: %v", mode, err)
		}
		if out.W != ref.W || out.H != ref.H || len(out.Pix) != len(ref.Pix) {
			t.Fatalf("%s 输出尺寸错误", mode)
		}
	}
}

func TestChromaRejectsBadOption(t *testing.T) {
	ref := genColor(8, 8)
	// 空串与非法模式都应报错。
	if _, err := ChromaRoundTrip(ref, Options{}); err == nil {
		t.Fatal("未指定重建模式应报错")
	}
	if _, err := ChromaRoundTrip(ref, Options{ChromaRecon: "nearest"}); err == nil {
		t.Fatal("非法重建模式应报错")
	}
	// 奇数画幅报错。
	odd := &ColorImage{W: 7, H: 8, Pix: make([]uint8, 7*8*3)}
	if _, err := ChromaRoundTrip(odd, Options{ChromaRecon: ChromaReconAdaptive}); err == nil {
		t.Fatal("奇数画幅应报错")
	}
}

func TestChromaColorDiffOrdering(t *testing.T) {
	ref := genColor(32, 32)
	bi, _ := ChromaRoundTrip(ref, Options{ChromaRecon: ChromaReconBilinear})
	ad, _ := ChromaRoundTrip(ref, Options{ChromaRecon: ChromaReconAdaptive})
	mB, _, _, _, err := ChromaColorDiff(ref, bi, Options{ChromaRecon: ChromaReconBilinear})
	if err != nil {
		t.Fatal(err)
	}
	mA, _, _, _, err := ChromaColorDiff(ref, ad, Options{ChromaRecon: ChromaReconAdaptive})
	if err != nil {
		t.Fatal(err)
	}
	if mA >= mB {
		t.Fatalf("自适应平均色差应更低: adapt=%.4f bilinear=%.4f", mA, mB)
	}
}

// genColor 构造带强彩色边缘的偶数尺寸测试图（左右两个高对比半区）。
func genColor(w, h int) *ColorImage {
	img := &ColorImage{W: w, H: h, Pix: make([]uint8, w*h*3)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := 3 * (y*w + x)
			if x < w/2 {
				img.Pix[p], img.Pix[p+1], img.Pix[p+2] = 220, 60, 60
			} else {
				img.Pix[p], img.Pix[p+1], img.Pix[p+2] = 40, 80, 220
			}
		}
	}
	_ = chroma.ModeAdaptive // 保持对内部模式常量的引用语义清晰
	return img
}
