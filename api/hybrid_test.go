package api

import (
	"bytes"
	"fmt"
	"testing"

	"gotoolkit/internal/hybrid"
	"gotoolkit/internal/testimg"
)

func allImages() map[string]*Image {
	out := map[string]*Image{}
	for name, pix := range testimg.All() {
		out[name] = &Image{W: testimg.Size, H: testimg.Size, Pix: pix}
	}
	return out
}

func TestHybridRoundTripAndMask(t *testing.T) {
	for name, img := range allImages() {
		a, err := AnalyzeHybrid(img)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		body, err := Encode(img, Options{Format: "hybrid"})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(body[:4]) != "GTH1" {
			t.Fatalf("%s: 魔数 %q", name, body[:4])
		}
		w, h, pix, mask, err := hybrid.DecodeMasked(body)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := &Image{W: w, H: h, Pix: pix}
		if w != img.W || h != img.H {
			t.Fatalf("%s: 尺寸不符", name)
		}
		if !bytes.Equal(mask, a.Mask) {
			t.Fatalf("%s: 解码掩码与分析掩码不一致", name)
		}
		if err := checkDistortions(img.Pix, got.Pix, a); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// 再走一次公共 Decode 入口。
		dst, err := Decode(body, Options{Format: "hybrid"})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(dst.Pix, got.Pix) {
			t.Fatalf("%s: 公共入口像素不一致", name)
		}
	}
}

// checkDistortions 复核报告里的两类失真确实是解码结果对原图的真实误差。
func checkDistortions(orig, dec []uint8, a *HybridAnalysis) error {
	var seFlat, seTex float64
	for _, st := range a.Blocks {
		var se int
		for y := st.BY * hybrid.Block; y < st.BY*hybrid.Block+hybrid.Block; y++ {
			for x := st.BX * hybrid.Block; x < st.BX*hybrid.Block+hybrid.Block; x++ {
				d := int(dec[y*a.W+x]) - int(orig[y*a.W+x])
				se += d * d
			}
		}
		if st.Flat {
			seFlat += float64(se)
		} else {
			seTex += float64(se)
		}
	}
	if a.Flat.Pixels > 0 {
		if got := seFlat / float64(a.Flat.Pixels); !closeTo(got, a.Flat.MSE, 1e-9) {
			return errf("平坦区 MSE 报告 %.6f，实算 %.6f", a.Flat.MSE, got)
		}
	}
	if a.Tex.Pixels > 0 {
		if got := seTex / float64(a.Tex.Pixels); !closeTo(got, a.Tex.MSE, 1e-9) {
			return errf("纹理区 MSE 报告 %.6f，实算 %.6f", a.Tex.MSE, got)
		}
	}
	return nil
}

func closeTo(a, b, eps float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= eps
}

type strErr string

func (e strErr) Error() string { return string(e) }

func errf(f string, args ...any) error {
	return strErr(fmt.Sprintf(f, args...))
}
