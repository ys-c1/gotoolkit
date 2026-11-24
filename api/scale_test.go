package api

import "testing"

func TestScaleSwitchAndGammaLab(t *testing.T) {
	img := &Image{W: 4, H: 4, Pix: make([]uint8, 16)}
	for y := 0; y < 4; y++ {
		for _, x := range []int{0, 2} {
			img.Pix[y*4+x] = 32
		}
	}

	enc, err := Scale(img, Options{Ratio: 0.5, ScaleSpace: "encoded", Gamma: 2.2})
	if err != nil {
		t.Fatal(err)
	}
	if enc.W != 2 || enc.H != 2 {
		t.Fatalf("尺寸应为 2x2，得到 %dx%d", enc.W, enc.H)
	}
	for i, v := range enc.Pix {
		if i%2 == 0 && v != 16 {
			t.Fatalf("第 %d 个边缘块应为 16，得到 %d", i, v)
		}
	}

	comp, err := Scale(img, Options{Ratio: 0.5, ScaleSpace: "compensated", Gamma: 2.2})
	if err != nil {
		t.Fatal(err)
	}
	dual, err := Scale(img, Options{Ratio: 0.5, ScaleSpace: "gamma-dual", Gamma: 2.2})
	if err != nil {
		t.Fatal(err)
	}
	for i := range dual.Pix {
		if d := int(comp.Pix[i]) - int(dual.Pix[i]); d < -1 || d > 1 {
			t.Fatalf("像素 %d 残差 %d 超出 1 code", i, d)
		}
	}

	rows, err := GammaLab(Options{Gamma: 2.2, Tolerance: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("应有三档，得到 %d", len(rows))
	}
	for _, r := range rows {
		if len(r.Frames) != 2 || !r.WithinTolerance {
			t.Fatalf("报告不完整或未过容差: %+v", r)
		}
	}
}
