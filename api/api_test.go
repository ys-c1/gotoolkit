package api

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"

	"gotoolkit/internal/quant"
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

func TestQuantizationDisabledKeepsOutput(t *testing.T) {
	// 不启用 Levels/Dither/AutoDither 时，产物必须与只设 Format/Gain 时逐字节一致。
	img := sample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		want, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		got, err := Encode(img, Options{Format: f, Levels: 0, Dither: 0, AutoDither: false})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s 未启用量化时字节不一致", f)
		}
		wantGain, err := Encode(img, Options{Format: f, Gain: 20})
		if err != nil {
			t.Fatal(err)
		}
		gotGain, err := Encode(img, Options{Format: f, Gain: 20, Levels: 0, Dither: 0})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gotGain, wantGain) {
			t.Fatalf("%s 带增益且未量化的字节不一致", f)
		}
	}
}

func TestOptimalDitherTable(t *testing.T) {
	// 三档桶数：最优配比必须同时优于不抖动与满强度抖动。
	for _, level := range []int{4, 16, 64} {
		plan, err := OptimalDither(level)
		if err != nil {
			t.Fatal(err)
		}
		if plan.Levels != level {
			t.Fatalf("levels=%d 返回到 %d", level, plan.Levels)
		}
		if plan.Tau <= 0 || plan.Tau >= 1 {
			t.Fatalf("levels=%d 最优 τ=%.4f 不在 (0,1)", level, plan.Tau)
		}
		parts0, _ := planFor(t, level, 0)
		parts1, _ := planFor(t, level, 1)
		if !(plan.Weighted() < parts0.Weighted() && plan.Weighted() < parts1.Weighted()) {
			t.Fatalf("levels=%d 最优 W=%.4f 未同时低于 τ=0 的 %.4f 与 τ=1 的 %.4f",
				level, plan.Weighted(), parts0.Weighted(), parts1.Weighted())
		}
	}
}

// planFor 用公开 API 在均匀灰度图上实测某个强度下的误差分解。
func planFor(t *testing.T, levels, dither int) (ErrorParts, *Image) {
	t.Helper()
	src := tileImage()
	body, err := Encode(src, Options{Format: "pgm", Levels: levels, Dither: dither})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Decode(body, Options{Format: "pgm"})
	if err != nil {
		t.Fatal(err)
	}
	parts, ok := MeasureQuantization(src, out, levels)
	if !ok {
		t.Fatal("测量失败")
	}
	return parts, out
}

func TestAutoDitherMatchesPlan(t *testing.T) {
	const levels = 16
	plan, err := OptimalDither(levels)
	if err != nil {
		t.Fatal(err)
	}
	src := tileImage()
	auto, err := Encode(src, Options{Format: "pgm", Levels: levels, AutoDither: true})
	if err != nil {
		t.Fatal(err)
	}
	manual, err := Encode(src, Options{Format: "pgm", Levels: levels, Dither: int(plan.Tau*100 + 0.5)})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(auto, manual) {
		t.Fatal("AutoDither 与按四舍五入后的强度手动设置结果不一致")
	}
}

// tileImage 生成 256×256 的 8×8 色块图：每个灰度值恰好占一个 8×8 小块，
// 从而均匀覆盖 Bayer 的全部 64 个相位；此时模型预测与逐像素实测一致。
func tileImage() *Image {
	const w, h = 256, 256
	img := &Image{W: w, H: h, Pix: make([]uint8, w*h)}
	cols := w / 8
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Pix[y*w+x] = uint8(((y/8)*cols + x/8) % 256)
		}
	}
	return img
}

func TestQuantizationReducesWeightedError(t *testing.T) {
	const levels = 16
	src := tileImage()
	hist := Histogram(src)
	for _, dither := range []int{0, 100} {
		body, err := Encode(src, Options{Format: "pgm", Levels: levels, Dither: dither})
		if err != nil {
			t.Fatal(err)
		}
		out, err := Decode(body, Options{Format: "pgm"})
		if err != nil {
			t.Fatal(err)
		}
		got, ok := MeasureQuantization(src, out, levels)
		if !ok {
			t.Fatal("测量失败")
		}
		want := quant.Predict(hist, levels, float64(dither)/100)
		if math.Abs(got.Banding-want.Banding) > 1e-9 || math.Abs(got.Noise-want.Noise) > 1e-9 {
			t.Fatalf("dither=%d 实测 (%v) 与模型 (%v) 不符", dither, got, want)
		}
	}
}

func TestQuantizationOptionValidation(t *testing.T) {
	img := sample()
	bad := []Options{
		{Format: "pgm", Levels: 1},
		{Format: "pgm", Levels: 257},
		{Format: "pgm", Levels: 4, Dither: 101},
		{Format: "pgm", Levels: 4, Dither: -1},
		{Format: "pgm", AutoDither: true},
		{Format: "pgm", Dither: 50},
		{Format: "pgm", Levels: 4, Dither: 50, AutoDither: true},
	}
	for _, opt := range bad {
		if _, err := Encode(img, opt); err == nil {
			t.Fatalf("应拒绝 %+v", opt)
		}
	}
	if _, err := OptimalDither(1); err == nil {
		t.Fatal("桶数越界应报错")
	}
	if _, err := OptimalDitherFor([256]float64{}, 300); err == nil {
		t.Fatal("桶数越界应报错")
	}
}
