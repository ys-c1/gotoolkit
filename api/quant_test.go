package api

import (
	"bytes"
	"testing"
)

// TestStepQuantizes 校验中点重建量化：q = round(v/step)*step，截断到 255。
func TestStepQuantizes(t *testing.T) {
	img := &Image{W: 4, H: 1, Pix: []uint8{0, 3, 5, 255}}
	body, err := Encode(img, Options{Format: "pgm", Step: 4})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(body, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	want := []uint8{0, 4, 4, 255}
	if !bytes.Equal(got.Pix, want) {
		t.Fatalf("量化结果 %v，期望 %v", got.Pix, want)
	}
}

// TestStepDefaultUnchanged 默认步长（0 或 1）不量化，产物逐字节不变。
func TestStepDefaultUnchanged(t *testing.T) {
	img := sample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		base, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		for _, step := range []int{0, 1} {
			got, err := Encode(img, Options{Format: f, Step: step})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, base) {
				t.Fatalf("%s step=%d 产物与默认不一致", f, step)
			}
		}
	}
}

// TestStepShrinksRLE 步长越大 RLE 体积越小，且往返误差不超过 step/2。
func TestStepShrinksRLE(t *testing.T) {
	img := rampImage()
	prev := -1
	for _, step := range []int{1, 4, 16} {
		size, mse := measure(t, img, step)
		if prev >= 0 && size >= prev {
			t.Fatalf("step=%d 体积 %d 未缩小（上一档 %d）", step, size, prev)
		}
		prev = size
		if step > 1 {
			maxErr := float64(step*step) / 4 // 逐点误差 ≤ step/2
			if mse > maxErr {
				t.Fatalf("step=%d MSE %.4f 超出 (step/2)^2=%.2f", step, mse, maxErr)
			}
		}
	}
}

// TestStepRejects 步长越界报错。
func TestStepRejects(t *testing.T) {
	img := sample()
	for _, step := range []int{-1, 256, 1000} {
		if _, err := Encode(img, Options{Format: "pgm", Step: step}); err == nil {
			t.Fatalf("Step=%d 应报错", step)
		}
	}
}
