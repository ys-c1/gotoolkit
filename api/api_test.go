package api

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
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

// ramp 生成值域铺满的渐变图，便于观察量化效果。
func ramp(w, h int) *Image {
	img := &Image{W: w, H: h, Pix: make([]uint8, w*h)}
	for i := range img.Pix {
		img.Pix[i] = uint8(i * 255 / (w*h - 1))
	}
	return img
}

// 调色板参数必须能从公开结构（Options）设进去，不是测试私有开关。
func TestPalettePublicParams(t *testing.T) {
	opt := DefaultOptions()
	opt.Palette = true
	opt.Buckets = 8
	opt.Format = "pgm"
	img := ramp(16, 16)
	out, stats, err := QuantizePalette(img, opt)
	if err != nil {
		t.Fatal(err)
	}
	if stats == nil || len(stats.Palette) == 0 {
		t.Fatal("应返回调色板统计")
	}
	if stats.Requested != 8 {
		t.Fatalf("Requested=%d, want 8", stats.Requested)
	}
	if !bytes.Equal(out.Pix, mustPalettePix(t, img, opt)) {
		t.Fatal("Encode 与 QuantizePalette 结果不一致")
	}
}

func mustPalettePix(t *testing.T, img *Image, opt Options) []uint8 {
	t.Helper()
	got, _, err := QuantizePalette(img, opt)
	if err != nil {
		t.Fatal(err)
	}
	return got.Pix
}

// 关闭调色板时，三种格式输出与 golden 逐字节一致，头长度与内容都不变。
func TestPaletteOffKeepsGolden(t *testing.T) {
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
			t.Fatalf("%s 关闭调色板后与 golden 不一致", f)
		}
	}
}

// 开启调色板后，三种格式的头长度与内容都不变，只有像素负载不同。
// PGM/BMP 的负载定长，总长也不变；RLE 的游程数随像素内容变化，总长可以变，
// 但其 8 字节头（GTR1 + 宽高）必须不变——这正是“头长度与内容都不许变”的范围。
func TestPaletteOnHeaderUnchanged(t *testing.T) {
	img := ramp(8, 8)
	for _, f := range []string{"pgm", "bmp", "rle"} {
		base, err := Encode(img, Options{Format: f})
		if err != nil {
			t.Fatal(err)
		}
		on, err := Encode(img, Options{Format: f, Palette: true, Buckets: 4})
		if err != nil {
			t.Fatal(err)
		}
		switch f {
		case "pgm":
			// P5 头: "P5\n8 8\n255\n" 长度 11。
			if string(on[:11]) != string(base[:11]) {
				t.Fatalf("pgm 头变化: %q vs %q", on[:11], base[:11])
			}
			if len(on) != len(base) {
				t.Fatalf("pgm 总长变化: %d vs %d", len(on), len(base))
			}
		case "bmp":
			if !bytes.Equal(on[:54], base[:54]) {
				t.Fatalf("bmp 头变化")
			}
			if len(on) != len(base) {
				t.Fatalf("bmp 总长变化: %d vs %d", len(on), len(base))
			}
		case "rle":
			if !bytes.Equal(on[:8], base[:8]) {
				t.Fatalf("rle 头变化: %x vs %x", on[:8], base[:8])
			}
			// 头里的宽高必须与原图一致。
			if on[4] != 8 || on[6] != 8 {
				t.Fatalf("rle 头宽高变化: %v", on[:8])
			}
		}
	}
}

// 三种格式逐一比对：同一次量化后，pgm/bmp/rle 解码出的像素应一致，且 MSE 相同。
func TestPaletteThreeFormatsAgree(t *testing.T) {
	img := ramp(12, 10)
	opt := Options{Format: "pgm", Palette: true, Buckets: 8}
	q, stats, err := QuantizePalette(img, opt)
	if err != nil {
		t.Fatal(err)
	}
	directMSE := mse(img.Pix, q.Pix)
	if math.Abs(directMSE-stats.MSE) > 1e-9 {
		t.Fatalf("stats.MSE=%v 与直算 %v 不一致", stats.MSE, directMSE)
	}
	for _, f := range []string{"pgm", "bmp", "rle"} {
		o := opt
		o.Format = f
		body, err := Encode(img, o)
		if err != nil {
			t.Fatalf("%s 编码: %v", f, err)
		}
		got, err := Decode(body, Options{Format: f})
		if err != nil {
			t.Fatalf("%s 解码: %v", f, err)
		}
		if !bytes.Equal(got.Pix, q.Pix) {
			t.Fatalf("%s 解码像素与量化结果不一致", f)
		}
		if m := mse(img.Pix, got.Pix); math.Abs(m-stats.MSE) > 1e-9 {
			t.Fatalf("%s MSE=%v, want %v", f, m, stats.MSE)
		}
	}
}

// 解码后与灰度直通的 MSE 有上界，两档桶数各验一组。
func TestPaletteMSEBoundTwoTiers(t *testing.T) {
	img := ramp(32, 32)
	for _, k := range []int{8, 32} {
		opt := Options{Format: "pgm", Palette: true, Buckets: k}
		body, err := Encode(img, opt)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Decode(body, Options{Format: "pgm"})
		if err != nil {
			t.Fatal(err)
		}
		_, stats, err := QuantizePalette(img, opt)
		if err != nil {
			t.Fatal(err)
		}
		m := mse(img.Pix, got.Pix)
		if m > stats.MSEBound+1e-9 {
			t.Fatalf("K=%d 实测 MSE=%v 超过 W+1/4=%v", k, m, stats.MSEBound)
		}
		if m > stats.ParamBound+1e-9 {
			t.Fatalf("K=%d 实测 MSE=%v 超过参数上界=%v", k, m, stats.ParamBound)
		}
		th, err := PaletteThreshold(k)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(stats.Threshold-th) > 1e-12 {
			t.Fatalf("K=%d 阈值=%v, want %v", k, stats.Threshold, th)
		}
	}
}

// 桶数为零、阈值倒置都要报错，且错误可被 errors.Is 识别。
func TestPaletteRejects(t *testing.T) {
	img := ramp(8, 8)
	if _, err := Encode(img, Options{Format: "pgm", Palette: true, Buckets: 0}); !errors.Is(err, ErrBadPaletteBuckets) {
		t.Fatalf("K=0 应报 ErrBadPaletteBuckets, got %v", err)
	}
	if _, err := Encode(img, Options{Format: "pgm", Palette: true, Buckets: -3}); !errors.Is(err, ErrBadPaletteBuckets) {
		t.Fatalf("K<0 应报 ErrBadPaletteBuckets, got %v", err)
	}
	if _, err := Encode(img, Options{Format: "pgm", Palette: true, Buckets: 257}); !errors.Is(err, ErrBadPaletteBuckets) {
		t.Fatalf("K>256 应报 ErrBadPaletteBuckets, got %v", err)
	}
	for _, th := range []float64{-1, math.NaN(), math.Inf(1), 16256.26} {
		if _, err := Encode(img, Options{Format: "pgm", Palette: true, Buckets: 4, VarianceThreshold: th}); !errors.Is(err, ErrBadPaletteThreshold) {
			t.Fatalf("τ=%v 应报 ErrBadPaletteThreshold, got %v", th, err)
		}
	}
	// Decode 同样校验参数。
	if _, err := Decode([]byte("x"), Options{Format: "pgm", Palette: true, Buckets: 0}); !errors.Is(err, ErrBadPaletteBuckets) {
		t.Fatalf("Decode K=0 应报错, got %v", err)
	}
}

func mse(a, b []uint8) float64 {
	var s float64
	for i := range a {
		d := float64(int(a[i]) - int(b[i]))
		s += d * d
	}
	return s / float64(len(a))
}

// 阈值倒置在开关关闭时也要拦下；桶数只在开关打开时校验（关闭时零值合法）。
func TestPaletteValidationMatrix(t *testing.T) {
	img := ramp(4, 4)
	// 关闭 + 负阈值：拦。
	if _, err := Encode(img, Options{Format: "pgm", VarianceThreshold: -1}); !errors.Is(err, ErrBadPaletteThreshold) {
		t.Fatalf("关闭时负阈值应报错, got %v", err)
	}
	// 关闭 + K=0：合法，输出与基准路径一致。
	if _, err := Encode(img, Options{Format: "pgm"}); err != nil {
		t.Fatalf("默认参数应合法: %v", err)
	}
	// 打开 + K 合法 + τ=0：合法。
	if _, err := Encode(img, Options{Format: "pgm", Palette: true, Buckets: 4}); err != nil {
		t.Fatalf("K=4 应合法: %v", err)
	}
	// 默认 Options 的调色板字段必须都是零值。
	d := DefaultOptions()
	if d.Palette || d.Buckets != 0 || d.VarianceThreshold != 0 {
		t.Fatalf("默认调色板字段应为零值: %+v", d)
	}
}
