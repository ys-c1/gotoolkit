package api

import "testing"

func TestQuantizeRoundTripShape(t *testing.T) {
	img := sample()
	out, err := Quantize(img, DitherOptions{Levels: 8, Beta: 0.5, Seed: 11})
	if err != nil {
		t.Fatal(err)
	}
	if out.W != img.W || out.H != img.H || len(out.Pix) != len(img.Pix) {
		t.Fatal("输出画幅与输入不一致")
	}
	// 不改写输入
	if img.Pix[0] != 0 {
		t.Fatal("Quantize 改写了输入图像")
	}
	// 8 级量化，输出值必须是 delta=32 的整数倍
	for _, v := range out.Pix {
		if v%32 != 0 && v != 255 {
			t.Fatalf("非法量化级: %d", v)
		}
	}
}

func TestQuantizeRejects(t *testing.T) {
	img := sample()
	if _, err := Quantize(img, DitherOptions{Levels: 1, Beta: 0}); err == nil {
		t.Fatal("级数越界应报错")
	}
	if _, err := Quantize(img, DitherOptions{Levels: 16, Beta: 2}); err == nil {
		t.Fatal("beta 越界应报错")
	}
	if _, err := Quantize(img, DitherOptions{Levels: 16, DitherScale: 1.5}); err == nil {
		t.Fatal("抖动幅度越界应报错")
	}
	if _, err := Quantize(nil, DefaultDitherOptions()); err == nil {
		t.Fatal("空图应报错")
	}
}

func TestQuantizeDeterministic(t *testing.T) {
	img := sample()
	a, _ := Quantize(img, DitherOptions{Levels: 4, Beta: 0.25, Seed: 5})
	b, _ := Quantize(img, DitherOptions{Levels: 4, Beta: 0.25, Seed: 5})
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			t.Fatal("同种子应得到一致结果")
		}
	}
}
