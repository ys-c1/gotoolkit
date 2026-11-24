package api

import (
	"bytes"
	"errors"
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

// rampImage 构造一张逐行相同的水平线性渐变图 512x64。
func rampImage(t *testing.T) *Image {
	t.Helper()
	const w, h = 512, 64
	img := &Image{W: w, H: h, Pix: make([]uint8, w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Pix[y*w+x] = uint8(x / 2)
		}
	}
	return img
}

func TestPredictRequiresPublicOption(t *testing.T) {
	img := rampImage(t)
	base, err := Encode(img, Options{Format: "rle"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tier := range []int{2, 4} {
		body, err := Encode(img, Options{Format: "rle", PredictOrder: tier})
		if err != nil {
			t.Fatalf("PredictOrder=%d: %v", tier, err)
		}
		if len(body) >= len(base)/2 {
			t.Fatalf("PredictOrder=%d 未把体积压到一半以下: %d vs %d", tier, len(body), len(base))
		}
		got, err := Decode(body, Options{Format: "rle"})
		if err != nil {
			t.Fatalf("PredictOrder=%d 解码失败: %v", tier, err)
		}
		if !bytes.Equal(got.Pix, img.Pix) {
			t.Fatalf("PredictOrder=%d 往返不一致", tier)
		}
	}
}

func TestPredictReport(t *testing.T) {
	img := rampImage(t)
	for _, tier := range []int{2, 4} {
		_, rep, err := EncodeWithReport(img, Options{Format: "rle", PredictOrder: tier})
		if err != nil {
			t.Fatal(err)
		}
		if rep == nil {
			t.Fatalf("tier=%d 应有报告", tier)
		}
		if rep.Samples != img.W*img.H {
			t.Fatalf("统计区间应为全图 %d，得到 %d", img.W*img.H, rep.Samples)
		}
		// 候选数 = 1（不预测）+ Σ_{s=1..tier}(s+1)。
		want := 1 + tier*(tier+1)/2 + tier
		if len(rep.Stats) != want {
			t.Fatalf("tier=%d 候选数 %d，期望 %d", tier, len(rep.Stats), want)
		}
		best := rep.Stats[0]
		for _, st := range rep.Stats {
			if st.Samples != rep.Samples {
				t.Fatalf("候选 %v 统计区间 %d 与全图不符", st.Candidate, st.Samples)
			}
			if st.Entropy < best.Entropy {
				best = st
			}
		}
		if rep.Selected != best.Candidate {
			t.Fatalf("tier=%d 选中 %v 不是熵最小 %v", tier, rep.Selected, best.Candidate)
		}
		if rep.Emitted.KX == 0 && rep.Emitted.KY == 0 {
			t.Fatalf("tier=%d 平滑渐变不应落回 GTR1", tier)
		}
	}
}

func TestPredictOrderRejects(t *testing.T) {
	img := rampImage(t)
	for _, order := range []int{-1, -100, MaxPredictOrder + 1} {
		_, err := Encode(img, Options{Format: "rle", PredictOrder: order})
		if err == nil {
			t.Fatalf("PredictOrder=%d 应报错", order)
		}
		if !errors.Is(err, ErrBadPredictOrder) {
			t.Fatalf("PredictOrder=%d 错误码不对: %v", order, err)
		}
	}
}

func TestPredictOffKeepsGolden(t *testing.T) {
	// 关闭预测（默认）时三种格式必须与既有 golden 逐字节一致。
	img := sample()
	for _, f := range []string{"pgm", "bmp", "rle"} {
		want, err := os.ReadFile(filepath.Join("..", "testdata", "golden", f+".bin"))
		if err != nil {
			t.Skipf("缺 golden %s", f)
		}
		got, err := Encode(img, Options{Format: f, PredictOrder: 0})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("关闭预测时 %s 字节变了", f)
		}
		// 非 rle 格式即使设置 PredictOrder 也应被忽略，字节不变。
		if f == "rle" {
			continue
		}
		got2, err := Encode(img, Options{Format: f, PredictOrder: 4})
		if err != nil {
			t.Fatalf("%s 带 PredictOrder 报错: %v", f, err)
		}
		if !bytes.Equal(got2, want) {
			t.Fatalf("非 rle 格式 %s 设置 PredictOrder 后字节变了", f)
		}
	}
}

func TestDecodeIgnoresEncodeOrderCap(t *testing.T) {
	// 编码用 ≤4，解码时不设 PredictOrder 也应完整解出流内候选。
	img := rampImage(t)
	body, err := Encode(img, Options{Format: "rle", PredictOrder: 4})
	if err != nil {
		t.Fatal(err)
	}
	for _, opt := range []Options{
		{Format: "rle"},
		{Format: "rle", PredictOrder: 1},
	} {
		got, err := Decode(body, opt)
		if err != nil {
			t.Fatalf("Decode(%+v): %v", opt, err)
		}
		if !bytes.Equal(got.Pix, img.Pix) {
			t.Fatalf("Decode(%+v) 像素不一致", opt)
		}
	}
}

func TestDecodeReportPredict(t *testing.T) {
	img := rampImage(t)
	body, err := Encode(img, Options{Format: "rle", PredictOrder: 2})
	if err != nil {
		t.Fatal(err)
	}
	_, rep, err := DecodeWithReport(body, Options{Format: "rle"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Predict.KX != 1 || rep.Predict.KY != 1 {
		t.Fatalf("解码报告候选 %v，期望 x1/y1", rep.Predict)
	}
}
