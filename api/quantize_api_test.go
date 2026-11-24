package api

import "testing"

func TestQuantizeOptionsValidation(t *testing.T) {
	img := sample()
	if _, err := Encode(img, Options{Format: "pgm", Buckets: 1}); err == nil {
		t.Fatal("Buckets=1 应报错")
	}
	if _, err := Encode(img, Options{Format: "pgm", Buckets: 300}); err == nil {
		t.Fatal("Buckets=300 应报错")
	}
	if _, err := Encode(img, Options{Format: "pgm", Buckets: 4, Dither: 1.5}); err == nil {
		t.Fatal("Dither=1.5 应报错")
	}
	if _, err := Encode(img, Options{Format: "pgm", Buckets: 4, Dither: -0.1}); err == nil {
		t.Fatal("Dither=-0.1 应报错")
	}
}

func TestDefaultOptionsUnchanged(t *testing.T) {
	// 默认组合（Buckets=0、Dither=0）必须与不含量化参数时逐字节一致。
	img := sample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		bare, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		withDefaults, err := Encode(img, Options{Format: f, Buckets: DefaultOptions().Buckets, Dither: DefaultOptions().Dither})
		if err != nil {
			t.Fatal(err)
		}
		if string(bare) != string(withDefaults) {
			t.Fatalf("%s 默认组合产物发生变化", f)
		}
	}
}

func TestQuantizeRoundTrip(t *testing.T) {
	img := sample()
	body, err := Encode(img, Options{Format: "pgm", Buckets: 8, Dither: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(body, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	if got.W != img.W || got.H != img.H || len(got.Pix) != len(img.Pix) {
		t.Fatal("量化后画幅不一致")
	}
	for _, v := range got.Pix {
		// K=8, Δ=32：输出电平为 32 的倍数（夹断到 255）。
		if v != 255 && v%32 != 0 {
			t.Fatalf("量化电平非法: %d", v)
		}
	}
}
